package audience

import (
	"errors"
	"strings"
	"testing"

	"github.com/cuihairu/herald/core/audit"
)

// sourceFixture wires an adapter over fresh registries with a recording
// audit trail, so tests can assert attribution as well as state.
type sourceFixture struct {
	surfaces  *SurfaceRegistry
	relations *Registry
	trail     *audit.Store
	adapter   *SourceAdapter
}

func newSourceFixture() *sourceFixture {
	surfaces := NewSurfaceRegistry()
	relations := NewRegistry()
	trail := audit.New(0)
	surfaces.SetRecorder(trail)
	relations.SetRecorder(trail)
	return &sourceFixture{surfaces: surfaces, relations: relations, trail: trail, adapter: NewSourceAdapter(surfaces, relations)}
}

func TestFollowRegistersSurfaceAndDefaults(t *testing.T) {
	f := newSourceFixture()
	res, err := f.adapter.Follow("alice", "wechat_mp", "openid-1", SourceWeChatMP, []string{"notices", "bills"})
	if err != nil {
		t.Fatalf("Follow: %v", err)
	}
	if !res.BindChanged {
		t.Error("first follow should report BindChanged")
	}
	surface, ok := f.surfaces.Surface("alice", "wechat_mp")
	if !ok || surface.Status != SurfaceActive || surface.Target != "openid-1" {
		t.Fatalf("surface = %+v, ok=%v; want active openid-1", surface, ok)
	}
	if len(res.Subscribed) != 2 {
		t.Fatalf("subscribed = %d relations, want 2", len(res.Subscribed))
	}
	for _, rel := range res.Subscribed {
		if rel.Type != RelationSubscription || rel.Source != SourceWeChatMP || rel.Channel != "wechat_mp" {
			t.Errorf("default relation shape wrong: %+v", rel)
		}
		if !rel.Policy.AllowUnsubscribe || rel.Policy.MustDeliver {
			t.Errorf("default subscription policy not the unsubscribable shape: %+v", rel.Policy)
		}
	}
	// The defaults must actually deliver: filter passes on the intersection.
	filter := NewFilter(f.relations, f.surfaces)
	if !filter.Allow("alice", "notices", "wechat_mp") {
		t.Error("followed default does not pass the delivery filter")
	}
}

func TestFollowRefreshAndConflict(t *testing.T) {
	f := newSourceFixture()
	if _, err := f.adapter.Follow("alice", "wechat_mp", "openid-1", SourceWeChatMP, nil); err != nil {
		t.Fatal(err)
	}
	// Same target again: a refresh no-op.
	res, err := f.adapter.Follow("alice", "wechat_mp", "openid-1", SourceWeChatMP, nil)
	if err != nil {
		t.Fatalf("refresh follow: %v", err)
	}
	if res.BindChanged {
		t.Error("same-target follow must not report BindChanged")
	}
	// Different target on a live slot: refuse — 对账's job, not the event's.
	_, err = f.adapter.Follow("alice", "wechat_mp", "openid-2", SourceWeChatMP, nil)
	if !errors.Is(err, ErrSurfaceConflict) {
		t.Fatalf("conflicting follow = %v, want ErrSurfaceConflict", err)
	}
	// After 取关 (invalid), a new handle re-activates without the dead
	// channel's confirm — same rule as the token flow.
	if _, err := f.adapter.Unfollow("alice", "wechat_mp", SourceWeChatMP); err != nil {
		t.Fatal(err)
	}
	res, err = f.adapter.Follow("alice", "wechat_mp", "openid-2", SourceWeChatMP, nil)
	if err != nil || !res.BindChanged {
		t.Fatalf("post-unfollow follow = (%+v, %v); want re-activation", res, err)
	}
	surface, _ := f.surfaces.Surface("alice", "wechat_mp")
	if surface.Target != "openid-2" || surface.Status != SurfaceActive {
		t.Errorf("surface after re-follow = %+v", surface)
	}
}

