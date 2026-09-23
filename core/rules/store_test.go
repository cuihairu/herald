package rules

import (
	"context"
	"errors"
	"testing"
)

func TestMemoryStore(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()

	if _, err := s.Get(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound for missing rule, got %v", err)
	}
	if err := s.Delete(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound deleting missing rule, got %v", err)
	}

	r1 := Rule{ID: "r1", Match: `level == "error"`, Route: []RouteStep{{Channels: []string{"a"}}}}
	r2 := Rule{ID: "r2", Match: `level == "info"`, Route: []RouteStep{{Channels: []string{"b"}}}}

	if err := s.Put(ctx, r1); err != nil {
		t.Fatalf("Put(r1): %v", err)
	}
	if err := s.Put(ctx, r2); err != nil {
		t.Fatalf("Put(r2): %v", err)
	}

	// List preserves insertion order (priority order).
	got, err := s.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 2 || got[0].ID != "r1" || got[1].ID != "r2" {
		t.Fatalf("expected [r1 r2], got %v", got)
	}

	// Put of an existing id replaces in place.
	r1v2 := r1
	r1v2.Match = `level == "warning"`
	if err := s.Put(ctx, r1v2); err != nil {
		t.Fatalf("Put(r1v2): %v", err)
	}
	got, _ = s.List(ctx)
	if len(got) != 2 || got[0].Match != r1v2.Match || got[1].ID != "r2" {
		t.Fatalf("expected in-place replace of r1, got %v", got)
	}

	one, err := s.Get(ctx, "r1")
	if err != nil {
		t.Fatalf("Get(r1): %v", err)
	}
	if one.Match != r1v2.Match {
		t.Errorf("expected updated match, got %q", one.Match)
	}

	// Delete keeps the remaining order dense.
	if err := s.Delete(ctx, "r1"); err != nil {
		t.Fatalf("Delete(r1): %v", err)
	}
	got, _ = s.List(ctx)
	if len(got) != 1 || got[0].ID != "r2" {
		t.Fatalf("expected [r2] after delete, got %v", got)
	}
	if _, err := s.Get(ctx, "r1"); !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound after delete, got %v", err)
	}

	// Re-adding after delete appends.
	r3 := Rule{ID: "r3", Match: `type == "deploy"`, Route: []RouteStep{{Channels: []string{"c"}}}}
	if err := s.Put(ctx, r3); err != nil {
		t.Fatalf("Put(r3): %v", err)
	}
	got, _ = s.List(ctx)
	if len(got) != 2 || got[0].ID != "r2" || got[1].ID != "r3" {
		t.Fatalf("expected [r2 r3], got %v", got)
	}
}

func TestNewMemoryStoreWith(t *testing.T) {
	a := Rule{ID: "a", Match: `level == "error"`, Route: []RouteStep{{Channels: []string{"x"}}}}
	b := Rule{ID: "b", Match: `level == "info"`, Route: []RouteStep{{Channels: []string{"y"}}}}
	s := NewMemoryStoreWith(a, b)

	got, err := s.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 2 || got[0].ID != "a" || got[1].ID != "b" {
		t.Fatalf("expected [a b], got %v", got)
	}
}
