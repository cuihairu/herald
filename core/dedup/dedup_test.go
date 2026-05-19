package dedup

import (
	"testing"
	"time"

	"github.com/cuihairu/herald/core"
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
	config := &Config{
		Window: 10 * time.Minute,
	}
	d := NewDedup(config)
	if d.window != 10*time.Minute {
		t.Errorf("expected window 10m, got %v", d.window)
	}
}

func TestDedupCheck(t *testing.T) {
	d := NewDedup(&Config{Window: 100 * time.Millisecond})

	event := &core.Event{
		Type: "test.event",
		Labels: map[string]string{
			"host": "server1",
		},
	}

	// First check should not dedup
	if d.Check(event) {
		t.Error("expected first check to not dedup")
	}

	// Immediate second check should dedup
	if !d.Check(event) {
		t.Error("expected second check to dedup")
	}
}

func TestDedupCheckExpired(t *testing.T) {
	d := NewDedup(&Config{Window: 50 * time.Millisecond})

	event := &core.Event{
		Type:   "test.event",
		Labels: map[string]string{"env": "prod"},
	}

	// First check
	if d.Check(event) {
		t.Error("expected first check to not dedup")
	}

	// Wait for window to expire
	time.Sleep(60 * time.Millisecond)

	// Should not dedup after window expires
	if d.Check(event) {
		t.Error("expected check to not dedup after window expires")
	}
}

func TestDedupCheckDifferentEvents(t *testing.T) {
	d := NewDedup(nil)

	event1 := &core.Event{
		Type: "error.event",
		Labels: map[string]string{
			"level": "error",
		},
	}

	event2 := &core.Event{
		Type: "warning.event",
		Labels: map[string]string{
			"level": "warning",
		},
	}

	// Different events should not dedup each other
	if d.Check(event1) {
		t.Error("expected first event to not dedup")
	}
	if d.Check(event2) {
		t.Error("expected second event to not dedup")
	}
}

func TestDedupCheckTask(t *testing.T) {
	d := NewDedup(&Config{Window: 100 * time.Millisecond})

	task := &core.Task{
		Provider: "telegram",
		Title:    "Test Alert",
		Body:     "Something happened",
		Target:   "chat123",
	}

	// First check should not dedup
	if d.CheckTask(task) {
		t.Error("expected first check to not dedup")
	}

	// Second check should dedup
	if !d.CheckTask(task) {
		t.Error("expected second check to dedup")
	}
}

func TestDedupCheckTaskExpired(t *testing.T) {
	d := NewDedup(&Config{Window: 50 * time.Millisecond})

	task := &core.Task{
		Provider: "slack",
		Title:    "Alert",
		Body:     "Test",
		Target:   "channel",
	}

	// First check
	if d.CheckTask(task) {
		t.Error("expected first check to not dedup")
	}

	// Wait for expiry
	time.Sleep(60 * time.Millisecond)

	// Should not dedup
	if d.CheckTask(task) {
		t.Error("expected check to not dedup after window expires")
	}
}

func TestDedupDifferentTasks(t *testing.T) {
	d := NewDedup(nil)

	task1 := &core.Task{
		Provider: "telegram",
		Title:    "Alert 1",
		Body:     "Body 1",
		Target:   "target1",
	}

	task2 := &core.Task{
		Provider: "telegram",
		Title:    "Alert 2",
		Body:     "Body 2",
		Target:   "target2",
	}

	// Different tasks should not dedup
	if d.CheckTask(task1) {
		t.Error("expected first task to not dedup")
	}
	if d.CheckTask(task2) {
		t.Error("expected second task to not dedup")
	}
}

func TestDedupCleanOldEntries(t *testing.T) {
	d := NewDedup(&Config{Window: 50 * time.Millisecond})

	event := &core.Event{Type: "test"}

	// Add entry
	d.Check(event)

	// Wait for expiry
	time.Sleep(60 * time.Millisecond)

	// Check should clean old entries and not dedup
	if d.Check(event) {
		t.Error("expected check to not dedup after cleanup")
	}
}

func TestDedupKeyGeneration(t *testing.T) {
	d := NewDedup(nil)

	event1 := &core.Event{
		Type: "test",
		Labels: map[string]string{
			"a": "1",
			"b": "2",
		},
	}

	event2 := &core.Event{
		Type: "test",
		Labels: map[string]string{
			"a": "1",
			"b": "2",
		},
	}

	event3 := &core.Event{
		Type: "test",
		Labels: map[string]string{
			"a": "1",
			"b": "3",
		},
	}

	// Same content should generate same key
	d.Check(event1)
	if !d.Check(event2) {
		t.Error("expected event2 to be dedup (same as event1)")
	}

	// Different content should not dedup
	if d.Check(event3) {
		t.Error("expected event3 to not dedup (different from event1)")
	}
}
