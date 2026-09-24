package ack

import (
	"context"
	"errors"
	"testing"
)

func TestMemoryAckFirstWins(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()

	rec, err := s.Ack(ctx, "alert-1", "alice", "api")
	if err != nil {
		t.Fatalf("Ack: %v", err)
	}
	if rec.AlertID != "alert-1" || rec.AckedBy != "alice" || rec.Source != "api" {
		t.Fatalf("unexpected record: %+v", rec)
	}
	if rec.AckedAt.IsZero() {
		t.Error("AckedAt must be set by the store")
	}

	// A second ack keeps the first record: the acknowledgement time is a
	// fact, not a counter.
	rec2, err := s.Ack(ctx, "alert-1", "bob", "api")
	if err != nil {
		t.Fatalf("second Ack: %v", err)
	}
	if rec2.AckedBy != "alice" || !rec2.AckedAt.Equal(rec.AckedAt) {
		t.Fatalf("first ack must win, got %+v (first was %+v)", rec2, rec)
	}

	// Get reflects the record; absent ids read as (nil, nil).
	got, err := s.Get(ctx, "alert-1")
	if err != nil || got == nil || got.AckedBy != "alice" {
		t.Fatalf("Get: %+v, %v", got, err)
	}
	if got, err := s.Get(ctx, "missing"); err != nil || got != nil {
		t.Fatalf("Get absent: %+v, %v (want nil, nil)", got, err)
	}
}

func TestMemoryAckDeleteAndClose(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()

	if _, err := s.Ack(ctx, "alert-1", "alice", "api"); err != nil {
		t.Fatalf("Ack: %v", err)
	}
	// Deleting an absent id is a no-op.
	if err := s.Delete(ctx, "missing"); err != nil {
		t.Fatalf("Delete absent: %v", err)
	}
	if err := s.Delete(ctx, "alert-1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if got, err := s.Get(ctx, "alert-1"); err != nil || got != nil {
		t.Fatalf("Get after delete: %+v, %v", got, err)
	}

	if _, err := s.Ack(ctx, "alert-2", "alice", "api"); err != nil {
		t.Fatalf("Ack: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if got, err := s.Get(ctx, "alert-2"); err != nil || got != nil {
		t.Fatalf("Get after close: %+v, %v", got, err)
	}
	// Close is safe to call more than once.
	if err := s.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

func TestMemoryAckEmptyIDRejected(t *testing.T) {
	_, err := NewMemoryStore().Ack(context.Background(), "", "alice", "api")
	if !errors.Is(err, ErrEmptyID) {
		t.Fatalf("expected ErrEmptyID, got %v", err)
	}
}
