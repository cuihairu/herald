package rules

import "testing"

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
