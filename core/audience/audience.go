// Package audience implements the user-level half of the audience model:
// named recipients each holding provider endpoints, and named audiences
// aggregating recipients. "user:" channel references resolve through it;
// the channel-centric half ("group:") lives in core/groups. See
// docs/design-audience-model.md for the design.
package audience

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
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

// ValidID reports whether an audience id is well-formed — the same
// pattern the registries enforce on write, exported so API entries
// (§8 source adapters) can 422 an input instead of layering a raw
// registry error.
func ValidID(id string) bool { return idPattern.MatchString(id) }

// Bounds keep the static audience tables the size of config, not a
// directory: squad-sized lists are the target, larger ones want a real
// identity system feeding recipients in (groups get that via their API).
const (
	maxAudienceRecipients = 64
	maxEndpoints          = 64
	maxNameChars          = 64
)

// Manager owns the live audience table and the static recipient table,
// and resolves "user:" references against them. Audiences are mutable at
// runtime through the store-backed CRUD API (the same shape groups use);
// recipients stay config-only — their write API remains deferred. The
// delivery hot path reads only the in-memory snapshot.
type Manager struct {
	mu         sync.RWMutex
	store      Store // nil = memory only
	audiences  map[string]Audience
	order      []string // live-table order; config seeds land sorted
	recipients map[string]Recipient
}

// validateAudience enforces the write-path rules for one audience: the
// same rules the config seed goes through at startup, applied to every
// runtime write so the two sources cannot drift apart.
func validateAudience(id string, aud Audience, recipients map[string]Recipient) error {
	if !idPattern.MatchString(id) {
		return fmt.Errorf("audience: invalid audience id %q (want 1-64 chars of letters, digits, dot, dash, underscore)", id)
	}
	if len(aud.Recipients) == 0 {
		return fmt.Errorf("audience: audience %q: recipients must list at least one id", id)
	}
	if len(aud.Recipients) > maxAudienceRecipients {
		return fmt.Errorf("audience: audience %q: %d recipients, max is %d", id, len(aud.Recipients), maxAudienceRecipients)
	}
	for _, rid := range aud.Recipients {
		if _, ok := recipients[rid]; !ok {
			return fmt.Errorf("audience: audience %q references unknown recipient %q", id, rid)
		}
	}
	return nil
}

// NewManager builds a manager over the config tables and (optionally) a
// store, validating that every audience references existing recipients
// and every recipient has at least one bounded non-blank endpoint. The
// config audiences seed the live table in sorted order; with a store
// attached, call Reload once and re-Put the seeds so the config wins
// same-id collisions (the exact groups startup semantics). Failures are
// configuration drift and refuse to start: a "user:" reference must
// never silently resolve to nothing at delivery time.
func NewManager(store Store, audiences map[string]Audience, recipients map[string]Recipient) (*Manager, error) {
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
	live := make(map[string]Audience, len(audiences))
	order := make([]string, 0, len(audiences))
	for id, aud := range audiences {
		if err := validateAudience(id, aud, recipients); err != nil {
			return nil, err
		}
		live[id] = aud
		order = append(order, id)
	}
	sort.Strings(order)
	return &Manager{store: store, audiences: live, order: order, recipients: recipients}, nil
}

// Reload re-reads every audience from the store into the live table. An
// entry that fails validation aborts the reload keeping the previous
// table serving, naming the offender.
func (m *Manager) Reload(ctx context.Context) error {
	if m.store == nil {
		return nil
	}
	stored, err := m.store.List(ctx)
	if err != nil {
		return fmt.Errorf("audience: store list: %w", err)
	}
	order := make([]string, 0, len(stored))
	live := make(map[string]Audience, len(stored))
	for _, entry := range stored {
		aud := Audience{Recipients: entry.Recipients}
		if err := validateAudience(entry.ID, aud, m.recipients); err != nil {
			return fmt.Errorf("audience: reload stopped at audience %q: %w", entry.ID, err)
		}
		live[entry.ID] = aud
		order = append(order, entry.ID)
	}
	m.mu.Lock()
	m.audiences = live
	m.order = order
	m.mu.Unlock()
	return nil
}

// Put validates then persists an audience and refreshes the live table
// in place (existing ids keep their position, new ids go last). A failed
// persist leaves the live table untouched.
func (m *Manager) Put(ctx context.Context, id string, aud Audience) error {
	if err := validateAudience(id, aud, m.recipients); err != nil {
		return err
	}
	if m.store != nil {
		if err := m.store.Put(ctx, Entry{ID: id, Recipients: aud.Recipients}); err != nil {
			return fmt.Errorf("audience: store put: %w", err)
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.audiences[id]; !ok {
		m.order = append(m.order, id)
	}
	m.audiences[id] = aud
	return nil
}

// Delete removes an audience from the store and the live table.
// References to it are not cleaned up — a "user:" reference to a deleted
// id fails loudly at expansion, never silently resolving to nothing.
func (m *Manager) Delete(ctx context.Context, id string) error {
	if m.store != nil {
		if err := m.store.Delete(ctx, id); err != nil {
			return err
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.audiences[id]; !ok {
		return ErrNotFound
	}
	for i, existing := range m.order {
		if existing == id {
			m.order = append(m.order[:i], m.order[i+1:]...)
			break
		}
	}
	delete(m.audiences, id)
	return nil
}

// Get returns an audience by id from the live table (the delivery
// truth).
func (m *Manager) Get(ctx context.Context, id string) (Audience, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	aud, ok := m.audiences[id]
	if !ok {
		return Audience{}, ErrNotFound
	}
	return aud, nil
}

// List returns the live table in stored order.
func (m *Manager) List(ctx context.Context) ([]Entry, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Entry, len(m.order))
	for i, id := range m.order {
		out[i] = Entry{ID: id, Recipients: m.audiences[id].Recipients}
	}
	return out, nil
}

// ExpandUser resolves a user id (the name after "user:") to the endpoints
// it names. The audiences table takes precedence over the recipients
// table when both carry the id; an unknown id reports not-found.
func (m *Manager) ExpandUser(name string) ([]Endpoint, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if aud, ok := m.audiences[name]; ok {
		eps := make([]Endpoint, 0, len(aud.Recipients))
		for _, rid := range aud.Recipients {
			// The write path validated the reference: the recipient exists.
			eps = append(eps, m.recipients[rid].Endpoints...)
		}
		return eps, true
	}
	rec, ok := m.recipients[name]
	if !ok {
		return nil, false
	}
	out := make([]Endpoint, len(rec.Endpoints))
	copy(out, rec.Endpoints)
	return out, true
}
