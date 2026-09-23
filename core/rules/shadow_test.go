package rules

import "testing"

func TestShadowSamplerFirstHitAlwaysRecorded(t *testing.T) {
	s := NewDefaultShadowSampler()
	if !s.ShouldRecord("r1") {
		t.Fatal("first hit for a rule must be recorded")
	}
	if got := s.Count("r1"); got != 1 {
		t.Fatalf("expected count 1, got %d", got)
	}
}

func TestShadowSamplerInterval(t *testing.T) {
	s := NewShadowSampler(3)
	// Hits 1 and 3 are recorded (first + every interval-th); hit 2 is not.
	want := []bool{true, false, true, false, false, true}
	for i, w := range want {
		if got := s.ShouldRecord("r1"); got != w {
			t.Fatalf("hit %d: expected recorded=%v, got %v", i+1, w, got)
		}
	}
	if got := s.Count("r1"); got != 6 {
		t.Fatalf("expected total count 6, got %d", got)
	}
}

func TestShadowSamplerPerRule(t *testing.T) {
	s := NewShadowSampler(100)
	if !s.ShouldRecord("a") {
		t.Fatal("first hit for rule a must be recorded")
	}
	if !s.ShouldRecord("b") {
		t.Fatal("first hit for rule b must be recorded")
	}
	// Rule a's counter is independent of rule b's.
	if s.ShouldRecord("a") {
		t.Fatal("second hit for rule a must not be recorded at interval 100")
	}
	if got := s.Count("missing"); got != 0 {
		t.Fatalf("unknown rule expected count 0, got %d", got)
	}
}
