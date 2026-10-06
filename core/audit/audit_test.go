package audit

import (
	"testing"
	"time"
)

func TestRecordStampsTimeAndCap(t *testing.T) {
	s := New(3)
	base := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	s.SetClock(func() time.Time { return base })
	for i := 0; i < 5; i++ {
		s.Record(Event{Kind: RelationSubscribe, AudienceID: "alice"})
	}
	all := s.List()
	if len(all) != 3 {
		t.Fatalf("List() len = %d, want 3 after the FIFO cap", len(all))
	}
	if all[0].At != base {
		t.Errorf("At = %v, want the injected clock stamp", all[0].At)
	}

	// An explicit At wins over the clock.
	explicit := base.Add(time.Hour)
	s.Record(Event{Kind: RelationEnroll, AudienceID: "bob", At: explicit})
	last := s.List()[2]
	if last.At != explicit {
		t.Errorf("At = %v, want the explicit timestamp kept", last.At)
	}
}

func TestListByRelationTypeAndAudience(t *testing.T) {
	s := New(0)
	s.Record(Event{Kind: RelationSubscribe, AudienceID: "alice", RelationType: "subscription"})
	s.Record(Event{Kind: RelationEnroll, AudienceID: "alice", RelationType: "enrollment"})
	s.Record(Event{Kind: SurfaceBind, AudienceID: "alice"})
	s.Record(Event{Kind: RelationSubscribe, AudienceID: "bob", RelationType: "subscription"})

	if got := s.ListByRelationType("subscription"); len(got) != 2 {
		t.Errorf("ListByRelationType(subscription) = %d, want 2", len(got))
	}
	if got := s.ListByRelationType("enrollment"); len(got) != 1 {
		t.Errorf("ListByRelationType(enrollment) = %d, want 1", len(got))
	}
	if got := s.ListByAudience("alice"); len(got) != 3 {
		t.Errorf("ListByAudience(alice) = %d, want 3", len(got))
	}
	if got := s.ListByAudience("ghost"); len(got) != 0 {
		t.Errorf("ListByAudience(unknown) = %d, want 0", len(got))
	}
}

func TestChronologicalOrder(t *testing.T) {
	s := New(0)
	base := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	s.Record(Event{Kind: RelationSubscribe, AudienceID: "a", RelationType: "subscription", At: base.Add(2 * time.Hour)})
	s.Record(Event{Kind: RelationEnroll, AudienceID: "a", RelationType: "enrollment", At: base})
	all := s.List()
	if !all[0].At.Before(all[1].At) {
		t.Errorf("List() not chronological: %v then %v", all[0].At, all[1].At)
	}
	byType := s.ListByRelationType("enrollment")
	if len(byType) != 1 || !byType[0].At.Equal(base) {
		t.Errorf("ListByRelationType = %+v, want the single earliest event", byType)
	}
}
