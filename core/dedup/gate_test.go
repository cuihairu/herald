package dedup

import (
	"strconv"
	"testing"
	"time"
)

func TestGateEventIdempotency(t *testing.T) {
	// Always-tier gate: the idempotency layer must be observable on its
	// own, without the content stage interfering.
	g := NewGate(time.Minute, func(string) Tier { return TierAlways })

	if out := g.Decide(Event{EventID: "evt-1", Key: "k", Category: "alerts"}); out.Decision != Pass {
		t.Fatalf("first delivery = %v, want pass", out)
	}
	// Same event id, drifted content: still the same event — dropped.
	out := g.Decide(Event{EventID: "evt-1", Key: "other", Category: "alerts"})
	if out.Decision != Suppress || out.Reason != ReasonIdempotent {
		t.Fatalf("repeat = %v/%v, want suppress/idempotent", out.Decision, out.Reason)
	}
	if out.Count != 1 {
		t.Errorf("count = %d, want 1", out.Count)
	}
	// The idempotent ledger folds under the event's own namespace, not
	// the content key.
	if f, ok := g.Fold("event:evt-1"); !ok || f.Count != 1 {
		t.Errorf("fold under event id = %+v ok=%v, want count 1", f, ok)
	}
	// A fresh id passes even on the same content key.
	if out := g.Decide(Event{EventID: "evt-2", Key: "k", Category: "alerts"}); out.Decision != Pass {
		t.Fatalf("fresh id = %v, want pass", out)
	}
}

func TestGateStateMachine(t *testing.T) {
	g := NewGate(time.Minute, nil)

	// First sighting fires.
	if out := g.Decide(Event{Key: "node-17", State: "down", Category: "alerts"}); out.Decision != Pass {
		t.Fatalf("first down = %v, want pass", out)
	}
	// Repeat of the current state folds.
	out := g.Decide(Event{Key: "node-17", State: "down", Category: "alerts", Fingerprint: "n-2"})
	if out.Decision != Suppress || out.Reason != ReasonStateRepeat || out.Count != 1 {
		t.Fatalf("repeat down = %v/%v/%d, want suppress/state-repeat/1", out.Decision, out.Reason, out.Count)
	}
	// Flip fires and closes the episode.
	if out := g.Decide(Event{Key: "node-17", State: "ok", Category: "alerts"}); out.Decision != Pass {
		t.Fatalf("recovery = %v, want pass", out)
	}
	if f, ok := g.Fold("node-17"); ok && f.Count != 0 {
		t.Errorf("fold after flip = %+v, want episode closed", f)
	}
	// And the wheel comes round: down again fires, a repeat folds with a
	// fresh count.
	if out := g.Decide(Event{Key: "node-17", State: "down", Category: "alerts"}); out.Decision != Pass {
		t.Fatalf("down again = %v, want pass", out)
	}
	out = g.Decide(Event{Key: "node-17", State: "down", Category: "alerts"})
	if out.Decision != Suppress || out.Count != 1 {
		t.Fatalf("repeat after flip = %v/%d, want suppress with fresh count 1", out.Decision, out.Count)
	}
}

