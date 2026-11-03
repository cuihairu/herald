package apps

import (
	"errors"
	"fmt"
	"net"
	"net/url"
)

// ErrCallbackUnresolvable marks a callback hostname that could not be
// resolved at validation time. Delivery treats it as transient (a DNS
// answer may appear on a later attempt), while configuration-time uses
// treat it as a plain refusal.
var ErrCallbackUnresolvable = errors.New("callback url host could not be resolved")

// Callback is one app's §13.5 webhook callback face configuration: the
// URL herald posts delivery results and unsubscribe backflow to, and the
// shared secret every payload's HMAC-SHA256 signature is computed with.
type Callback struct {
	URL    string `json:"url"`
	Secret string `json:"-"`
}

// Callback secret bounds: it signs every callback, so a short one would
// make the signature decorative; a long one is a paste mistake.
const (
	minCallbackSecret = 16
	maxCallbackSecret = 128
)

// defaultCallbackDenyCIDRs are the networks a callback URL must never
// point at unless the operator explicitly exempts them. A config-scope
// integration could otherwise direct signed callbacks at the internal
// control plane or an adjacent host in a multi-tenant deployment.
var defaultCallbackDenyCIDRs = []string{
	"127.0.0.0/8",    // loopback
	"::1/128",        // IPv6 loopback
	"10.0.0.0/8",     // RFC1918 private
	"172.16.0.0/12",  // RFC1918 private
	"192.168.0.0/16", // RFC1918 private
	"169.254.0.0/16", // link-local
	"0.0.0.0/8",      // unspecified
	"::/128",         // IPv6 unspecified
	"fe80::/10",      // IPv6 link-local
	"fc00::/7",       // IPv6 unique-local
	"100.64.0.0/10",  // CGNAT (RFC 6598) — cloud internal services and metadata
	"192.0.0.0/24",   // IETF protocol assignments (RFC 6890)
	"198.18.0.0/15",  // benchmarking (RFC 2544)
	"240.0.0.0/4",    // reserved, incl. broadcast (RFC 1112)
}

// CallbackPolicy controls which callback URL destinations are accepted.
// The zero value denies every private/loopback/link-local network. The
// operator may relax this per deployment (e.g. an on-prem callback).
//
// The deny list always wins: AllowCIDRs and AllowPrivate can never
// re-admit a network on the default deny list. AllowCIDRs is only for
// networks the default list does not cover (e.g. a private range the
// operator genuinely owns is already denied, so it cannot be re-opened
// here). To point a callback at loopback or a private host, set
// AllowPrivate — an explicit, deployment-wide decision.
type CallbackPolicy struct {
	// AllowPrivate exempts the default deny list entirely. Prefer an
	// explicit allow list when the destination is a known internal host.
	AllowPrivate bool
	// AllowCIDRs exempts specific networks from the deny list — but only
	// networks NOT already on the default deny list. The default deny
	// list stays absolute: it is the SSRF hard stop. Entries must be
	// valid CIDRs or they are ignored.
	AllowCIDRs []string
}

type cidrSet struct {
	nets []*net.IPNet
}

func parseCIDRs(raw []string) *cidrSet {
	s := &cidrSet{}
	for _, r := range raw {
		if _, n, err := net.ParseCIDR(r); err == nil {
			s.nets = append(s.nets, n)
		}
	}
	return s
}

func (s *cidrSet) contains(ip net.IP) bool {
	for _, n := range s.nets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

var defaultCallbackDeny = func() *cidrSet {
	return parseCIDRs(defaultCallbackDenyCIDRs)
}()

// SetCallback attaches or replaces the app's callback configuration.
// The URL must be absolute http(s); the secret must be 16-128 chars.
// Both refuse, never clamp.
func (r *Registry) SetCallback(app, rawURL, secret string) error {
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("callback url must be an absolute http(s) url")
	}
	if len(secret) < minCallbackSecret || len(secret) > maxCallbackSecret {
		return fmt.Errorf("callback secret must be %d-%d chars", minCallbackSecret, maxCallbackSecret)
	}
	if err := r.validateCallbackDestination(u); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	a, ok := r.apps[app]
	if !ok {
		return ErrUnknownApp
	}
	a.callback = &Callback{URL: u.String(), Secret: secret}
	return nil
}

// SetCallbackPolicy configures the destination exemptions for this
// registry. A zero policy keeps the strict default: every private,
// loopback, link-local, and unspecified network is refused.
func (r *Registry) SetCallbackPolicy(p CallbackPolicy) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.callbackPolicy = p
}

// ValidateCallbackURL re-runs the destination policy on a stored callback
// URL. Delivery calls this immediately before POSTing so a hostname whose
// DNS answer changed after SetCallback cannot bypass the deny list.
func (r *Registry) ValidateCallbackURL(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("callback url must be an absolute http(s) url")
	}
	return r.validateCallbackDestination(u)
}

// ResolveCallbackURL re-runs the destination policy and returns the
// addresses the URL was validated against. Delivery pins these IPs on
// the connection so the hostname cannot be re-resolved (and rebound to a
// denied network) between this check and the actual POST.
func (r *Registry) ResolveCallbackURL(rawURL string) ([]net.IP, error) {
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("callback url must be an absolute http(s) url")
	}
	return r.resolveCallbackDestination(u)
}

// validateCallbackDestination rejects destinations on the deny list
// unless the configured policy exempts them. Both literal IP hosts and
// hostnames (resolved against the system resolver) are checked.
func (r *Registry) validateCallbackDestination(u *url.URL) error {
	_, err := r.resolveCallbackDestination(u)
	return err
}

// resolveCallbackDestination rejects destinations on the deny list
// unless the configured policy exempts them, and returns the IPs that
// passed. Both literal IP hosts and hostnames (resolved against the
// system resolver) are checked.
func (r *Registry) resolveCallbackDestination(u *url.URL) ([]net.IP, error) {
	host := u.Hostname()
	if host == "" {
		return nil, fmt.Errorf("callback url must have a host")
	}
	r.mu.RLock()
	allowPrivate := r.callbackPolicy.AllowPrivate
	allow := parseCIDRs(r.callbackPolicy.AllowCIDRs)
	lookupIP := r.LookupIP
	r.mu.RUnlock()

	ips := []net.IP{}
	if ip := net.ParseIP(host); ip != nil {
		ips = append(ips, ip)
	} else if resolved, err := lookupIP(host); err == nil {
		ips = append(ips, resolved...)
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("%w: %s", ErrCallbackUnresolvable, host)
	}
	for _, ip := range ips {
		// The default deny list is an absolute hard stop: an allow-list
		// entry cannot re-admit loopback/private/link-local. Checking it
		// first means a malicious "allow 0.0.0.0/0" cannot blanket-reopen
		// the networks the policy exists to refuse.
		if defaultCallbackDeny.contains(ip) {
			if !allowPrivate {
				return nil, fmt.Errorf("callback url must not target a private/loopback network %q", host)
			}
			continue
		}
		if allow.contains(ip) {
			continue
		}
	}
	return ips, nil
}

// ClearCallback removes the callback face: the app stops receiving
// events (the emitter skips unconfigured apps).
func (r *Registry) ClearCallback(app string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	a, ok := r.apps[app]
	if !ok {
		return ErrUnknownApp
	}
	a.callback = nil
	return nil
}

// Callback returns the app's callback configuration, if set.
func (r *Registry) Callback(app string) (*Callback, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	a, ok := r.apps[app]
	if !ok || a.callback == nil {
		return nil, false
	}
	return a.callback, true
}
