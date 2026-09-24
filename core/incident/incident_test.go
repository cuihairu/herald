package incident

import (
	"testing"
)

func TestStoreOpenIsIdempotentWhileRunning(t *testing.T) {
	s := New(0)
	first := s.Open(Opening{RuleID: "r-up", GroupKey: "g1", AlertID: "a1", Title: "disk full", Level: "critical", Channels: []string{"oncall"}})
	if first == nil || first.Status() != StatusOpen {
		t.Fatalf("expected an open incident, got %+v", first)
	}
	// A repeated delivery of the same alert refreshes the context but
	// keeps the episode.
	again := s.Open(Opening{RuleID: "r-up", GroupKey: "g1", AlertID: "a1", Title: "disk full (2)", Level: "error", Channels: []string{"oncall", "ops"}})
	if again.ID != first.ID {
		t.Fatalf("same alert must reuse the open episode, got %s vs %s", again.ID, first.ID)
	}
	if again.Title != "disk full (2)" {
		t.Fatalf("context must refresh, got %q", again.Title)
	}
	if again.Level != "error" {
		t.Fatalf("level must refresh, got %q", again.Level)
	}
	if len(again.Channels) != 2 || again.Channels[0] != "oncall" {
		t.Fatalf("channels must refresh, got %+v", again.Channels)
	}
	if got := s.List(nil); len(got) != 1 {
		t.Fatalf("expected one incident, got %d", len(got))
	}
}

func TestStoreOpenAfterResolveStartsNewEpisode(t *testing.T) {
	s := New(0)
	first := s.Open(Opening{RuleID: "r-up", GroupKey: "g1", AlertID: "a1", Title: "disk full"})
	if s.Resolve("r-up", "g1", 3) == nil {
		t.Fatal("expected the episode to resolve")
	}
	second := s.Open(Opening{RuleID: "r-up", GroupKey: "g1", AlertID: "a1", Title: "disk full"})
	if second.ID == first.ID {
		t.Fatal("a regression is a new episode, not a revival")
	}
	if second.Status() != StatusOpen {
		t.Fatalf("new episode must be open, got %s", second.Status())
	}
}

func TestStoreAckLifecycle(t *testing.T) {
	s := New(0)
	s.Open(Opening{RuleID: "r-up", GroupKey: "g1", AlertID: "a1", Title: "disk full"})

	// Ack with no open episode anywhere: fine, reports false.
	if s.Ack("never-seen", "alice") {
		t.Fatal("ack of an unknown alert must report false")
	}

	if !s.Ack("a1", "alice") {
		t.Fatal("expected the ack to land")
	}
	inc := s.List(&Filter{AlertID: "a1"})[0]
	if inc.Status() != StatusAcked || inc.AckedBy != "alice" {
		t.Fatalf("expected acked incident, got %+v", inc)
	}

	// A second ack keeps the first record (idempotent like the ack store).
	s.Ack("a1", "bob")
	if inc.AckedBy != "alice" {
		t.Fatalf("first ack must win, got %q", inc.AckedBy)
	}

	// Resolve closes it (addressed by the engine's group key).
	if s.Resolve("r-up", "g1", 5) == nil {
		t.Fatal("expected resolve")
	}
	if inc.Status() != StatusResolved || inc.Events != 5 {
		t.Fatalf("expected resolved with events, got %+v", inc)
	}
	if inc.Duration < 0 {
		t.Fatalf("duration must not be negative: %d", inc.Duration)
	}
	// A resolved episode is not re-resolvable or ack-able.
	if s.Resolve("r-up", "g1", 9) != nil {
		t.Fatal("resolved episode must not resolve twice")
	}
}

func TestStoreAppendTimeline(t *testing.T) {
	s := New(0)
	s.Open(Opening{RuleID: "r-up", GroupKey: "g1", AlertID: "a1", Title: "disk full"})
	s.Append("r-up", "a1", "escalation_fired", "no ack within 5m")
	inc := s.List(&Filter{AlertID: "a1"})[0]
	if len(inc.Timeline) != 1 || inc.Timeline[0].Kind != "escalation_fired" {
		t.Fatalf("expected a timeline entry, got %+v", inc.Timeline)
	}
	// Milestones for unknown episodes are dropped, not stored.
	s.Append("r-up", "ghost", "escalation_fired", "")
	if got := s.List(nil); len(got) != 1 || len(got[0].Timeline) != 1 {
		t.Fatalf("unknown episode must not create a record, got %+v", got)
	}
}

