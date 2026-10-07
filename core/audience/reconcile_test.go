package audience

import (
	"context"
	"errors"
	"testing"

	"github.com/cuihairu/herald/core/audit"
)

// fakeProbe answers from a fixed table; anything not in the table errors,
// so tests see the difference between "no" and "cannot ask".
type fakeProbe struct {
	channel string
	answers map[string]bool
}

func (p *fakeProbe) Channel() string { return p.channel }

func (p *fakeProbe) Valid(_ context.Context, target string) (bool, error) {
	v, ok := p.answers[target]
	if !ok {
		return false, errors.New("probe offline")
	}
	return v, nil
}

func reconcileFixture(probes ...SurfaceProbe) (*Reconciler, *SurfaceRegistry, *Registry, *audit.Store) {
	surfaces := NewSurfaceRegistry()
	relations := NewRegistry()
	trail := audit.New(0)
	surfaces.SetRecorder(trail)
	relations.SetRecorder(trail)
	return NewReconciler(surfaces, relations, probes...), surfaces, relations, trail
}

func TestReconcileCorrectsDeadHandles(t *testing.T) {
	probe := &fakeProbe{channel: "wechat_mp", answers: map[string]bool{"openid-live": true, "openid-dead": false}}
	r, surfaces, relations, trail := reconcileFixture(probe)
	adapter := NewSourceAdapter(surfaces, relations)
	if _, err := adapter.Follow("alice", "wechat_mp", "openid-live", SourceWeChatMP, []string{"notices"}); err != nil {
		t.Fatal(err)
	}
	if _, err := adapter.Follow("bob", "wechat_mp", "openid-dead", SourceWeChatMP, []string{"notices"}); err != nil {
		t.Fatal(err)
	}

	report := r.RunOnce(context.Background())
	if report.Checked != 2 || report.Corrected != 1 || report.Skipped != 0 || len(report.Errors) != 0 {
		t.Fatalf("report = %+v, want checked 2 corrected 1", report)
	}
	surface, _ := surfaces.Surface("bob", "wechat_mp")
	if surface.Status != SurfaceInvalid {
		t.Errorf("bob surface = %s, want invalid", surface.Status)
	}
	if _, ok := relations.Lookup("bob", "notices", "wechat_mp"); ok {
		t.Error("dead handle's subscription survived reconciliation")
	}
	if _, ok := relations.Lookup("alice", "notices", "wechat_mp"); !ok {
		t.Error("live handle's subscription was swept")
	}
	// Corrections are attributed: terminate detail names the reconcile actor.
	for _, e := range trail.List() {
		if e.Kind == audit.RelationTerminate && e.AudienceID == "bob" {
			if e.Detail != "unsubscribe via "+ReconcileSource {
				t.Errorf("terminate detail = %q, want reconcile actor", e.Detail)
			}
		}
		if e.Kind == audit.SurfaceInvalidate && e.AudienceID == "bob" {
			if e.Detail == "" {
				t.Error("invalidate detail empty — the audit cannot answer why")
			}
		}
	}
}