func TestFollowActivationClearsParkedRebind(t *testing.T) {
	f := newSourceFixture()
	// First redeem activates chat-1; a redeem of chat-2 parks as pending.
	tok, err := f.surfaces.IssueBinding("alice", "telegram")
	if err != nil {
		t.Fatal(err)
	}
	if res, err := f.surfaces.RedeemBinding(tok, "chat-1"); err != nil || res.Outcome != OutcomeActivated {
		t.Fatalf("first redeem = (%+v, %v)", res, err)
	}
	tok2, err := f.surfaces.IssueBinding("alice", "telegram")
	if err != nil {
		t.Fatal(err)
	}
	if res, err := f.surfaces.RedeemBinding(tok2, "chat-2"); err != nil || res.Outcome != OutcomePendingConfirm {
		t.Fatalf("second redeem should park: (%+v, %v)", res, err)
	}
	// A platform follow of the incumbent re-affirms the live slot and
	// does NOT decide the parked challenger — a follow event for chat-1
	// says nothing about a pending rebind to chat-2.
	res, err := f.adapter.Follow("alice", "telegram", "chat-1", SourceBot, nil)
	if err != nil || res.BindChanged {
		t.Fatalf("follow incumbent = (%+v, %v); want no-op", res, err)
	}
	// The parked rebind survives the refresh and still confirms.
	if res, err := f.surfaces.ConfirmRebind("alice", "telegram"); err != nil || res.Target != "chat-2" {
		t.Fatalf("confirm after follow = (%+v, %v); want chat-2 taking the slot", res, err)
	}
}

