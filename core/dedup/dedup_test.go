package dedup

import (
	"testing"
	"time"
)

func TestNewDedup(t *testing.T) {
	d := NewDedup(nil)
	if d == nil {
		t.Fatal("expected non-nil dedup")
	}
	if d.window != 5*time.Minute {
		t.Errorf("expected default window 5m, got %v", d.window)
	}
}

func TestNewDedupWithConfig(t *testing.T) {
	d := NewDedup(&Config{Window: 10 * time.Minute})
	if d.window != 10*time.Minute {
		t.Errorf("expected window 10m, got %v", d.window)
	}
}

func TestDedupCheck(t *testing.T) {
	d := NewDedup(&Config{Window: 100 * time.Millisecond})

	if d.Check("id1", []string{"email"}, nil) {
		t.Error("expected first check to not dedup")
	}
	if !d.Check("id1", []string{"email"}, nil) {
		t.Error("expected second check to dedup")
	}
}

func TestDedupCheckExpired(t *testing.T) {
	d := NewDedup(&Config{Window: 50 * time.Millisecond})

	d.Check("id1", []string{"email"}, nil)
	time.Sleep(60 * time.Millisecond)

	if d.Check("id1", []string{"email"}, nil) {
		t.Error("expected check to not dedup after window expires")
	}
}

func TestDedupCheckDifferentChannels(t *testing.T) {
	d := NewDedup(nil)

	if d.Check("id1", []string{"email"}, nil) {
		t.Error("expected first check to not dedup")
	}
	if d.Check("id1", []string{"slack"}, nil) {
		t.Error("expected different channels to not dedup")
	}
}

func TestDedupCleanOldEntries(t *testing.T) {
	d := NewDedup(&Config{Window: 50 * time.Millisecond})
	d.Check("id1", []string{"email"}, nil)
	time.Sleep(60 * time.Millisecond)

	if d.Check("id1", []string{"email"}, nil) {
		t.Error("expected check to not dedup after cleanup")
	}
}
