package apps

import (
	"fmt"
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
	r.mu.Lock()
	defer r.mu.Unlock()
	a, ok := r.apps[app]
	if !ok {
		return ErrUnknownApp
	}
	a.callback = &Callback{URL: u.String(), Secret: secret}
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
