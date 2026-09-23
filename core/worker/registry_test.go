package worker

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestRegistryRegisterAndGet(t *testing.T) {
	r := NewRegistry()

	err := r.Register(&Info{ID: "w1", Mode: Local, Capabilities: []string{"webhook"}})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	got, err := r.Get("w1")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.ID != "w1" {
		t.Errorf("ID = %q, want %q", got.ID, "w1")
	}
	if got.Mode != Local {
		t.Errorf("Mode = %q, want %q", got.Mode, Local)
	}
	if len(got.Capabilities) != 1 || got.Capabilities[0] != "webhook" {
		t.Errorf("Capabilities = %v, want [webhook]", got.Capabilities)
	}
	if got.Status != "online" {
		t.Errorf("Status = %q, want %q", got.Status, "online")
	}
	if got.ConnectedAt.IsZero() {
		t.Error("ConnectedAt not set")
	}
	if got.LastHeartbeat.IsZero() {
		t.Error("LastHeartbeat not set")
	}
}

func TestRegistryRegisterEmptyID(t *testing.T) {
	r := NewRegistry()
	if err := r.Register(&Info{}); err == nil {
		t.Error("Register() with empty ID should return error")
	}
	if err := r.Register(&Info{ID: "x"}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if r.Count() != 1 {
		t.Errorf("Count() = %d, want 1", r.Count())
	}
}

func TestRegistryRegisterKeepsConnectedAt(t *testing.T) {
	r := NewRegistry()
	ts := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	if err := r.Register(&Info{ID: "w1", ConnectedAt: ts}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	got, err := r.Get("w1")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if !got.ConnectedAt.Equal(ts) {
		t.Errorf("ConnectedAt = %v, want %v", got.ConnectedAt, ts)
	}
}

func TestRegistryRegisterOverwrite(t *testing.T) {
	r := NewRegistry()

	if err := r.Register(&Info{ID: "w1", Mode: Local}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if err := r.Register(&Info{ID: "w1", Mode: Remote}); err != nil {
		t.Fatalf("Register() overwrite error = %v", err)
	}
	if r.Count() != 1 {
		t.Errorf("Count() = %d, want 1 after overwrite", r.Count())
	}
	got, err := r.Get("w1")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.Mode != Remote {
		t.Errorf("Mode = %q, want %q after overwrite", got.Mode, Remote)
	}
}

func TestRegistryGetNotFound(t *testing.T) {
	r := NewRegistry()

	if _, err := r.Get("missing"); err == nil {
		t.Error("Get() on missing worker should return error")
	}
}

func TestRegistryDeregister(t *testing.T) {
	r := NewRegistry()

	if err := r.Register(&Info{ID: "w1"}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	r.Deregister("w1")
	if r.Count() != 0 {
		t.Errorf("Count() = %d, want 0", r.Count())
	}
	if _, err := r.Get("w1"); err == nil {
		t.Error("Get() after Deregister should return error")
	}

	r.Deregister("missing")
	if r.Count() != 0 {
		t.Errorf("Count() = %d, want 0 after deregistering missing worker", r.Count())
	}
}

func TestRegistryHeartbeat(t *testing.T) {
	r := NewRegistry()

	if err := r.Heartbeat("missing"); err == nil {
		t.Error("Heartbeat() on missing worker should return error")
	}

	if err := r.Register(&Info{ID: "w1"}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	got, err := r.Get("w1")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	before := got.LastHeartbeat

	time.Sleep(2 * time.Millisecond)
	if err := r.Heartbeat("w1"); err != nil {
		t.Fatalf("Heartbeat() error = %v", err)
	}

	got, err = r.Get("w1")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if !got.LastHeartbeat.After(before) {
		t.Errorf("LastHeartbeat = %v, want after %v", got.LastHeartbeat, before)
	}
}

func TestRegistryList(t *testing.T) {
	r := NewRegistry()

	if got := r.List(); len(got) != 0 {
		t.Errorf("List() on empty registry = %v, want empty", got)
	}

	if err := r.Register(&Info{ID: "w1", Mode: Local, Capabilities: []string{"a"}}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if err := r.Register(&Info{ID: "w2", Mode: Remote}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	list := r.List()
	if len(list) != 2 {
		t.Fatalf("List() length = %d, want 2", len(list))
	}
	ids := map[string]bool{}
	for _, w := range list {
		ids[w.ID] = true
	}
	if !ids["w1"] || !ids["w2"] {
		t.Errorf("List() IDs = %v, want [w1 w2]", list)
	}

	var w1 Info
	for _, w := range list {
		if w.ID == "w1" {
			w1 = w
			break
		}
	}
	if w1.ID != "w1" {
		t.Fatalf("List() missing w1: %v", list)
	}
	w1.Capabilities[0] = "mutated"
	got, err := r.Get("w1")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.Capabilities[0] == "mutated" {
		t.Error("List() should return deep copies")
	}
}

func TestRegistryListByMode(t *testing.T) {
	r := NewRegistry()

	if err := r.Register(&Info{ID: "w1", Mode: Local}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if err := r.Register(&Info{ID: "w2", Mode: Remote}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	locals := r.ListByMode(Local)
	if len(locals) != 1 || locals[0].ID != "w1" {
		t.Errorf("ListByMode(Local) = %v, want [w1]", locals)
	}
	remotes := r.ListByMode(Remote)
	if len(remotes) != 1 || remotes[0].ID != "w2" {
		t.Errorf("ListByMode(Remote) = %v, want [w2]", remotes)
	}
}

func TestRegistryCount(t *testing.T) {
	r := NewRegistry()
	if r.Count() != 0 {
		t.Errorf("Count() = %d, want 0", r.Count())
	}
	for i := 0; i < 3; i++ {
		if err := r.Register(&Info{ID: fmt.Sprintf("w%d", i)}); err != nil {
			t.Fatalf("Register() error = %v", err)
		}
	}
	if r.Count() != 3 {
		t.Errorf("Count() = %d, want 3", r.Count())
	}
}

func TestRegistryRemoveStale(t *testing.T) {
	r := NewRegistry()

	if err := r.Register(&Info{ID: "stale-remote", Mode: Remote}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if err := r.Register(&Info{ID: "fresh-remote", Mode: Remote}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if err := r.Register(&Info{ID: "stale-local", Mode: Local}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	old := time.Now().Add(-5 * time.Minute)
	r.mu.Lock()
	r.workers["stale-remote"].LastHeartbeat = old
	r.workers["stale-local"].LastHeartbeat = old
	r.mu.Unlock()

	r.RemoveStale(time.Minute)

	if r.Count() != 2 {
		t.Errorf("Count() = %d, want 2", r.Count())
	}
	if _, err := r.Get("stale-remote"); err == nil {
		t.Error("stale remote worker should be removed")
	}
	if _, err := r.Get("fresh-remote"); err != nil {
		t.Errorf("fresh remote worker should remain: %v", err)
	}
	if _, err := r.Get("stale-local"); err != nil {
		t.Errorf("local worker should never be removed: %v", err)
	}
}

func TestRegistryConcurrentRegister(t *testing.T) {
	r := NewRegistry()
	const n = 50

	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if err := r.Register(&Info{ID: fmt.Sprintf("w-%d", i), Mode: Remote}); err != nil {
				t.Errorf("Register() error = %v", err)
			}
			_ = r.Heartbeat(fmt.Sprintf("w-%d", i))
		}(i)
	}
	wg.Wait()

	if r.Count() != n {
		t.Errorf("Count() = %d, want %d", r.Count(), n)
	}
}
