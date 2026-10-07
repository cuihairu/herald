package audience

import (
	"github.com/cuihairu/herald/core/audit"

	"errors"
	"strings"
	"testing"
	"time"
)

// newTestSurfaces: a registry with a controllable clock so expiry paths
// run deterministically — no real waiting, no flaky windows.
func newTestSurfaces(t *testing.T) (*SurfaceRegistry, *time.Time) {
	t.Helper()
	s := NewSurfaceRegistry()
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }
	return s, &now
}

func advance(t *testing.T, now *time.Time, d time.Duration) {
	t.Helper()
	*now = now.Add(d)
}

// TestBindingRedeemActivates: the happy path — issue, redeem, active —
// and the one-time property: the second redemption of the same token is
// indistinguishable from a forged one.
func TestBindingRedeemActivates(t *testing.T) {
	s, _ := newTestSurfaces(t)

	token, err := s.IssueBinding("alice", "telegram")
	if err != nil {
		t.Fatalf("IssueBinding() error = %v", err)
	}
	if len(token) != 32 { // 128 bits hex
		t.Errorf("token = %q, want 32 hex chars", token)
	}

	res, err := s.RedeemBinding(token, "chat-123")
	if err != nil {
		t.Fatalf("RedeemBinding() error = %v", err)
	}
	if res.Outcome != OutcomeActivated {
		t.Errorf("outcome = %q, want activated", res.Outcome)
	}
	got, ok := s.Surface("alice", "telegram")
	if !ok || got.Target != "chat-123" || got.Status != SurfaceActive {
		t.Errorf("surface = %+v ok=%v, want active chat-123", got, ok)
	}

	if _, err := s.RedeemBinding(token, "chat-123"); !errors.Is(err, ErrBindingUnknown) {
		t.Errorf("re-redeem = %v, want ErrBindingUnknown (one-time)", err)
	}
}

// TestBindingExpiry: a token past its TTL is refused, and the refusal
// consumes it — expiry is not a revival.
func TestBindingExpiry(t *testing.T) {
	s, now := newTestSurfaces(t)

	token, err := s.IssueBinding("alice", "email")
	if err != nil {
		t.Fatal(err)
	}
	advance(t, now, BindingTTL+time.Minute)

	if _, err := s.RedeemBinding(token, "a@b.c"); !errors.Is(err, ErrBindingExpired) {
		t.Fatalf("redeem expired = %v, want ErrBindingExpired", err)
	}
	if _, err := s.RedeemBinding(token, "a@b.c"); !errors.Is(err, ErrBindingUnknown) {
		t.Errorf("redeem after expiry = %v, want ErrBindingUnknown (consumed)", err)
	}
	if _, ok := s.Surface("alice", "email"); ok {
		t.Error("expired token left a surface behind")
	}
}

func TestRedeemUnknownToken(t *testing.T) {
	s, _ := newTestSurfaces(t)
	if _, err := s.RedeemBinding("nosuchtoken", "a@b.c"); !errors.Is(err, ErrBindingUnknown) {
		t.Fatalf("redeem unknown = %v, want ErrBindingUnknown", err)
	}
}

// TestRedeemBoundaryTargets: the target bound is validated before the
// token is burned — a rejected redeem leaves the token usable.
func TestRedeemBoundaryTargets(t *testing.T) {
	s, _ := newTestSurfaces(t)

	cases := []struct {
		name   string
		target string
	}{
		{"empty target", ""},
		{"oversized target", strings.Repeat("x", maxTargetChars+1)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			token, err := s.IssueBinding("alice", "email")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := s.RedeemBinding(token, tc.target); err == nil {
				t.Fatal("RedeemBinding() = nil, want a target validation error")
			}
			// The token was not consumed by the rejected attempt.
			res, err := s.RedeemBinding(token, "a@b.c")
			if err != nil || res.Outcome != OutcomeActivated {
				t.Errorf("redeem after rejected attempt = %+v, %v; want the token to still work", res, err)
			}
		})
	}
}

