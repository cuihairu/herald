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

func TestListBySource(t *testing.T) {
	store := New(0)
	store.Record(Event{Kind: DeliveryDeduped, Source: "app:demo-app", Category: "alerts", Detail: "throttle: k (×2)"})
	store.Record(Event{Kind: RelationSubscribe, Source: "bot", AudienceID: "alice"})
	store.Record(Event{Kind: DeliveryDeduped, Source: "app:app-b", Category: "billing"})
	// A second demo-app event recorded out of chronological order: the
	// trail must come back sorted by At regardless of write order.
	earlier := time.Now().Add(-2 * time.Hour)
	store.Record(Event{At: earlier, Kind: RelationSubscribe, Source: "app:demo-app", AudienceID: "bob"})

	all := store.ListBySource("app:demo-app", time.Time{})
	if len(all) != 2 {
		t.Fatalf("demo-app trail: want 2 events, got %v", all)
	}
	if !all[0].At.Before(all[1].At) {
		t.Fatalf("trail order: want chronological, got %v then %v", all[0].At, all[1].At)
	}
	demoApp := store.ListBySource("app:demo-app", time.Now().Add(-time.Hour))
	if len(demoApp) != 1 || demoApp[0].Category != "alerts" {
		t.Fatalf("demo-app trail after past: want its own late event, got %v", demoApp)
	}

	cutoff := time.Now().Add(time.Hour)
	if got := store.ListBySource("app:demo-app", cutoff); len(got) != 0 {
		t.Fatalf("since filter: want 0 events after cutoff, got %v", got)
	}
	past := time.Now().Add(-time.Hour)
	if got := store.ListBySource("app:demo-app", past); len(got) != 1 {
		t.Fatalf("since filter: want the event after past, got %v", got)
	}
	if got := store.ListBySource("app:ghost", time.Time{}); len(got) != 0 {
		t.Fatalf("unknown source: want empty, got %v", got)
	}
}
