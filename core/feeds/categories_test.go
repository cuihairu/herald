package feeds

import (
	"testing"
	"time"
)

func TestCategoriesReturnsKnownCategories(t *testing.T) {
	s := NewStore(0)
	base := time.Date(2026, 11, 5, 9, 0, 0, 0, time.UTC)
	_ = s.Add(Item{ID: "a", Category: "alerts", Title: "t", PublishedAt: base})
	_ = s.Add(Item{ID: "b", Category: "deploy", Title: "t", PublishedAt: base})
	_ = s.Add(Item{ID: "c", Category: "alerts", Title: "t", AudienceID: "alice", PublishedAt: base})

	cats := s.Categories()
	if len(cats) != 2 || cats[0] != "alerts" || cats[1] != "deploy" {
		t.Fatalf("expected sorted [alerts deploy], got %v", cats)
	}
	if cats := NewStore(0).Categories(); len(cats) != 0 {
		t.Fatalf("expected no categories on an empty store, got %v", cats)
	}
}

func TestCountReturnsCategoryLogSize(t *testing.T) {
	s := NewStore(0)
	base := time.Date(2026, 11, 5, 9, 0, 0, 0, time.UTC)
	_ = s.Add(Item{ID: "a", Category: "alerts", Title: "t", PublishedAt: base})
	_ = s.Add(Item{ID: "b", Category: "alerts", Title: "t", AudienceID: "alice", PublishedAt: base})

	if n := s.Count("alerts"); n != 2 {
		t.Fatalf("expected 2 items in alerts, got %d", n)
	}
	if n := s.Count("ghost"); n != 0 {
		t.Fatalf("expected 0 for an unknown category, got %d", n)
	}
}