func TestStoreListFilters(t *testing.T) {
	s := New(0)
	s.Open(Opening{RuleID: "r1", AlertID: "a1", Title: "one"})
	s.Open(Opening{RuleID: "r2", AlertID: "a2", Title: "two"})
	s.Ack("a2", "alice")
	s.Open(Opening{RuleID: "r3", AlertID: "a3", Title: "three"})
	s.Resolve("r3", "", 1)

	if got := s.List(&Filter{Status: StatusOpen}); len(got) != 1 || got[0].AlertID != "a1" {
		t.Fatalf("open filter: %+v", got)
	}
	if got := s.List(&Filter{Status: StatusAcked}); len(got) != 1 || got[0].AlertID != "a2" {
		t.Fatalf("acked filter: %+v", got)
	}
	if got := s.List(&Filter{Status: StatusResolved}); len(got) != 1 || got[0].AlertID != "a3" {
		t.Fatalf("resolved filter: %+v", got)
	}
	if got := s.List(&Filter{RuleID: "r1"}); len(got) != 1 || got[0].AlertID != "a1" {
		t.Fatalf("rule filter: %+v", got)
	}
	if got := s.List(&Filter{AlertID: "a2"}); len(got) != 1 || got[0].AlertID != "a2" {
		t.Fatalf("alert filter: %+v", got)
	}
	if len(s.List(nil)) != 3 {
		t.Fatalf("unfiltered list: %+v", s.List(nil))
	}
	// Newest first.
	if got := s.List(nil); got[0].AlertID != "a3" {
		t.Fatalf("list must be newest-first, got %+v", got)
	}
}

func TestStoreEvictionKeepsOpenIncidents(t *testing.T) {
	s2 := New(2)
	s2.Open(Opening{RuleID: "r1", GroupKey: "g1", AlertID: "a-old", Title: "t"})
	s2.Resolve("r1", "g1", 1)
	s2.Open(Opening{RuleID: "r2", AlertID: "a-live", Title: "t"})
	s2.Open(Opening{RuleID: "r2", AlertID: "a-x", Title: "t"})
	s2.Open(Opening{RuleID: "r2", AlertID: "a-y", Title: "t"})
	got := s2.List(nil)
	for _, inc := range got {
		if inc.AlertID == "a-live" {
			return // open episode survived the overflow
		}
	}
	t.Fatalf("open incident must survive eviction, got %+v", got)
}

func TestStoreOpenWithEmptyIdentity(t *testing.T) {
	s := New(0)
	if s.Open(Opening{RuleID: "r1"}) != nil {
		t.Fatal("empty alert id must not open an incident")
	}
	if s.Open(Opening{AlertID: "a1"}) != nil {
		t.Fatal("empty rule id must not open an incident")
	}
	if len(s.List(nil)) != 0 {
		t.Fatal("store must stay empty")
	}
}

func TestStoreGetUnknown(t *testing.T) {
	s := New(0)
	if s.Get("nope") != nil {
		t.Fatal("unknown id must return nil")
	}
}

func TestStoreGetReturnsTheEpisode(t *testing.T) {
	s := New(0)
	opened := s.Open(Opening{RuleID: "r-up", GroupKey: "g1", AlertID: "a1", Title: "disk full"})
	got := s.Get(opened.ID)
	if got == nil || got.AlertID != "a1" || got.Status() != StatusOpen {
		t.Fatalf("Get must return the tracked episode, got %+v", got)
	}
}

func TestStoreAppendToReachesResolvedEpisodes(t *testing.T) {
	s := New(0)
	opened := s.Open(Opening{RuleID: "r-up", GroupKey: "g1", AlertID: "a1", Title: "disk full"})
	if s.Resolve("r-up", "g1", 2) == nil {
		t.Fatal("expected resolve")
	}
	// Append is for open episodes and no longer reaches it; AppendTo is
	// how a resolution-time milestone (a summary that failed to deliver)
	// lands on the closed episode.
	s.Append("r-up", "a1", "late", "")
	s.AppendTo(opened.ID, "resolve_delivery_failed", "ghost: no provider")
	inc := s.Get(opened.ID)
	if len(inc.Timeline) != 1 || inc.Timeline[0].Kind != "resolve_delivery_failed" {
		t.Fatalf("expected only the AppendTo milestone, got %+v", inc.Timeline)
	}
	// Unknown ids are ignored.
	s.AppendTo("ghost", "x", "")
	if len(s.Get(opened.ID).Timeline) != 1 {
		t.Fatalf("unknown id must not append, got %+v", inc.Timeline)
	}
}
