package apps

import (
	"fmt"
	"net"
	"net/url"
)

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
}

// CallbackPolicy controls which callback URL destinations are accepted.
// The zero value denies every private/loopback/link-local network. The
// operator may relax this per deployment (e.g. an on-prem callback).
type CallbackPolicy struct {
	// AllowPrivate exempts the default deny list entirely. Prefer an
	// explicit allow list when the destination is a known internal host.
	AllowPrivate bool
	// AllowCIDRs exempts specific networks in addition to the public
	// range. Entries must be valid CIDRs or they are ignored.
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

func (s *cidrSet) empty() bool { return len(s.nets) == 0 }

var defaultCallbackDeny = func() *cidrSet {
	return parseCIDRs(defaultCallbackDenyCIDRs)
}()

// lookupIP is the resolver seam used by callback destination validation.
var lookupIP = net.LookupIP

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

// validateCallbackDestination rejects destinations on the deny list
// unless the configured policy exempts them. Both literal IP hosts and
// hostnames (resolved against the system resolver) are checked.
func (r *Registry) validateCallbackDestination(u *url.URL) error {
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("callback url must have a host")
	}
	r.mu.RLock()
	allowPrivate := r.callbackPolicy.AllowPrivate
	allow := parseCIDRs(r.callbackPolicy.AllowCIDRs)
	r.mu.RUnlock()

	ips := []net.IP{}
	if ip := net.ParseIP(host); ip != nil {
		ips = append(ips, ip)
	} else if resolved, err := lookupIP(host); err == nil {
		ips = append(ips, resolved...)
	}
	if len(ips) == 0 {
		return fmt.Errorf("callback url host %q could not be resolved", host)
	}
	for _, ip := range ips {
		if allow.contains(ip) {
			continue
		}
		if defaultCallbackDeny.contains(ip) && !allowPrivate {
			return fmt.Errorf("callback url must not target a private/loopback network %q", host)
		}
	}
	return nil
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