func TestGateTiers(t *testing.T) {
	t.Run("always delivers every copy", func(t *testing.T) {
		g := NewGate(time.Minute, func(string) Tier { return TierAlways })
		for i := 0; i < 3; i++ {
			if out := g.Decide(Event{Key: "audit-x", Category: "critical-audit"}); out.Decision != Pass {
				t.Fatalf("copy %d = %v, want pass", i, out)
			}
		}
	})

	t.Run("once delivers exactly once", func(t *testing.T) {
		g := NewGate(time.Minute, nil)
		if out := g.Decide(Event{Key: "deploy-9", Category: "system"}); out.Decision != Pass {
			t.Fatalf("first = %v, want pass", out)
		}
		out := g.Decide(Event{Key: "deploy-9", Category: "system"})
		if out.Decision != Suppress || out.Reason != ReasonOnce || out.Count != 1 {
			t.Fatalf("second = %v/%v/%d, want suppress/once/1", out.Decision, out.Reason, out.Count)
		}
	})

	t.Run("throttle honours the window", func(t *testing.T) {
		g := NewGate(30*time.Millisecond, nil)
		if out := g.Decide(Event{Key: "alert-a", Category: "alerts"}); out.Decision != Pass {
			t.Fatalf("first = %v, want pass", out)
		}
		out := g.Decide(Event{Key: "alert-a", Category: "alerts", Fingerprint: "n-2"})
		if out.Decision != Suppress || out.Reason != ReasonThrottled || out.Count != 1 {
			t.Fatalf("within window = %v/%v/%d, want suppress/throttled/1", out.Decision, out.Reason, out.Count)
		}
		time.Sleep(40 * time.Millisecond)
		if out := g.Decide(Event{Key: "alert-a", Category: "alerts"}); out.Decision != Pass {
			t.Fatalf("after window = %v, want pass", out)
		}
		// The expired episode closed: the next suppression counts fresh.
		out = g.Decide(Event{Key: "alert-a", Category: "alerts"})
		if out.Decision != Suppress || out.Count != 1 {
			t.Fatalf("re-fold = %v/%d, want suppress with fresh count 1", out.Decision, out.Count)
		}
	})
}

func TestGateFoldLedger(t *testing.T) {
	g := NewGate(time.Minute, func(string) Tier { return TierThrottle })
	for i := 1; i <= 3; i++ {
		g.Decide(Event{Key: "hot", Category: "alerts"}) // first pass
		g.Decide(Event{Key: "hot", Category: "alerts", Fingerprint: "copy"})
		_ = i
	}
	// Only the first Decide passes; the other five fold.
	f, ok := g.Fold("hot")
	if !ok || f.Count != 5 {
		t.Fatalf("fold = %+v ok=%v, want count 5", f, ok)
	}
	if len(f.Events) != 5 || f.Events[0] != "copy" {
		t.Errorf("retained originals = %v, want five 'copy' fingerprints", f.Events)
	}
	if _, ok := g.Fold("missing"); ok {
		t.Error("unknown key folded")
	}
}

func TestGateFoldRetainsOriginalsUpToCap(t *testing.T) {
	g := NewGate(time.Minute, func(string) Tier { return TierAlways })
	g.folds["k"] = &Fold{}
	// The seed delivery, then more repeats than the expansion cap holds.
	g.Decide(Event{Key: "k", State: "down", Category: "x", Fingerprint: "seed"})
	for i := 0; i < foldEventCap+7; i++ {
		g.Decide(Event{Key: "k", State: "down", Fingerprint: "f"})
	}
	f, _ := g.Fold("k")
	if f.Count != foldEventCap+7 {
		t.Errorf("count = %d, want %d (cap only truncates the expansion)", f.Count, foldEventCap+7)
	}
	if len(f.Events) != foldEventCap {
		t.Errorf("retained = %d, want capped at %d", len(f.Events), foldEventCap)
	}
}

func TestGateOverrides(t *testing.T) {
	g := NewGate(0, nil)
	if g.Window() != 5*time.Minute {
		t.Errorf("window = %v, want default 5m", g.Window())
	}
	// nil tier resolver falls back to the §11.2 category table: system
	// is once.
	if out := g.Decide(Event{Key: "k", Category: "system"}); out.Decision != Pass {
		t.Fatalf("system first = %v, want pass", out)
	}
	if out := g.Decide(Event{Key: "k", Category: "system"}); out.Decision != Suppress {
		t.Errorf("system repeat = %v, want suppress (once tier)", out)
	}
}