// TestRebindRequiresOldConfirm: §3.1's guard — a new target on a live
// slot parks as pending, the incumbent keeps receiving deliveries, and
// only the old channel's confirm moves the new target in.
func TestRebindRequiresOldConfirm(t *testing.T) {
	s, _ := newTestSurfaces(t)

	seed := func() string {
		t.Helper()
		token, err := s.IssueBinding("alice", "telegram")
		if err != nil {
			t.Fatal(err)
		}
		return token
	}
	if _, err := s.RedeemBinding(seed(), "old-chat"); err != nil {
		t.Fatal(err)
	}

	res, err := s.RedeemBinding(seed(), "new-chat")
	if err != nil {
		t.Fatalf("rebind redeem = %v", err)
	}
	if res.Outcome != OutcomePendingConfirm || res.Surface.Status != SurfacePending {
		t.Errorf("rebind = %+v, want pending_confirm", res)
	}
	if got, _ := s.Surface("alice", "telegram"); got.Target != "old-chat" || got.Status != SurfaceActive {
		t.Errorf("incumbent = %+v, want old-chat still active during the confirm window", got)
	}

	got, err := s.ConfirmRebind("alice", "telegram")
	if err != nil {
		t.Fatalf("ConfirmRebind() error = %v", err)
	}
	if got.Target != "new-chat" || got.Status != SurfaceActive {
		t.Errorf("confirmed = %+v, want new-chat active", got)
	}
	if _, err := s.ConfirmRebind("alice", "telegram"); !errors.Is(err, ErrRebindNotPending) {
		t.Errorf("second confirm = %v, want ErrRebindNotPending", err)
	}
}

