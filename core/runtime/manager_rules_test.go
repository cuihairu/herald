package runtime

import (
	"errors"
	"testing"

	"github.com/cuihairu/herald/core"
	"github.com/cuihairu/herald/core/logstore"
)

func TestManagerRecordShadow(t *testing.T) {
	m := NewManager(100)

	n := &core.Notification{ID: "notif-1", Type: "deploy", Level: "error"}

	// First hit is always recorded (sample policy: first + every Nth).
	m.RecordShadow("r1", []string{"oncall"}, n)

	logs := m.GetLogs(0, 10, &logstore.Filter{Status: "shadow"})
	if len(logs) != 1 {
		t.Fatalf("expected 1 shadow log, got %d", len(logs))
	}
	entry := logs[0]
	if entry.RuleID != "r1" || !entry.WouldFire || entry.Status != "shadow" {
		t.Fatalf("unexpected shadow entry: %+v", entry)
	}
	if entry.MatchedAt.IsZero() {
		t.Error("expected matched_at to be set")
	}
	if len(entry.Channels) != 1 || entry.Channels[0] != "oncall" {
		t.Errorf("expected would-be channels [oncall], got %v", entry.Channels)
	}
	if m.ShadowRuleCount("r1") != 1 {
		t.Errorf("expected hit count 1, got %d", m.ShadowRuleCount("r1"))
	}
}

func TestManagerRecordShadowSampling(t *testing.T) {
	m := NewManager(1000)
	n := &core.Notification{ID: "notif", Level: "error"}

	// Default interval is 100: hits 1 and 100 are recorded, 2..99 are not.
	recorded := 0
	for i := 1; i <= 100; i++ {
		m.RecordShadow("r1", nil, n)
		if len(m.GetLogs(0, 10, &logstore.Filter{Status: "shadow"})) > recorded {
			recorded++
		}
	}
	if recorded != 2 {
		t.Fatalf("expected hits 1 and 100 recorded (2 entries), got %d", recorded)
	}
	if m.ShadowRuleCount("r1") != 100 {
		t.Errorf("counter must track every hit, got %d", m.ShadowRuleCount("r1"))
	}
}

func TestManagerRecordEvalError(t *testing.T) {
	m := NewManager(100)
	n := &core.Notification{ID: "notif-1", Level: "error"}

	m.RecordEvalError("broken", errors.New("missing key"), n)

	logs := m.GetLogs(0, 10, &logstore.Filter{Status: "shadow"})
	if len(logs) != 1 {
		t.Fatalf("expected 1 log, got %d", len(logs))
	}
	if logs[0].RuleID != "broken" || logs[0].Error != "missing key" {
		t.Fatalf("unexpected entry: %+v", logs[0])
	}
	if logs[0].WouldFire {
		t.Error("eval errors must not be marked would_fire")
	}

	// nil errors are ignored.
	m.RecordEvalError("broken", nil, n)
	if got := len(m.GetLogs(0, 10, &logstore.Filter{Status: "shadow"})); got != 1 {
		t.Fatalf("nil error must not be recorded, got %d entries", got)
	}
}

func TestManagerRecordForPending(t *testing.T) {
	m := NewManager(100)
	n := &core.Notification{ID: "notif-1", Level: "error"}

	m.RecordForPending("r-for", n)

	logs := m.GetLogs(0, 10, &logstore.Filter{Status: "pending"})
	if len(logs) != 1 {
		t.Fatalf("expected 1 pending log, got %d", len(logs))
	}
	entry := logs[0]
	if entry.RuleID != "r-for" || entry.Status != "pending" {
		t.Fatalf("unexpected pending entry: %+v", entry)
	}
	if entry.WouldFire {
		t.Error("a suppressed event must not be marked would_fire")
	}
	if entry.MatchedAt.IsZero() {
		t.Error("expected matched_at to be set")
	}

	// Suppressions are sampled per rule like shadow hits: the counter
	// tracks every occurrence but the second entry is not recorded.
	m.RecordForPending("r-for", &core.Notification{ID: "notif-2", Level: "error"})
	if got := len(m.GetLogs(0, 10, &logstore.Filter{Status: "pending"})); got != 1 {
		t.Errorf("expected sampling to keep 1 entry, got %d", got)
	}
	if m.ShadowRuleCount("r-for") != 2 {
		t.Errorf("counter must track every suppression, got %d", m.ShadowRuleCount("r-for"))
	}
}
