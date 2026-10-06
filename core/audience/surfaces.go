package audience

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"
)

// SurfaceStatus is the lifecycle state of a contact surface (§3.2): a
// binding starts pending (issued but unredeemed, or a rebind awaiting the
// old channel's confirm), turns active on redemption, and goes invalid
// when the outside world says the handle is dead (reconciliation, chat
// unreachable, unsubscribe回流). Only active surfaces receive deliveries.
type SurfaceStatus string

const (
	// SurfacePending: bound in intent, not yet usable. Never delivered to.
	SurfacePending SurfaceStatus = "pending"
	// SurfaceActive: redeemed and usable. Delivered to per its relations.
	SurfaceActive SurfaceStatus = "active"
	// SurfaceInvalid: the external platform marked the handle dead. Not
	// delivered to; a fresh binding re-activates the slot without asking
	// the dead channel for confirm.
	SurfaceInvalid SurfaceStatus = "invalid"
)

// BindingOutcome is what a token redemption did: activated the surface
// outright, or parked the new target behind the old channel's confirm
// (the rebind guard against "one deep link steals someone's channel").
type BindingOutcome string

const (
	// OutcomeActivated: the surface is active with the redeemed target.
	OutcomeActivated BindingOutcome = "activated"
	// OutcomePendingConfirm: a same-type active surface exists; the new
	// target waits in pending until the OLD channel confirms.
	OutcomePendingConfirm BindingOutcome = "pending_confirm"
)

// RedeemResult reports what redeeming a binding token did.
type RedeemResult struct {
	Outcome BindingOutcome
	// Surface is the resulting state: the active surface on activation,
	// or the parked pending target on OutcomePendingConfirm.
	Surface ContactSurface
}

// ContactSurface is one channel handle bound to an audience — the 受众的
// 联系面. The audience layer owns these; applications only ever see
// audience ids and binding tokens, never the credential plaintext of the
// channel itself.
type ContactSurface struct {
	AudienceID string
	Channel    string
	Target     string
	Status     SurfaceStatus
}

// Binding token lifecycle errors. ErrBindingUnknown covers both never-
// issued and already-consumed tokens — a one-time token that has been
// redeemed once is gone, and telling the two apart for an unauthenticated
// bearer string buys nothing. ErrBindingExpired is distinct: the caller
// may usefully prompt for a fresh link.
var (
	ErrBindingUnknown   = errors.New("audience: binding token unknown, used or revoked")
	ErrBindingExpired   = errors.New("audience: binding token expired")
	ErrRebindNotPending = errors.New("audience: no pending rebind on this surface")
	ErrSurfaceNotFound  = errors.New("audience: contact surface not found")
)

// maxTargetChars bounds a surface target. 256 covers RFC-maximum email
// addresses (254) and every chat handle in use; the point is a bound, not
// a policy.
const maxTargetChars = 256

// bindingToken is a one-time, expiring deep-link token: issued by the
// binding API, redeemed by the bot's /start <token>.
type bindingToken struct {
	audienceID string
	channel    string
	expiresAt  time.Time
}

// pendingRebind is a redeemed target waiting for the OLD channel to
// confirm the switch. The incumbent surface stays active (deliveries do
// not stop while the confirm window runs); if the window lapses the
// pending target is dropped and the incumbent keeps the slot.
type pendingRebind struct {
	target    string
	expiresAt time.Time
}

// SurfaceRegistry owns contact surfaces and the binding token flow
// (§3). In-memory like Registry — the shape is the API, not the storage.
// Time is injected so expiry behaviour is deterministic under test.
type SurfaceRegistry struct {
	mu       sync.RWMutex
	surfaces map[string]map[string]ContactSurface // audienceID -> channel -> surface
	tokens   map[string]bindingToken              // token value -> one-time record
	rebinds  map[string]map[string]pendingRebind  // audienceID -> channel -> parked target
	rss      map[string]string                    // audienceID -> private RSS feed token
	ttl      time.Duration                        // token and confirm-window lifetime (15 min by default)
	now      func() time.Time
}

// BindingTTL is how long a binding token — and a pending rebind's confirm
// window — stays live.
const BindingTTL = 15 * time.Minute

// NewSurfaceRegistry creates an empty surface registry with the default
// 15-minute binding TTL.
func NewSurfaceRegistry() *SurfaceRegistry {
	return &SurfaceRegistry{
		surfaces: make(map[string]map[string]ContactSurface),
		tokens:   make(map[string]bindingToken),
		rebinds:  make(map[string]map[string]pendingRebind),
		rss:      make(map[string]string),
		ttl:      BindingTTL,
		now:      time.Now,
	}
}

