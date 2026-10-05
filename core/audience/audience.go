// Package audience implements the user-level half of the audience model:
// named recipients each holding provider endpoints, and named audiences
// aggregating recipients. "user:" channel references resolve through it;
// the channel-centric half ("group:") lives in core/groups. See
// docs/design-audience-model.md for the design.
package audience

import (
	"fmt"
	"regexp"
	"strings"
)

// UserPrefix marks a channel reference as user-level: `user:alice` names
// either the audience "alice" or the recipient "alice" (the audience
// table takes precedence). Like "group:", the explicit category word
// cannot collide with provider instance names.
const UserPrefix = "user:"

// Ref builds the channel reference for a user id.
func Ref(id string) string {
	return UserPrefix + id
}

// IsUserRef reports whether a channel reference names a user audience.
func IsUserRef(ref string) bool {
	return strings.HasPrefix(ref, UserPrefix)
}

// Endpoint is one concrete delivery address of a recipient: the provider
// instance the delivery goes through and the provider-specific target
// (chat id, email address, phone number — whatever that provider expects).
type Endpoint struct {
	Type   string `json:"type" yaml:"type"`
	Target string `json:"target" yaml:"target"`
}

// Recipient is a named person holding the endpoints that person receives
// on.
type Recipient struct {
	Endpoints []Endpoint `json:"endpoints" yaml:"endpoints"`
}

// Audience is a named audience aggregating recipient ids; a "user:" name
// resolving to an audience delivers to the union of its recipients'
// endpoints.
type Audience struct {
	Recipients []string `json:"recipients" yaml:"recipients"`
}

// idPattern bounds audience/recipient ids exactly like group ids.
var idPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,63}$`)

// Bounds keep the static audience tables the size of config, not a
// directory: squad-sized lists are the target, larger ones want a real
// identity system feeding recipients in (groups get that via their API).
const (
	maxAudienceRecipients = 64
	maxEndpoints          = 64
	maxNameChars          = 64
)

// Manager resolves "user:" references against the static audience and
// recipient tables. The MVP shape is config-only: unlike groups there is
// no runtime API yet, so the tables settle at startup.
type Manager struct {
	audiences  map[string]Audience
	recipients map[string]Recipient
}

// NewManager builds a manager over the config tables, validating that
// every audience references existing recipients and every recipient has
// at least one bounded non-blank endpoint. Failures are configuration
// drift and refuse to start: a "user:" reference must never silently
// resolve to nothing at delivery time.
func NewManager(audiences map[string]Audience, recipients map[string]Recipient) (*Manager, error) {
	for id, aud := range audiences {
		if !idPattern.MatchString(id) {
			return nil, fmt.Errorf("audience: invalid audience id %q (want 1-64 chars of letters, digits, dot, dash, underscore)", id)
		}
		if len(aud.Recipients) == 0 {
			return nil, fmt.Errorf("audience: audience %q: recipients must list at least one id", id)
		}
		if len(aud.Recipients) > maxAudienceRecipients {
			return nil, fmt.Errorf("audience: audience %q: %d recipients, max is %d", id, len(aud.Recipients), maxAudienceRecipients)
		}
		for _, rid := range aud.Recipients {
			if _, ok := recipients[rid]; !ok {
				return nil, fmt.Errorf("audience: audience %q references unknown recipient %q", id, rid)
			}
		}
	}
	for id, rec := range recipients {
		if !idPattern.MatchString(id) {
			return nil, fmt.Errorf("audience: invalid recipient id %q (want 1-64 chars of letters, digits, dot, dash, underscore)", id)
		}
		if len(rec.Endpoints) == 0 {
			return nil, fmt.Errorf("audience: recipient %q: endpoints must list at least one entry", id)
		}
		if len(rec.Endpoints) > maxEndpoints {
			return nil, fmt.Errorf("audience: recipient %q: %d endpoints, max is %d", id, len(rec.Endpoints), maxEndpoints)
		}
		for i, ep := range rec.Endpoints {
			if strings.TrimSpace(ep.Type) == "" || len(ep.Type) > maxNameChars {
				return nil, fmt.Errorf("audience: recipient %q: endpoint %d type must be 1-64 chars", id, i)
			}
			if strings.TrimSpace(ep.Target) == "" || len(ep.Target) > maxNameChars {
				return nil, fmt.Errorf("audience: recipient %q: endpoint %d target must be 1-64 chars", id, i)
			}
		}
	}
	return &Manager{audiences: audiences, recipients: recipients}, nil
}

// ExpandUser resolves a user id (the name after "user:") to the endpoints
// it names. The audiences table takes precedence over the recipients
// table when both carry the id; an unknown id reports not-found.
func (m *Manager) ExpandUser(name string) ([]Endpoint, bool) {
	if aud, ok := m.audiences[name]; ok {
		eps := make([]Endpoint, 0, len(aud.Recipients))
		for _, rid := range aud.Recipients {
			// NewManager validated the reference: the recipient exists.
			eps = append(eps, m.recipients[rid].Endpoints...)
		}
		return eps, true
	}
	rec, ok := m.recipients[name]
	if !ok {
		return nil, false
	}
	return rec.Endpoints, true
}