func TestReconcileSkipsBlindSpotsAndErrors(t *testing.T) {
	probe := &fakeProbe{channel: "wechat_mp", answers: map[string]bool{}} // everything errors
	tgProbe := &fakeProbe{channel: "telegram", answers: map[string]bool{"chat-1": true}}
	r, surfaces, relations, _ := reconcileFixture(probe, tgProbe)
	if _, err := NewSourceAdapter(surfaces, relations).Follow("alice", "wechat_mp", "openid-x", SourceWeChatMP, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := NewSourceAdapter(surfaces, relations).Follow("bob", "telegram", "chat-1", SourceBot, nil); err != nil {
		t.Fatal(err)
	}
	// A pending surface (rebind parked) must not be probed.
	tok, err := surfaces.IssueBinding("bob", "telegram")
	if err != nil {
		t.Fatal(err)
	}
	if res, err := surfaces.RedeemBinding(tok, "chat-9"); err != nil || res.Outcome != OutcomePendingConfirm {
		t.Fatalf("park: (%+v, %v)", res, err)
	}

	report := r.RunOnce(context.Background())
	if report.Skipped != 1 || len(report.Errors) != 1 {
		t.Fatalf("report = %+v, want one skipped probe error", report)
	}
	if report.Checked != 1 {
		t.Errorf("checked = %d, want 1 (pending surface never asked)", report.Checked)
	}
	if surface, _ := surfaces.Surface("alice", "wechat_mp"); surface.Status != SurfaceActive {
		t.Error("a blind probe must not correct anything")
	}
}

func TestReconcileChannelsStableOrder(t *testing.T) {
	r, _, _, _ := reconcileFixture(
		&fakeProbe{channel: "wechat_mp", answers: nil},
		&fakeProbe{channel: "telegram", answers: nil},
	)
	got := r.Channels()
	if len(got) != 2 || got[0] != "telegram" || got[1] != "wechat_mp" {
		t.Fatalf("Channels() = %v, want sorted", got)
	}
}

func TestReconcileHonorsContextCancellation(t *testing.T) {
	probe := &fakeProbe{channel: "wechat_mp", answers: map[string]bool{"openid-1": true}}
	r, surfaces, relations, _ := reconcileFixture(probe)
	if _, err := NewSourceAdapter(surfaces, relations).Follow("alice", "wechat_mp", "openid-1", SourceWeChatMP, nil); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	report := r.RunOnce(ctx)
	if len(report.Errors) == 0 {
		t.Fatal("canceled sweep reported no error")
	}
	if report.Corrected != 0 {
		t.Errorf("canceled sweep corrected %d surfaces", report.Corrected)
	}
}

func TestNewReconcilerNeedsBothRegistries(t *testing.T) {
	for name, fn := range map[string]func(){
		"nil surfaces":  func() { NewReconciler(nil, NewRegistry()) },
		"nil relations": func() { NewReconciler(NewSurfaceRegistry(), nil) },
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

func TestReconcileSnapshotSkipsInactiveAndUnprobed(t *testing.T) {
	probe := &fakeProbe{channel: "wechat_mp", answers: map[string]bool{"openid-live": true}}
	r, surfaces, relations, _ := reconcileFixture(probe)
	adapter := NewSourceAdapter(surfaces, relations)
	if _, err := adapter.Follow("alice", "wechat_mp", "openid-live", SourceWeChatMP, nil); err != nil {
		t.Fatal(err)
	}
	// bob's MP surface is already invalid (unfollowed): already stopped,
	// the sweep must not ask the platform about it again.
	if _, err := adapter.Follow("bob", "wechat_mp", "openid-gone", SourceWeChatMP, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := adapter.Unfollow("bob", "wechat_mp", SourceWeChatMP); err != nil {
		t.Fatal(err)
	}
	// carol holds an active surface on a channel with no probe: not asked.
	if _, err := adapter.Follow("carol", "email", "carol@example.com", SourcePreferenceCenter, nil); err != nil {
		t.Fatal(err)
	}

	report := r.RunOnce(context.Background())
	if report.Checked != 1 || report.Skipped != 0 || len(report.Errors) != 0 {
		t.Fatalf("report = %+v, want exactly alice asked", report)
	}
	if surface, _ := surfaces.Surface("bob", "wechat_mp"); surface.Status != SurfaceInvalid {
		t.Error("the inactive surface was probed or resurrected")
	}
	if surface, _ := surfaces.Surface("carol", "email"); surface.Status != SurfaceActive {
		t.Error("the unprobed surface was touched")
	}
}

func TestReconcileSweepSparesOtherChannels(t *testing.T) {
	probe := &fakeProbe{channel: "wechat_mp", answers: map[string]bool{"openid-dead": false}}
	r, surfaces, relations, _ := reconcileFixture(probe)
	adapter := NewSourceAdapter(surfaces, relations)
	if _, err := adapter.Follow("bob", "wechat_mp", "openid-dead", SourceWeChatMP, []string{"notices"}); err != nil {
		t.Fatal(err)
	}
	// A subscription on a channel the correction is not about must
	// survive the sweep — only the dead handle's channel 全停.
	if err := relations.Subscribe(Relation{AudienceID: "bob", Category: "bills", Channel: "email", Type: RelationSubscription, Source: SourcePreferenceCenter}); err != nil {
		t.Fatal(err)
	}

	report := r.RunOnce(context.Background())
	if report.Corrected != 1 {
		t.Fatalf("report = %+v, want one correction", report)
	}
	if _, ok := relations.Lookup("bob", "notices", "wechat_mp"); ok {
		t.Error("the dead channel's subscription survived")
	}
	if _, ok := relations.Lookup("bob", "bills", "email"); !ok {
		t.Error("the email subscription was swept by the MP correction")
	}
}
