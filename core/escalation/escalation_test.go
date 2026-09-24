package escalation

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/cuihairu/herald/core/ack"
)

// recordingNotifier captures fired upgrades.
type recordingNotifier struct {
	mu    sync.Mutex
	fired []Pending
}

func (r *recordingNotifier) Escalate(p Pending) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.fired = append(r.fired, p)
}

func (r *recordingNotifier) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.fired)
}

func pending(alertID string, timeout time.Duration) Pending {
	return Pending{
		RuleID:    "r-up",
		AlertID:   alertID,
		To:        []string{"phone-bridge"},
		Timeout:   timeout,
		CreatedAt: time.Now(),
		Type:      "alert",
		Level:     "critical",
		Title:     "disk full",
	}
}

func TestManagerFiresWhenNotAcknowledged(t *testing.T) {
	notify := &recordingNotifier{}
	m := NewManager(ack.NewMemoryStore(), notify, "")
	if err := m.Schedule(context.Background(), pending("a1", 10*time.Millisecond)); err != nil {
		t.Fatalf("Schedule: %v", err)
	}
	if notify.count() != 0 {
		t.Fatalf("upgrade fired before the timeout")
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if notify.count() == 1 {
			got := notify.fired[0]
			if got.AlertID != "a1" || got.RuleID != "r-up" || got.Title != "disk full" {
				t.Fatalf("unexpected upgrade: %+v", got)
			}
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("upgrade never fired")
}

func TestManagerAckBeforeFireStandsDown(t *testing.T) {
	notify := &recordingNotifier{}
	acks := ack.NewMemoryStore()
	m := NewManager(acks, notify, "")
	ctx := context.Background()
	if err := m.Schedule(ctx, pending("a1", 10*time.Millisecond)); err != nil {
		t.Fatalf("Schedule: %v", err)
	}
	// The ack lands before the deadline.
	if _, err := acks.Ack(ctx, "a1", "alice", "api"); err != nil {
		t.Fatalf("Ack: %v", err)
	}
	time.Sleep(40 * time.Millisecond)
	if notify.count() != 0 {
		t.Fatalf("acknowledged alert must not escalate")
	}
}

func TestManagerCancelDropsPending(t *testing.T) {
	notify := &recordingNotifier{}
	m := NewManager(ack.NewMemoryStore(), notify, "")
	ctx := context.Background()
	if err := m.Schedule(ctx, pending("a1", 10*time.Millisecond)); err != nil {
		t.Fatalf("Schedule: %v", err)
	}
	if err := m.Cancel(ctx, "a1"); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	time.Sleep(40 * time.Millisecond)
	if notify.count() != 0 {
		t.Fatalf("cancelled upgrade must not fire")
	}
}

func TestManagerScheduleReArmsWindow(t *testing.T) {
	notify := &recordingNotifier{}
	m := NewManager(ack.NewMemoryStore(), notify, "")
	ctx := context.Background()
	// First delivery arms a 30ms window; a second delivery 20ms later
	// resets it, so at 35ms nothing has fired yet.
	if err := m.Schedule(ctx, pending("a1", 30*time.Millisecond)); err != nil {
		t.Fatalf("Schedule: %v", err)
	}
	time.Sleep(20 * time.Millisecond)
	if err := m.Schedule(ctx, pending("a1", 30*time.Millisecond)); err != nil {
		t.Fatalf("re-Schedule: %v", err)
	}
	time.Sleep(15 * time.Millisecond)
	if notify.count() != 0 {
		t.Fatalf("re-armed window fired too early")
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if notify.count() == 1 {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("re-armed upgrade never fired")
}

func TestManagerValidation(t *testing.T) {
	m := NewManager(ack.NewMemoryStore(), &recordingNotifier{}, "")
	ctx := context.Background()
	cases := map[string]Pending{
		"empty alert id":   {RuleID: "r", Timeout: time.Minute},
		"empty rule id":    {AlertID: "a", Timeout: time.Minute},
		"zero timeout":     {RuleID: "r", AlertID: "a"},
		"negative timeout": {RuleID: "r", AlertID: "a", Timeout: -time.Minute},
	}
	for name, p := range cases {
		if err := m.Schedule(ctx, p); err == nil {
			t.Errorf("%s: expected rejection", name)
		}
	}
}

func TestManagerRestoreJudgesExpiredAndReArmsRest(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/pendings.json"
	notify := &recordingNotifier{}
	acks := ack.NewMemoryStore()
	ctx := context.Background()

	// Seed the persistence file directly (bypassing Schedule, whose timers
	// would immediately fire under the fixed test clock): one expired
	// unacked, one expired acked (down period), one still running.
	now := time.Now()
	expired := pending("a-expired", time.Minute)
	expired.CreatedAt = now.Add(-2 * time.Minute)
	acked := pending("a-acked", time.Minute)
	acked.CreatedAt = now.Add(-2 * time.Minute)
	running := pending("a-running", 5*time.Second)
	running.CreatedAt = now.Add(-4980 * time.Millisecond) // 20ms remaining
	data, err := json.Marshal(fileFormat{Version: fileFormatVersion, Pendings: []Pending{expired, acked, running}})
	if err != nil {
		t.Fatalf("marshal seed: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write seed: %v", err)
	}
	if _, err := acks.Ack(ctx, "a-acked", "alice", "api"); err != nil {
		t.Fatalf("Ack: %v", err)
	}

	m := NewManager(acks, notify, path)
	m.SetNow(func() time.Time { return now })
	// Restore re-judges the expired one (upgraded), stands down for the
	// acked one, and re-arms the running one for its remaining 20ms.
	if err := m.Restore(ctx); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if notify.count() == 2 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if notify.count() != 2 {
		t.Fatalf("expected expired + running upgrades to fire, got %d", notify.count())
	}
	if notify.fired[0].AlertID != "a-expired" || notify.fired[1].AlertID != "a-running" {
		t.Fatalf("unexpected fire order: %+v", notify.fired)
	}
}

func TestManagerRestoreWithoutPathIsNoop(t *testing.T) {
	m := NewManager(ack.NewMemoryStore(), &recordingNotifier{}, "")
	if err := m.Restore(context.Background()); err != nil {
		t.Fatalf("Restore without path: %v", err)
	}
}

func TestManagerCloseStopsTimers(t *testing.T) {
	notify := &recordingNotifier{}
	m := NewManager(ack.NewMemoryStore(), notify, "")
	ctx := context.Background()
	if err := m.Schedule(ctx, pending("a1", 10*time.Millisecond)); err != nil {
		t.Fatalf("Schedule: %v", err)
	}
	if err := m.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// Close is safe to call again.
	if err := m.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	time.Sleep(40 * time.Millisecond)
	if notify.count() != 0 {
		t.Fatalf("closed manager must not fire upgrades")
	}
}

func TestManagerSetNotifierSwapsAfterConstruction(t *testing.T) {
	// A manager constructed without a notifier (herald wires the service in
	// after NewServer) still fires once one is set.
	m := NewManager(ack.NewMemoryStore(), nil, "")
	notify := &recordingNotifier{}
	m.SetNotifier(notify)
	if err := m.Schedule(context.Background(), pending("a1", 10*time.Millisecond)); err != nil {
		t.Fatalf("Schedule: %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if notify.count() == 1 {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("upgrade never fired after SetNotifier")
}

func TestManagerCancelUnknownAlertIsNoop(t *testing.T) {
	m := NewManager(ack.NewMemoryStore(), &recordingNotifier{}, "")
	if err := m.Cancel(context.Background(), "never-scheduled"); err != nil {
		t.Fatalf("Cancel of an unknown alert must be a no-op, got %v", err)
	}
}

func TestManagerRestoreMissingFileIsEmpty(t *testing.T) {
	m := NewManager(ack.NewMemoryStore(), &recordingNotifier{}, t.TempDir()+"/absent.json")
	if err := m.Restore(context.Background()); err != nil {
		t.Fatalf("Restore with a missing file must succeed empty, got %v", err)
	}
}

func TestManagerRestoreRejectsCorruptFile(t *testing.T) {
	t.Run("broken json", func(t *testing.T) {
		path := t.TempDir() + "/pendings.json"
		if err := os.WriteFile(path, []byte("{broken"), 0o644); err != nil {
			t.Fatalf("seed: %v", err)
		}
		m := NewManager(ack.NewMemoryStore(), &recordingNotifier{}, path)
		if err := m.Restore(context.Background()); err == nil {
			t.Fatal("expected Restore to reject a corrupt file")
		}
	})
	t.Run("unknown version", func(t *testing.T) {
		path := t.TempDir() + "/pendings.json"
		data, err := json.Marshal(fileFormat{Version: fileFormatVersion + 1})
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatalf("seed: %v", err)
		}
		m := NewManager(ack.NewMemoryStore(), &recordingNotifier{}, path)
		if err := m.Restore(context.Background()); err == nil {
			t.Fatal("expected Restore to reject an unknown format version")
		}
	})
}

func TestManagerPersistenceFailureDoesNotBlock(t *testing.T) {
	// An unwritable store must not disable the in-memory timers: missing
	// persistence degrades to losing pendings across restarts (Schedule
	// reports the write error, the caller may ignore it), not to silently
	// dropping upgrades in the running process.
	notify := &recordingNotifier{}
	m := NewManager(ack.NewMemoryStore(), notify, t.TempDir()+"/no/such/dir/pendings.json")
	err := m.Schedule(context.Background(), pending("a1", 10*time.Millisecond))
	if err == nil {
		t.Fatal("expected Schedule to report the persistence failure")
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if notify.count() == 1 {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("upgrade never fired despite the persistence failure")
}

func TestArmClampsElapsedDeadline(t *testing.T) {
	notify := &recordingNotifier{}
	m := NewManager(ack.NewMemoryStore(), notify, "")
	p := pending("a-past", time.Minute)
	p.CreatedAt = time.Now().Add(-2 * time.Hour) // deadline already passed
	if err := m.Schedule(context.Background(), p); err != nil {
		t.Fatalf("Schedule: %v", err)
	}
	// A negative remaining must clamp to an immediate fire, not a
	// multi-hour timer.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if notify.count() == 1 {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("an elapsed deadline must fire immediately")
}

func TestCancelReportsSaveFailure(t *testing.T) {
	m := NewManager(ack.NewMemoryStore(), &recordingNotifier{},
		filepath.Join(t.TempDir(), "pendings.json"))
	if err := m.Schedule(context.Background(), pending("a-c", time.Minute)); err != nil {
		t.Fatalf("Schedule: %v", err)
	}
	// Break the persistence line after the pending was stored.
	m.mu.Lock()
	m.path = filepath.Join(t.TempDir(), "missing-dir", "pendings.json")
	m.mu.Unlock()
	if err := m.Cancel(context.Background(), "a-c"); err == nil {
		t.Fatal("cancel must surface the save failure")
	}
}

func TestFireToleratesSaveFailure(t *testing.T) {
	notify := &recordingNotifier{}
	m := NewManager(ack.NewMemoryStore(), notify,
		filepath.Join(t.TempDir(), "pendings.json"))
	// The pending is stored first; the persistence line breaks before the
	// timer fires.
	if err := m.Schedule(context.Background(), pending("a-f", 20*time.Millisecond)); err != nil {
		t.Fatalf("Schedule: %v", err)
	}
	m.mu.Lock()
	m.path = filepath.Join(t.TempDir(), "missing-dir", "pendings.json")
	m.mu.Unlock()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if notify.count() == 1 {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("the upgrade must still fire when persistence fails")
}

func TestLoadRejectsUnreadablePath(t *testing.T) {
	// Reading a directory fails with something other than NotExist.
	m := NewManager(ack.NewMemoryStore(), nil, t.TempDir())
	if _, err := m.load(); err == nil {
		t.Fatal("reading a directory must fail")
	}
}

func TestEmptyPathSkipsPersistence(t *testing.T) {
	// An empty path means in-memory only: Schedule succeeds without
	// touching the filesystem.
	m := NewManager(ack.NewMemoryStore(), &recordingNotifier{}, "")
	if err := m.Schedule(context.Background(), pending("a-nofile", time.Minute)); err != nil {
		t.Fatalf("Schedule with no persistence path must succeed: %v", err)
	}
}

func TestCancelSkipsOtherAlerts(t *testing.T) {
	notify := &recordingNotifier{}
	m := NewManager(ack.NewMemoryStore(), notify, "")
	if err := m.Schedule(context.Background(), pending("a-keep", 20*time.Millisecond)); err != nil {
		t.Fatalf("Schedule: %v", err)
	}
	if err := m.Schedule(context.Background(), pending("a-drop", 20*time.Millisecond)); err != nil {
		t.Fatalf("Schedule: %v", err)
	}
	if err := m.Cancel(context.Background(), "a-drop"); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	// Only a-drop was removed; a-keep stays armed and still fires.
	deadline := time.Now().Add(2 * time.Second)
	kept := false
	for time.Now().Before(deadline) {
		if notify.count() > 0 {
			if got := notify.fired[0]; got.AlertID == "a-keep" {
				kept = true
			}
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	if !kept {
		t.Fatal("the untouched alert must still fire after cancelling another")
	}
}

func TestFireIgnoresUnknownKey(t *testing.T) {
	// A timer for a key that was cancelled (or never existed) must stand
	// down quietly.
	m := NewManager(ack.NewMemoryStore(), nil, "")
	m.fire("no-such-key")
}

func TestSaveLockInitializesNilTable(t *testing.T) {
	// The zero-value Manager (no NewManager) must survive a save: the nil
	// pending table gets initialized instead of panicking.
	m := &Manager{path: filepath.Join(t.TempDir(), "pendings.json")}
	if err := m.saveLocked(); err != nil {
		t.Fatalf("saving an empty table must succeed: %v", err)
	}
	if m.pendings == nil {
		t.Fatal("save must leave a usable pending table behind")
	}
}