// TestReactivationClearsParkedRebind: when the incumbent genuinely
// changes (unfollow then re-follow on a new handle), a parked rebind
// cannot outlive the slot it was racing for.
func TestReactivationClearsParkedRebind(t *testing.T) {
	f := newSourceFixture()
	if _, err := f.adapter.Follow("alice", "telegram", "chat-1", SourceBot, nil); err != nil {
		t.Fatal(err)
	}
	tok, err := f.surfaces.IssueBinding("alice", "telegram")
	if err != nil {
		t.Fatal(err)
	}
	if res, err := f.surfaces.RedeemBinding(tok, "chat-2"); err != nil || res.Outcome != OutcomePendingConfirm {
		t.Fatalf("redeem should park: (%+v, %v)", res, err)
	}
	if _, err := f.adapter.Unfollow("alice", "telegram", SourceBot); err != nil {
		t.Fatal(err)
	}
	if _, err := f.adapter.Follow("alice", "telegram", "chat-9", SourceBot, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := f.surfaces.ConfirmRebind("alice", "telegram"); !errors.Is(err, ErrRebindNotPending) {
		t.Fatalf("confirm after re-activation = %v, want ErrRebindNotPending", err)
	}
}

func TestFollowValidation(t *testing.T) {
	f := newSourceFixture()
	cases := []struct {
		name                          string
		audienceID, channel, tgt, src string
	}{
		{"bad audience id", "bad id!", "wechat_mp", "x", SourceWeChatMP},
		{"empty channel", "alice", "", "x", SourceWeChatMP},
		{"empty source", "alice", "wechat_mp", "x", ""},
		{"long source", "alice", "wechat_mp", "x", string(make([]byte, 200))},
		{"empty target", "alice", "wechat_mp", "", SourceWeChatMP},
		{"long target", "alice", "wechat_mp", string(make([]byte, 300)), SourceWeChatMP},
	}
	for _, tc := range cases {
		if _, err := f.adapter.Follow(tc.audienceID, tc.channel, tc.tgt, tc.src, nil); err == nil {
			t.Errorf("%s: Follow accepted invalid input", tc.name)
		}
	}
}

func TestUnfollowStopsEverythingOnTheChannel(t *testing.T) {
	f := newSourceFixture()
	if _, err := f.adapter.Follow("alice", "wechat_mp", "openid-1", SourceWeChatMP, []string{"notices", "bills"}); err != nil {
		t.Fatal(err)
	}
	// An enrollment on the same channel survives — its entitlement is not
	// the audience's to undo, the dead surface stops it instead.
	if err := f.relations.Enroll(Relation{
		AudienceID: "alice", Category: "system", Channel: "wechat_mp",
		Type: RelationEnrollment, Source: SourceAdmin, Policy: Policy{MustDeliver: true},
	}); err != nil {
		t.Fatal(err)
	}
	// And a subscription on another channel is untouched.
	if err := f.relations.Subscribe(Relation{
		AudienceID: "alice", Category: "notices", Channel: "email",
		Type: RelationSubscription, Source: SourcePreferenceCenter,
	}); err != nil {
		t.Fatal(err)
	}

	res, err := f.adapter.Unfollow("alice", "wechat_mp", SourceWeChatMP)
	if err != nil {
		t.Fatalf("Unfollow: %v", err)
	}
	if !res.SurfaceInvalidated {
		t.Error("unfollow should invalidate the live surface")
	}
	if len(res.Terminated) != 2 {
		t.Fatalf("terminated %d subscriptions, want 2", len(res.Terminated))
	}
	surface, _ := f.surfaces.Surface("alice", "wechat_mp")
	if surface.Status != SurfaceInvalid {
		t.Errorf("surface = %s, want invalid", surface.Status)
	}
	if _, ok := f.relations.Lookup("alice", "system", "wechat_mp"); !ok {
		t.Error("enrollment must survive the unfollow")
	}
	if _, ok := f.relations.Lookup("alice", "notices", "email"); !ok {
		t.Error("other-channel subscription must survive the unfollow")
	}
	if _, ok := f.relations.Lookup("alice", "notices", "wechat_mp"); ok {
		t.Error("unfollowed subscription still registered")
	}
	// 全停: the filter refuses the whole channel now.
	filter := NewFilter(f.relations, f.surfaces)
	if filter.Allow("alice", "system", "wechat_mp") {
		t.Error("must-deliver enrollment still delivers on an invalid surface")
	}
}

func TestUnfollowIsIdempotentAndAttributed(t *testing.T) {
	f := newSourceFixture()
	if _, err := f.adapter.Follow("alice", "wechat_mp", "openid-1", SourceWeChatMP, []string{"notices"}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.adapter.Unfollow("alice", "wechat_mp", SourceBot); err != nil {
		t.Fatal(err)
	}
	// Second unfollow: nothing left to stop, still no error.
	res, err := f.adapter.Unfollow("alice", "wechat_mp", SourceBot)
	if err != nil {
		t.Fatalf("second unfollow: %v", err)
	}
	if res.SurfaceInvalidated || len(res.Terminated) != 0 {
		t.Errorf("second unfollow did work: %+v", res)
	}

	// Attribution: the terminate event names the relation's entry source
	// (wechat_mp) and the acting adapter in Detail (bot).
	var terminate *audit.Event
	for _, e := range f.trail.List() {
		if e.Kind == audit.RelationTerminate {
			terminate = &e
			break
		}
	}
	if terminate == nil {
		t.Fatal("no terminate event recorded")
	}
	if terminate.Source != SourceWeChatMP {
		t.Errorf("terminate event source = %q, want the relation's entry source %q", terminate.Source, SourceWeChatMP)
	}
	if terminate.Detail != "unsubscribe via bot" {
		t.Errorf("terminate detail = %q, want acting adapter", terminate.Detail)
	}
	var invalidate *audit.Event
	for i := range f.trail.List() {
		if f.trail.List()[i].Kind == audit.SurfaceInvalidate {
			invalidate = &f.trail.List()[i]
			break
		}
	}
	if invalidate == nil || invalidate.Detail != "unfollow via bot" {
		t.Fatalf("invalidate event = %+v, want detail naming the actor", invalidate)
	}
}

func TestUnfollowValidation(t *testing.T) {
	f := newSourceFixture()
	if _, err := f.adapter.Unfollow("bad id", "wechat_mp", SourceBot); err == nil {
		t.Error("bad audience id accepted")
	}
	if _, err := f.adapter.Unfollow("alice", "wechat_mp", ""); err == nil {
		t.Error("empty source accepted")
	}
}

func TestToggleSubscribesAndTerminates(t *testing.T) {
	f := newSourceFixture()
	if _, err := f.adapter.Toggle("alice", "notices", "email", SourcePreferenceCenter, true); err != nil {
		t.Fatalf("toggle on: %v", err)
	}
	if _, ok := f.relations.Lookup("alice", "notices", "email"); !ok {
		t.Fatal("toggle-on did not subscribe")
	}
	rel, err := f.adapter.Toggle("alice", "notices", "email", SourcePreferenceCenter, false)
	if err != nil {
		t.Fatalf("toggle off: %v", err)
	}
	if rel.Category != "notices" {
		t.Errorf("toggle-off returned %+v", rel)
	}
	if _, ok := f.relations.Lookup("alice", "notices", "email"); ok {
		t.Error("toggle-off left the relation registered")
	}
	// A must-deliver enrollment refuses the off toggle (the bottom line).
	if err := f.relations.Enroll(Relation{
		AudienceID: "alice", Category: "system", Channel: "email",
		Type: RelationEnrollment, Source: SourceAdmin, Policy: Policy{MustDeliver: true},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.adapter.Toggle("alice", "system", "email", SourcePreferenceCenter, false); err == nil {
		t.Error("toggle-off took down a must-deliver enrollment")
	}
	if _, err := f.adapter.Toggle("alice", "notices", "email", "", true); err == nil {
		t.Error("empty source accepted")
	}
}

func TestNewSourceAdapterNeedsBothRegistries(t *testing.T) {
	for name, fn := range map[string]func(){
		"nil surfaces":  func() { NewSourceAdapter(nil, NewRegistry()) },
		"nil relations": func() { NewSourceAdapter(NewSurfaceRegistry(), nil) },
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s: no panic", name)
				}
			}()
			fn()
		}()
	}
}

// TestAuditTrailCarriesFollowEntry: the bind event records the entry
// adapter, closing the §8 loop — every relation change is attributable.
func TestAuditTrailCarriesFollowEntry(t *testing.T) {
	f := newSourceFixture()
	if _, err := f.adapter.Follow("alice", "wechat_mp", "openid-1", SourceWeChatMP, []string{"notices"}); err != nil {
		t.Fatal(err)
	}
	var bind *audit.Event
	for _, e := range f.trail.List() {
		if e.Kind == audit.SurfaceBind {
			bind = &e
			break
		}
	}
	if bind == nil || bind.Detail != "follow via wechat_mp" {
		t.Fatalf("bind event = %+v, want entry detail", bind)
	}
	var sub *audit.Event
	for _, e := range f.trail.List() {
		if e.Kind == audit.RelationSubscribe {
			sub = &e
			break
		}
	}
	if sub == nil || sub.Source != SourceWeChatMP {
		t.Fatalf("subscribe event = %+v, want entry source", sub)
	}
}

// TestFollowRecordsRefusedDefaultGroups: a default group the registry
// refuses (here: oversized category) must not sink the follow — the
// surface bound, the good groups landed, the refusal is reported.
func TestFollowRecordsRefusedDefaultGroups(t *testing.T) {
	surfaces := NewSurfaceRegistry()
	relations := NewRegistry()
	a := NewSourceAdapter(surfaces, relations)
	oversized := strings.Repeat("x", 65)

	res, err := a.Follow("alice", "telegram", "777", SourceBot, []string{"notices", oversized})
	if err != nil {
		t.Fatal(err)
	}
	if surface, _ := surfaces.Surface("alice", "telegram"); surface.Status != SurfaceActive {
		t.Errorf("surface = %s, want active despite the refused group", surface.Status)
	}
	if len(res.Subscribed) != 1 || res.Subscribed[0].Category != "notices" {
		t.Errorf("subscribed = %+v, want only notices", res.Subscribed)
	}
	if len(res.Failed) != 1 || res.Failed[0].Category != oversized {
		t.Errorf("failed = %+v, want the oversized category reported", res.Failed)
	}
}
