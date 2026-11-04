package callback

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sort"
	"sync"
	"time"

	"github.com/cuihairu/herald/core"
	"github.com/cuihairu/herald/core/apps"
	"github.com/cuihairu/herald/core/httpclient"
	"github.com/cuihairu/herald/core/retry"
)

// ProviderName is the queue-side provider name every callback task
// carries; heraldd registers one instance of this provider under it.
const ProviderName = "app-callback"

// Provider posts signed callback events to the URL the emitter pinned
// in the task targets. It carries no per-instance configuration: the
// event, its signature and its destination all travel on the task, so
// one instance serves every app namespace.
type Provider struct {
	status *core.ProviderStatus
	client *httpclient.Client
	apps   *apps.Registry
	mu     sync.Mutex
	pinned map[string]*pinnedClient
	// unregisterHook is the registry change-hook handle for the current
	// apps registry; it is detached when the registry is swapped or the
	// provider is discarded. Holds the lock order: p.mu before registry.
	unregisterHook func()
}

// pinnedClient remembers one URL's pinned client so repeated deliveries to
// the same destination reuse the same transport (and its idle connection
// pool) instead of cloning a fresh one on every POST.
type pinnedClient struct {
	ipsKey string
	client *httpclient.Client
}

// NewProvider builds the callback dispatcher provider.
func NewProvider() *Provider {
	return &Provider{
		status: &core.ProviderStatus{
			Name:   ProviderName,
			Type:   ProviderName,
			Status: "available",
			Since:  time.Now(),
		},
		client: httpclient.NewClient(nil),
	}
}

// SetAppRegistry attaches the live integration namespace registry. When
// present, the provider re-runs the callback destination policy on each
// stored URL immediately before posting — the Set-time check cannot see
// DNS answers that changed between configuration and delivery.
func (p *Provider) SetAppRegistry(reg *apps.Registry) {
	p.mu.Lock()
	prev := p.apps
	prevUnregister := p.unregisterHook
	// Register the new registry's hook before publishing it as live so a
	// Set/Clear on the new registry can never fire into a window where no
	// subscriber is attached (which would leak the dropped URL's pinned
	// transport until the next reattach). The hook only calls
	// dropPinnedURL, which takes p.mu; it is registered before p.pinned is
	// cleared, so a notify racing this reattach simply drops a cache the
	// swap is about to discard anyway.
	if reg != nil {
		hook := reg.AddCallbackChangeHook(func(url string, present bool) {
			// Drop the cached transport only when no app references the
			// URL anymore. Two apps sharing one URL both deliver through
			// the same pinned client; one app clearing its callback must
			// not tear down a transport the other still uses.
			if !present {
				p.dropPinnedURL(url)
			}
		})
		p.unregisterHook = hook
	} else {
		p.unregisterHook = nil
	}
	p.apps = reg
	p.pinned = nil
	p.mu.Unlock()
	// Detach from the previous registry outside the lock: the unregister
	// takes the registry lock, and a notify may re-enter dropPinnedURL
	// (which takes p.mu), so holding p.mu here would invert the lock
	// order.
	if prev != nil && prevUnregister != nil {
		prevUnregister()
	}
}

// dropPinnedURL removes the cached pinned transport for a callback URL
// that is no longer attached. The transport's idle connection pool would
// otherwise be retained indefinitely, growing without bound as URLs are
// added and cleared at runtime.
func (p *Provider) dropPinnedURL(rawURL string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.pinned, rawURL)
}

// Name identifies the provider on the queue task.
func (p *Provider) Name() string { return ProviderName }

// Type is the provider family.
func (p *Provider) Type() string { return ProviderName }

// Status reports the provider's health.
func (p *Provider) Status() *core.ProviderStatus { return p.status }

// Deliver posts one callback event. A non-2xx answer is an error so the
// pipeline's retry budget and at-least-once redelivery apply to the
// callback exactly as they do to any delivery.
func (p *Provider) Deliver(ctx context.Context, task *core.DeliveryTask) error {
	if len(task.Targets) == 0 || task.Targets[0] == "" {
		return fmt.Errorf("callback: task carries no url")
	}
	eventID, _ := task.Payload.Raw["event_id"].(string)
	body, _ := task.Payload.Raw["body"].(string)
	signature, _ := task.Payload.Raw["signature"].(string)
	if body == "" || signature == "" {
		return fmt.Errorf("callback: task carries no signed event body")
	}
	// Revalidate the destination and pin the validated addresses on the
	// connection. The hostname is not re-resolved by the transport, so a
	// DNS answer that moves after this check cannot redirect the POST to
	// a network the policy refused.
	client := p.client
	p.mu.Lock()
	reg := p.apps
	p.mu.Unlock()
	if reg != nil {
		ips, err := reg.ResolveCallbackURL(task.Targets[0])
		if err != nil {
			// A hostname that momentarily fails to resolve is transient:
			// the next attempt may see a healthy DNS answer. Deterministic
			// refusals (private/loopback destinations) stay terminal.
			if errors.Is(err, apps.ErrCallbackUnresolvable) {
				return retry.NewRetryableError(fmt.Errorf("callback: %w", err))
			}
			return fmt.Errorf("callback: %w", err)
		}
		if len(ips) > 0 {
			client = p.pinnedClientFor(task.Targets[0], ips)
		}
	}
	// Non-2xx answers come back as errors from the client itself, so
	// the pipeline's retry budget applies to them unchanged.
	_, err := client.PostJSONWithHeaders(ctx, task.Targets[0], json.RawMessage(body), map[string]string{
		"Content-Type":  "application/json",
		SignatureHeader: signature,
		EventIDHeader:   eventID,
	})
	return err
}

// pinnedClientFor returns a client that dials exactly the validated IPs.
// The same URL with the same address set reuses one transport (keep-alive
// pool preserved); a changed DNS answer builds a fresh one because the
// old pinned address set must not be reused.
func (p *Provider) pinnedClientFor(rawURL string, ips []net.IP) *httpclient.Client {
	key := rawURL
	ipsKey := joinIPs(ips)
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.pinned == nil {
		p.pinned = make(map[string]*pinnedClient)
	}
	if e, ok := p.pinned[key]; ok && e.ipsKey == ipsKey {
		return e.client
	}
	c := p.client.WithPinnedIPs(ips)
	p.pinned[key] = &pinnedClient{ipsKey: ipsKey, client: c}
	return c
}

func joinIPs(ips []net.IP) string {
	// The address set is unordered (it comes from a DNS answer), so sort
	// it before hashing: the same set in a different answer order must
	// map to the same pinned client, not spuriously rebuild a transport.
	sorted := make([]net.IP, len(ips))
	copy(sorted, ips)
	sort.Slice(sorted, func(i, j int) bool {
		ib, jb := sorted[i].To16(), sorted[j].To16()
		if ib == nil || jb == nil {
			return sorted[i].String() < sorted[j].String()
		}
		for k := 0; k < 16; k++ {
			if ib[k] != jb[k] {
				return ib[k] < jb[k]
			}
		}
		return false
	})
	b := make([]byte, 0, len(ips)*16)
	for _, ip := range sorted {
		if v4 := ip.To4(); v4 != nil {
			b = append(b, v4...)
		} else if v16 := ip.To16(); v16 != nil {
			b = append(b, v16...)
		}
		b = append(b, 0)
	}
	return string(b)
}