func TestGateEvictsOldestPastCap(t *testing.T) {
	g := NewGate(time.Minute, nil)
	n := gateCap + 4
	// Stateful events: passes fill the state and event tables, the
	// immediate repeat folds into the ledger.
	for i := 0; i < n; i++ {
		g.Decide(Event{EventID: idOf("e", i), Key: idOf("k", i), State: "down", Category: "alerts"})
		g.Decide(Event{EventID: idOf("e2", i), Key: idOf("k", i), State: "down", Category: "alerts"})
	}
	// Throttle passes fill the stamp table; once passes fill the once table.
	for i := 0; i < n; i++ {
		g.Decide(Event{Key: idOf("t", i), Category: "alerts"})
		g.Decide(Event{Key: idOf("o", i), Category: "system"})
	}
	// Everything overflowed; the oldest entries must have been evicted —
	// the oldest stateful key passes again as a first sighting, and the
	// oldest event id delivers again.
	if out := g.Decide(Event{Key: idOf("k", 0), State: "down", Category: "alerts"}); out.Decision != Pass {
		t.Errorf("oldest state key = %v, want pass after eviction", out)
	}
	// The evicted event id lost its guarantee: the same id delivers again.
	if out := g.Decide(Event{EventID: idOf("e", 0), Key: idOf("t", 0) + "-late", Category: "alerts"}); out.Decision != Pass {
		t.Errorf("oldest event id = %v, want pass after eviction", out)
	}
	if out := g.Decide(Event{Key: idOf("o", 0), Category: "system"}); out.Decision != Pass {
		t.Errorf("oldest once key = %v, want pass after eviction", out)
	}
}

func idOf(prefix string, i int) string {
	return prefix + "-" + strconv.Itoa(i)
}

func TestGateOperatorOverrides(t *testing.T) {
	d := NewDedup(&Config{
		Window:          time.Minute,
		CategoryTiers:   map[string]string{"bills": "always", "marketing": "once", "broken": "hourly"},
		CategoryWindows: map[string]time.Duration{"alerts": 10 * time.Millisecond, "bills": -time.Second},
	})
	g := d.Gate()

	// bills re-graded always: every copy passes.
	for i := 0; i < 3; i++ {
		if out := g.Decide(Event{Key: "b-1", Category: "bills"}); out.Decision != Pass {
			t.Fatalf("bills copy %d = %v, want pass (always override)", i, out)
		}
	}
	// marketing re-graded once: second folds.
	if out := g.Decide(Event{Key: "m-1", Category: "marketing"}); out.Decision != Pass {
		t.Fatalf("marketing first = %v, want pass", out)
	}
	if out := g.Decide(Event{Key: "m-1", Category: "marketing"}); out.Reason != ReasonOnce {
		t.Errorf("marketing repeat = %v, want %v", out.Reason, ReasonOnce)
	}
	// Unparseable tier falls back to the resolver (alerts → default
	// throttle), not to a guess.
	if out := g.Decide(Event{Key: "x-1", Category: "broken"}); out.Decision != Pass {
		t.Fatalf("broken-tier first = %v, want pass via default throttle", out)
	}
	if out := g.Decide(Event{Key: "x-1", Category: "broken"}); out.Reason != ReasonThrottled {
		t.Errorf("broken-tier repeat = %v, want %v", out.Reason, ReasonThrottled)
	}
	// Per-category window: alerts re-stamps after 10ms, not the base
	// minute — while broken keeps the base (no override for it).
	if out := g.Decide(Event{Key: "a-1", Category: "alerts"}); out.Decision != Pass {
		t.Fatalf("alerts first = %v, want pass", out)
	}
	if out := g.Decide(Event{Key: "a-1", Category: "alerts"}); out.Reason != ReasonThrottled {
		t.Fatalf("alerts repeat = %v, want throttled", out.Reason)
	}
	time.Sleep(15 * time.Millisecond)
	if out := g.Decide(Event{Key: "a-1", Category: "alerts"}); out.Decision != Pass {
		t.Errorf("alerts after override window = %v, want pass", out)
	}
	// Negative window entries are ignored — the base window holds.
	g2 := d.Gate()
	if out := g2.Decide(Event{Key: "b-2", Category: "bills"}); out.Decision != Pass {
		t.Fatalf("bills 1 = %v", out)
	}
	if out := g2.Decide(Event{Key: "b-2", Category: "bills"}); out.Decision != Pass {
		t.Errorf("bills 2 = %v, want pass (always tier ignores windows)", out)
	}
	_ = g2.Decide(Event{Key: "w-1", Category: "notices"})
	if out := g2.Decide(Event{Key: "w-1", Category: "notices"}); out.Reason != ReasonThrottled {
		t.Errorf("base-window category = %v, want throttled within base minute", out.Reason)
	}
}