// IssueBinding starts a binding: a one-time token the application hands
// the user as a bot deep link. One token binds one audience on one
// channel; it expires unused after the TTL and is consumed by the first
// redemption.
func (s *SurfaceRegistry) IssueBinding(audienceID, channel string) (string, error) {
	if err := s.checkIDAndChannel(audienceID, channel); err != nil {
		return "", err
	}
	token, err := newToken()
	if err != nil {
		return "", fmt.Errorf("audience: generate binding token: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tokens[token] = bindingToken{audienceID: audienceID, channel: channel, expiresAt: s.now().Add(s.ttl)}
	return token, nil
}

// RedeemBinding consumes a one-time token and binds target as the
// audience's contact surface on the token's channel. Three shapes:
//
//   - no live surface on the slot (or one that is not active): the target
//     activates outright — first bind, or re-binding a channel the
//     outside world already declared invalid (nothing left to hijack);
//   - an active surface with the same target: a refresh, still a plain
//     activation — no point making the user confirm what already works;
//   - an active surface with a different target: the §3.1 rebind guard —
//     the new target parks as pending and only switches after the OLD
//     channel confirms (ConfirmRebind). The incumbent keeps receiving
//     deliveries meanwhile.
func (s *SurfaceRegistry) RedeemBinding(token, target string) (RedeemResult, error) {
	if len(target) == 0 || len(target) > maxTargetChars {
		return RedeemResult{}, fmt.Errorf("audience: surface target must be 1-%d chars", maxTargetChars)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	rec, ok := s.tokens[token]
	if !ok {
		return RedeemResult{}, ErrBindingUnknown
	}
	if s.now().After(rec.expiresAt) {
		delete(s.tokens, token)
		return RedeemResult{}, ErrBindingExpired
	}
	// One-time: the token is consumed whatever the outcome below.
	delete(s.tokens, token)

	slot := s.slot(rec.audienceID)
	surface, exists := slot[rec.channel]
	if exists && surface.Status == SurfaceActive && surface.Target != target {
		// Rebind guard: park the new target behind the old channel's
		// confirm; the confirm window shares the token TTL.
		if s.rebinds[rec.audienceID] == nil {
			s.rebinds[rec.audienceID] = make(map[string]pendingRebind)
		}
		s.rebinds[rec.audienceID][rec.channel] = pendingRebind{target: target, expiresAt: s.now().Add(s.ttl)}
		return RedeemResult{
			Outcome: OutcomePendingConfirm,
			Surface: ContactSurface{AudienceID: rec.audienceID, Channel: rec.channel, Target: target, Status: SurfacePending},
		}, nil
	}

	fresh := ContactSurface{AudienceID: rec.audienceID, Channel: rec.channel, Target: target, Status: SurfaceActive}
	slot[rec.channel] = fresh
	// A conflicting parked rebind cannot survive its incumbent changing.
	delete(s.rebinds[rec.audienceID], rec.channel)
	return RedeemResult{Outcome: OutcomeActivated, Surface: fresh}, nil
}

// ConfirmRebind applies the old channel's approval: the parked target
// takes the slot. A confirm past the window fails and drops the pending
// target — the incumbent keeps the surface, and the requester must bind
// afresh. Confirming with nothing pending is a caller bug.
func (s *SurfaceRegistry) ConfirmRebind(audienceID, channel string) (ContactSurface, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	pending, ok := s.rebinds[audienceID][channel]
	if !ok {
		return ContactSurface{}, ErrRebindNotPending
	}
	delete(s.rebinds[audienceID], channel)
	if s.now().After(pending.expiresAt) {
		return ContactSurface{}, ErrBindingExpired
	}
	surface := ContactSurface{AudienceID: audienceID, Channel: channel, Target: pending.target, Status: SurfaceActive}
	s.slot(audienceID)[channel] = surface
	return surface, nil
}

// Surface returns one contact surface.
func (s *SurfaceRegistry) Surface(audienceID, channel string) (ContactSurface, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	surface, ok := s.surfaces[audienceID][channel]
	return surface, ok
}

// Surfaces lists every contact surface held for one audience.
func (s *SurfaceRegistry) Surfaces(audienceID string) []ContactSurface {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]ContactSurface, 0, len(s.surfaces[audienceID]))
	for _, surface := range s.surfaces[audienceID] {
		out = append(out, surface)
	}
	return out
}

// Invalidate marks a surface dead on the outside world's word —
// reconciliation, an unreachable chat, an unsubscribe回流. Deliveries
// stop; the next token redemption re-activates the slot without asking
// the dead channel to confirm.
func (s *SurfaceRegistry) Invalidate(audienceID, channel string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	slot := s.slot(audienceID)
	surface, ok := slot[channel]
	if !ok {
		return ErrSurfaceNotFound
	}
	surface.Status = SurfaceInvalid
	slot[channel] = surface
	return nil
}

// RSSToken returns the audience's private RSS feed token, issuing it on
// first use (§3.1: 随受众一份，零绑定成本). The value is stable for the
// audience's lifetime — it IS the feed address's secret part (§9).
func (s *SurfaceRegistry) RSSToken(audienceID string) (string, error) {
	if !idPattern.MatchString(audienceID) {
		return "", fmt.Errorf("audience: invalid audience id %q (want 1-64 chars of letters, digits, dot, dash, underscore)", audienceID)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if tok, ok := s.rss[audienceID]; ok {
		return tok, nil
	}
	tok, err := newToken()
	if err != nil {
		return "", fmt.Errorf("audience: generate rss token: %w", err)
	}
	s.rss[audienceID] = tok
	return tok, nil
}

// checkIDAndChannel validates the two fields every binding names, with
// the same rules as the static tables.
func (s *SurfaceRegistry) checkIDAndChannel(audienceID, channel string) error {
	if !idPattern.MatchString(audienceID) {
		return fmt.Errorf("audience: invalid audience id %q (want 1-64 chars of letters, digits, dot, dash, underscore)", audienceID)
	}
	if len(channel) == 0 || len(channel) > maxNameChars {
		return fmt.Errorf("audience: channel must be 1-%d chars", maxNameChars)
	}
	return nil
}

// slot returns the audience's channel→surface map, creating it if absent.
// Write paths only — callers hold the write lock; readers index
// s.surfaces directly (a nil inner map reads as empty).
func (s *SurfaceRegistry) slot(audienceID string) map[string]ContactSurface {
	if s.surfaces[audienceID] == nil {
		s.surfaces[audienceID] = make(map[string]ContactSurface)
	}
	return s.surfaces[audienceID]
}

// newToken mints 128 bits of randomness as hex — the bearer secret of a
// deep link or a private feed.
func newToken() (string, error) {
	buf := make([]byte, 16)
	if _, err := randRead(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// randRead is a test seam for the token entropy source.
var randRead = rand.Read
