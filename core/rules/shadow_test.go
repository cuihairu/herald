package rules

import (
	"fmt"
	"testing"
	"time"
)

func TestShadowSamplerFirstHitAlwaysRecorded(t *testing.T) {
	s := NewDefaultShadowSampler()
	if !s.ShouldRecord(KindShadow, "r1") {
		t.Fatal("first hit for a rule must be recorded")
	}
	if got := s.Count(KindShadow, "r1"); got != 1 {
		t.Fatalf("expected count 1, got %d", got)
	}
}

func TestShadowSamplerInterval(t *testing.T) {
	s := NewShadowSampler(3)
	// Hits 1 and 3 are recorded (first + every interval-th); hit 2 is not.
	want := []bool{true, false, true, false, false, true}
	for i, w := range want {
		if got := s.ShouldRecord(KindShadow, "r1"); got != w {
			t.Fatalf("hit %d: expected recorded=%v, got %v", i+1, w, got)
		}
	}
	if got := s.Count(KindShadow, "r1"); got != 6 {
		t.Fatalf("expected total count 6, got %d", got)
	}
}

func TestShadowSamplerPerRule(t *testing.T) {
	s := NewShadowSampler(100)
	if !s.ShouldRecord(KindShadow, "a") {
		t.Fatal("first hit for rule a must be recorded")
	}
	if !s.ShouldRecord(KindShadow, "b") {
		t.Fatal("first hit for rule b must be recorded")
	}
	// Rule a's counter is independent of rule b's.
	if s.ShouldRecord(KindShadow, "a") {
		t.Fatal("second hit for rule a must not be recorded at interval 100")
	}
	if got := s.Count(KindShadow, "missing"); got != 0 {
		t.Fatalf("unknown rule expected count 0, got %d", got)
	}
}

// The sampling budget is per (kind, rule): withheld events are the
// high-volume ones, and if they shared the counter with shadow hits the
// shadow sample stream — the only preview of what activation would send —
// would be starved exactly when traffic is heavy.
func TestShadowSamplerKindsAreIndependent(t *testing.T) {
	s := NewShadowSampler(100)

	// Rule "r" is suppressed 200 times: its first entry is recorded, the
	// rest are sampled away.
	for i := 0; i < 200; i++ {
		s.ShouldRecord(KindSuppressed, "r")
	}
	if got := s.Count(KindSuppressed, "r"); got != 200 {
		t.Fatalf("expected 200 suppressions counted, got %d", got)
	}

	// The shadow hits of the same rule still get their own first entry.
	if !s.ShouldRecord(KindShadow, "r") {
		t.Fatal("a rule's first shadow hit must be recorded regardless of its suppression count")
	}
	if got := s.Count(KindShadow, "r"); got != 1 {
		t.Fatalf("expected shadow count 1, got %d", got)
	}
	// And a different kind's counter is not the shadow counter either.
	if got := s.Count(KindSilenced, "r"); got != 0 {
		t.Fatalf("expected no silenced hits, got %d", got)
	}
}

func TestShadowSamplerZeroIntervalRecordsEverything(t *testing.T) {
	// A zero interval disables sampling: every hit is a full log entry.
	s := NewShadowSampler(0)
	for i := 1; i <= 5; i++ {
		if !s.ShouldRecord(KindShadow, "r") {
			t.Fatalf("hit %d must be recorded with no sampling", i)
		}
	}
}

// clockSampler returns a sampler whose clock the test drives through *at,
// so buckets, windows and sample timestamps share one controllable base.
func clockSampler() (*ShadowSampler, *time.Time) {
	s := NewShadowSampler(0)
	at := time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return at }
	return s, &at
}

func TestShadowSamplerWindowCounts(t *testing.T) {
	s, at := clockSampler()
	// Three buckets — 30h, 2h and 0h behind the query hour — with
	// 4+3+2 shadow hits; a silenced hit must not leak into them.
	for i := 0; i < 4; i++ {
		s.ShouldRecord(KindShadow, "r1")
	}
	*at = at.Add(28 * time.Hour)
	for i := 0; i < 3; i++ {
		s.ShouldRecord(KindShadow, "r1")
	}
	*at = at.Add(2 * time.Hour)
	for i := 0; i < 2; i++ {
		s.ShouldRecord(KindShadow, "r1")
	}
	s.ShouldRecord(KindSilenced, "r1")

	if got := s.Count(KindShadow, "r1"); got != 9 {
		t.Fatalf("total = %d, want 9", got)
	}
	if got := s.WindowCount(KindShadow, "r1", 24*time.Hour); got != 5 {
		t.Errorf("24h window = %d, want 5 (the 2h and 0h buckets)", got)
	}
	if got := s.WindowCount(KindShadow, "r1", 7*24*time.Hour); got != 9 {
		t.Errorf("7d window = %d, want 9", got)
	}
	// Sub-hour windows read the current bucket only.
	if got := s.WindowCount(KindShadow, "r1", 30*time.Minute); got != 2 {
		t.Errorf("30m window = %d, want 2", got)
	}

	// Eight days on: buckets past the keep window are pruned on write, so
	// the 7d window restarts from the fresh bucket while the exact total
	// keeps accumulating.
	*at = at.Add(8 * 24 * time.Hour)
	s.ShouldRecord(KindShadow, "r1")
	if got := s.Count(KindShadow, "r1"); got != 10 {
		t.Errorf("total after prune = %d, want 10", got)
	}
	if got := s.WindowCount(KindShadow, "r1", 7*24*time.Hour); got != 1 {
		t.Errorf("7d window after prune = %d, want 1", got)
	}
}

func TestShadowSamplerSamples(t *testing.T) {
	s, at := clockSampler()

	// 22 samples on one rule: the ring keeps the latest 20.
	for i := 0; i < 22; i++ {
		*at = at.Add(time.Minute)
		s.AddSample("r1", ShadowSample{Type: "alert", Level: "error", Title: fmt.Sprintf("t%d", i)})
	}
	stats := s.Stats("r1")
	if len(stats.Samples) != DefaultShadowSampleKeep {
		t.Fatalf("kept %d samples, want %d", len(stats.Samples), DefaultShadowSampleKeep)
	}
	// Newest first: the last two added lead the list.
	if stats.Samples[0].Title != "t21" || stats.Samples[1].Title != "t20" {
		t.Errorf("newest-first order broken: %q, %q", stats.Samples[0].Title, stats.Samples[1].Title)
	}
	for _, sm := range stats.Samples {
		if sm.At.IsZero() {
			t.Fatal("sample timestamps must come from the sampler clock")
		}
	}
	// The returned slice is a copy: mutating it must not touch the ring.
	stats.Samples[0].Title = "mutated"
	if got := s.Stats("r1").Samples[0].Title; got != "t21" {
		t.Errorf("Stats must return a defensive copy, got %q", got)
	}

	// An untouched rule reports the zero stats with no sample list.
	if got := s.Stats("r2"); got.Total != 0 || got.Last24h != 0 || got.Last7d != 0 || got.Samples != nil {
		t.Errorf("untouched rule stats = %+v, want zero value", got)
	}
}