// TestRebindConfirmExpires: the confirm window lapsing drops the pending
// target and leaves the incumbent untouched — silence never completes a
// hijack.
func TestRebindConfirmExpires(t *testing.T) {
	s, now := newTestSurfaces(t)

	issue := func() string {
		t.Helper()
		token, err := s.IssueBinding("alice", "telegram")
		if err != nil {
			t.Fatal(err)
		}
		return token
	}
	if _, err := s.RedeemBinding(issue(), "old-chat"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RedeemBinding(issue(), "new-chat"); err != nil {
		t.Fatal(err)
	}

	advance(t, now, BindingTTL+time.Minute)

	if _, err := s.ConfirmRebind("alice", "telegram"); !errors.Is(err, ErrBindingExpired) {
		t.Fatalf("late confirm = %v, want ErrBindingExpired", err)
	}
	if got, _ := s.Surface("alice", "telegram"); got.Target != "old-chat" {
		t.Errorf("incumbent = %+v, want old-chat untouched", got)
	}
	if _, err := s.ConfirmRebind("alice", "telegram"); !errors.Is(err, ErrRebindNotPending) {
		t.Errorf("confirm after expiry = %v, want ErrRebindNotPending (pending dropped)", err)
	}
}

// TestRedeemSameTargetRefreshes: re-binding the target that already holds
// the slot is a plain re-activation — confirming what already works is
// pure friction.
func TestRedeemSameTargetRefreshes(t *testing.T) {
	s, _ := newTestSurfaces(t)

	issue := func() string {
		t.Helper()
		token, err := s.IssueBinding("alice", "email")
		if err != nil {
			t.Fatal(err)
		}
		return token
	}
	if _, err := s.RedeemBinding(issue(), "a@b.c"); err != nil {
		t.Fatal(err)
	}
	res, err := s.RedeemBinding(issue(), "a@b.c")
	if err != nil || res.Outcome != OutcomeActivated {
		t.Fatalf("same-target rebind = %+v, %v; want activated", res, err)
	}
	if got, _ := s.Surface("alice", "email"); got.Target != "a@b.c" || got.Status != SurfaceActive {
		t.Errorf("surface = %+v, want a@b.c active", got)
	}
}

// TestRedeemOverInvalidSurface: reconciliation declared the slot dead, so
// the next token activates outright — a dead channel cannot be hijacked
// and must not be allowed to block its own replacement.
func TestRedeemOverInvalidSurface(t *testing.T) {
	s, _ := newTestSurfaces(t)

	issue := func() string {
		t.Helper()
		token, err := s.IssueBinding("alice", "telegram")
		if err != nil {
			t.Fatal(err)
		}
		return token
	}
	if _, err := s.RedeemBinding(issue(), "dead-chat"); err != nil {
		t.Fatal(err)
	}
	if err := s.Invalidate("alice", "telegram"); err != nil {
		t.Fatalf("Invalidate() error = %v", err)
	}
	if got, _ := s.Surface("alice", "telegram"); got.Status != SurfaceInvalid {
		t.Errorf("status = %q, want invalid", got.Status)
	}

	res, err := s.RedeemBinding(issue(), "fresh-chat")
	if err != nil || res.Outcome != OutcomeActivated {
		t.Fatalf("redeem over invalid = %+v, %v; want activated", res, err)
	}
}

// TestInvalidateMissing: invalidating a surface that does not exist is
// reported, not silently absorbed.
func TestInvalidateMissing(t *testing.T) {
	s, _ := newTestSurfaces(t)
	if err := s.Invalidate("ghost", "email"); !errors.Is(err, ErrSurfaceNotFound) {
		t.Fatalf("Invalidate(unknown) = %v, want ErrSurfaceNotFound", err)
	}
}

// TestSurfacesListing: the per-audience view lists every channel's
// surface; an unknown audience lists as empty rather than nil.
func TestSurfacesListing(t *testing.T) {
	s, _ := newTestSurfaces(t)

	issue := func(aud, ch string) string {
		t.Helper()
		token, err := s.IssueBinding(aud, ch)
		if err != nil {
			t.Fatal(err)
		}
		return token
	}
	for _, bind := range []struct{ aud, ch, target string }{
		{"alice", "telegram", "chat-1"},
		{"alice", "email", "a@b.c"},
	} {
		if _, err := s.RedeemBinding(issue(bind.aud, bind.ch), bind.target); err != nil {
			t.Fatal(err)
		}
	}

	if got := s.Surfaces("alice"); len(got) != 2 {
		t.Errorf("Surfaces(alice) = %d items, want 2", len(got))
	}
	if got := s.Surfaces("ghost"); got == nil || len(got) != 0 {
		t.Errorf("Surfaces(unknown) = %v, want empty non-nil", got)
	}
}

// TestRSSTokenStable: the private feed token is issued once and stays —
// it is the secret part of the feed address (§9), churn would break
// subscribers.
func TestRSSTokenStable(t *testing.T) {
	s, _ := newTestSurfaces(t)

	first, err := s.RSSToken("alice")
	if err != nil {
		t.Fatalf("RSSToken() error = %v", err)
	}
	again, err := s.RSSToken("alice")
	if err != nil {
		t.Fatalf("RSSToken() again error = %v", err)
	}
	if first != again || len(first) != 32 {
		t.Errorf("rss tokens %q vs %q, want one stable 32-char secret", first, again)
	}
	if bob, err := s.RSSToken("bob"); err != nil || bob == first {
		t.Errorf("bob's token = %q, want a distinct secret", bob)
	}
	if _, err := s.RSSToken("-bad"); err == nil {
		t.Error("RSSToken(bad id) = nil, want a validation error")
	}
}

// TestBindingValidation: ids and channels are checked at the issue door,
// with the static tables' rules.
func TestBindingValidation(t *testing.T) {
	s, _ := newTestSurfaces(t)

	if _, err := s.IssueBinding("-bad", "email"); err == nil {
		t.Error("IssueBinding(bad id) = nil, want a validation error")
	}
	if _, err := s.IssueBinding("alice", ""); err == nil {
		t.Error("IssueBinding(empty channel) = nil, want a validation error")
	}
	if _, err := s.IssueBinding("alice", strings.Repeat("x", maxNameChars+1)); err == nil {
		t.Error("IssueBinding(oversized channel) = nil, want a validation error")
	}
}

// TestTokenEntropyFailure: an entropy failure surfaces as an error from
// both token consumers instead of minting a predictable secret.
func TestTokenEntropyFailure(t *testing.T) {
	s, _ := newTestSurfaces(t)

	orig := randRead
	randRead = func([]byte) (int, error) { return 0, errors.New("no entropy") }
	defer func() { randRead = orig }()

	if _, err := s.IssueBinding("alice", "email"); err == nil {
		t.Error("IssueBinding() = nil, want the entropy error")
	}
	if _, err := s.RSSToken("alice"); err == nil {
		t.Error("RSSToken() = nil, want the entropy error")
	}
}

// TestSurfaceAuditTrail: bind / rebind / invalidate each emit one event;
// a rejected invalidate emits none.
func TestSurfaceAuditTrail(t *testing.T) {
	s := NewSurfaceRegistry()
	st := audit.New(0)
	s.SetRecorder(st)

	token, err := s.IssueBinding("alice", "telegram")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.RedeemBinding(token, "chat-1"); err != nil {
		t.Fatal(err)
	}
	// Park a rebind, confirm it, then invalidate.
	token2, _ := s.IssueBinding("alice", "telegram")
	if res, _ := s.RedeemBinding(token2, "chat-2"); res.Outcome != OutcomePendingConfirm {
		t.Fatalf("Outcome = %v, want pending confirm", res.Outcome)
	}
	if _, err := s.ConfirmRebind("alice", "telegram"); err != nil {
		t.Fatal(err)
	}
	if err := s.Invalidate("alice", "telegram"); err != nil {
		t.Fatal(err)
	}
	if err := s.Invalidate("alice", "sms"); err == nil {
		t.Fatal("want not-found error")
	}

	got := st.ListByAudience("alice")
	wantKinds := []audit.EventKind{audit.SurfaceBind, audit.SurfaceRebind, audit.SurfaceInvalidate}
	if len(got) != len(wantKinds) {
		t.Fatalf("events = %+v, want %d", got, len(wantKinds))
	}
	for i, ev := range got {
		if ev.Kind != wantKinds[i] {
			t.Errorf("event %d kind = %v, want %v", i, ev.Kind, wantKinds[i])
		}
	}
}

// TestRSSAudienceAndResetToken covers the §9 private feed address:
// empty and unknown tokens resolve to nothing; a real token resolves
// back to its audience; a reset makes the old token not-found (重置即旧
// 地址失效) and the next issue mints a fresh value. Resets of audiences
// with no token — and of malformed ids — error.
func TestRSSAudienceAndResetToken(t *testing.T) {
	s := NewSurfaceRegistry()

	if id, ok := s.RSSAudience(""); ok || id != "" {
		t.Errorf("empty token = (%q, %v), want empty+false", id, ok)
	}
	if id, ok := s.RSSAudience("deadbeef"); ok || id != "" {
		t.Errorf("unknown token = (%q, %v), want empty+false", id, ok)
	}

	tok, err := s.RSSToken("alice")
	if err != nil {
		t.Fatalf("RSSToken: %v", err)
	}
	if id, ok := s.RSSAudience(tok); !ok || id != "alice" {
		t.Fatalf("RSSAudience(token) = (%q, %v), want alice+true", id, ok)
	}

	if err := s.ResetRSSToken("alice"); err != nil {
		t.Fatalf("ResetRSSToken: %v", err)
	}
	if _, ok := s.RSSAudience(tok); ok {
		t.Error("stale token still resolves after reset, want not-found")
	}
	// A second reset has nothing to retire — the caller is confused.
	if err := s.ResetRSSToken("alice"); err == nil {
		t.Error("reset without an issued token = nil, want error")
	}
	// Malformed ids are refused before the table is touched.
	if err := s.ResetRSSToken("bad id!"); err == nil {
		t.Error("reset with malformed id = nil, want error")
	}

	fresh, err := s.RSSToken("alice")
	if err != nil {
		t.Fatalf("RSSToken after reset: %v", err)
	}
	if fresh == tok {
		t.Error("reissued token equals the reset one, want fresh entropy")
	}
}
