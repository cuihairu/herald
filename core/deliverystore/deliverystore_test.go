package deliverystore

import (
	"testing"
	"time"

	"github.com/cuihairu/herald/core"
)

func TestNew(t *testing.T) {
	t.Run("with valid limit", func(t *testing.T) {
		store := New(100)
		if store == nil {
			t.Fatal("expected non-nil store")
		}
		if store.limit != 100 {
			t.Errorf("expected limit 100, got %d", store.limit)
		}
	})

	t.Run("with zero limit", func(t *testing.T) {
		store := New(0)
		if store == nil {
			t.Fatal("expected non-nil store")
		}
		if store.limit != 10000 {
			t.Errorf("expected default limit 10000, got %d", store.limit)
		}
	})
}

func TestStore_PutGet(t *testing.T) {
	t.Run("put and get", func(t *testing.T) {
		store := New(10)
		task := &core.DeliveryTask{
			ID:        "d-1",
			Provider:  "email",
			Status:    core.StatusDelivering,
			CreatedAt: time.Now(),
		}
		store.Put(NewDelivery(task))

		d, ok := store.Get("d-1")
		if !ok {
			t.Fatal("expected to find delivery by ID")
		}
		if d.Status != string(core.StatusDelivering) {
			t.Errorf("expected status delivering, got %q", d.Status)
		}
		if d.Provider != "email" {
			t.Errorf("expected provider email, got %q", d.Provider)
		}
	})

	t.Run("get missing", func(t *testing.T) {
		store := New(10)
		if _, ok := store.Get("nope"); ok {
			t.Error("expected missing delivery")
		}
	})

	t.Run("put updates in place", func(t *testing.T) {
		store := New(10)
		task := &core.DeliveryTask{ID: "d-1", Provider: "email", Status: core.StatusQueued, CreatedAt: time.Now()}
		store.Put(NewDelivery(task))
		task.Status = core.StatusDelivered
		store.Put(NewDelivery(task))

		d, _ := store.Get("d-1")
		if d.Status != string(core.StatusDelivered) {
			t.Errorf("expected status delivered, got %q", d.Status)
		}
		if got := store.Count(nil); got != 1 {
			t.Errorf("expected 1 delivery after update, got %d", got)
		}
	})
}

func TestStore_Retention(t *testing.T) {
	t.Run("oldest drops first", func(t *testing.T) {
		store := New(2)
		for _, id := range []string{"a", "b", "c"} {
			store.Put(NewDelivery(&core.DeliveryTask{ID: id, Provider: "email", CreatedAt: time.Now()}))
		}
		if got := store.Count(nil); got != 2 {
			t.Errorf("expected 2 deliveries, got %d", got)
		}
		if _, ok := store.Get("a"); ok {
			t.Error("expected oldest delivery a to be dropped")
		}
		if _, ok := store.Get("c"); !ok {
			t.Error("expected newest delivery c to be kept")
		}
	})
}

func TestStore_List(t *testing.T) {
	t.Run("list newest first", func(t *testing.T) {
		store := New(10)
		base := time.Now()
		for i, id := range []string{"a", "b", "c"} {
			store.Put(NewDelivery(&core.DeliveryTask{
				ID: id, Provider: "email", CreatedAt: base,
			}))
			// UpdatedAt drives ordering; stagger it so the order is
			// deterministic regardless of map iteration.
			d, _ := store.Get(id)
			d.UpdatedAt = base.Add(time.Duration(i) * time.Minute)
		}

		got := store.List(0, 10, nil)
		if len(got) != 3 {
			t.Fatalf("expected 3 deliveries, got %d", len(got))
		}
		if got[0].ID != "c" {
			t.Errorf("expected newest first (c), got %q", got[0].ID)
		}
	})

	t.Run("list pagination", func(t *testing.T) {
		store := New(10)
		base := time.Now()
		for i, id := range []string{"a", "b", "c"} {
			store.Put(NewDelivery(&core.DeliveryTask{ID: id, Provider: "email", CreatedAt: base}))
			d, _ := store.Get(id)
			d.UpdatedAt = base.Add(time.Duration(i) * time.Minute)
		}

		got := store.List(1, 1, nil)
		if len(got) != 1 {
			t.Fatalf("expected 1 delivery, got %d", len(got))
		}
		if got[0].ID != "b" {
			t.Errorf("expected b at offset 1, got %q", got[0].ID)
		}
	})

	t.Run("list filter by status", func(t *testing.T) {
		store := New(10)
		store.Put(NewDelivery(&core.DeliveryTask{ID: "a", Provider: "email", Status: core.StatusDelivered, CreatedAt: time.Now()}))
		store.Put(NewDelivery(&core.DeliveryTask{ID: "b", Provider: "email", Status: core.StatusFailed, CreatedAt: time.Now()}))

		got := store.List(0, 10, &Filter{Status: string(core.StatusDelivered)})
		if len(got) != 1 || got[0].ID != "a" {
			t.Errorf("expected only delivered a, got %v", got)
		}
	})
}

func TestStore_Stats(t *testing.T) {
	t.Run("stats by status and provider", func(t *testing.T) {
		store := New(10)
		store.Put(NewDelivery(&core.DeliveryTask{ID: "a", Provider: "email", Status: core.StatusDelivered, Level: "info", CreatedAt: time.Now()}))
		store.Put(NewDelivery(&core.DeliveryTask{ID: "b", Provider: "feishu", Status: core.StatusFailed, Level: "error", CreatedAt: time.Now()}))

		stats := store.Stats()
		if stats.Total != 2 {
			t.Errorf("expected total 2, got %d", stats.Total)
		}
		if stats.ByStatus[string(core.StatusDelivered)] != 1 {
			t.Errorf("expected 1 delivered, got %d", stats.ByStatus[string(core.StatusDelivered)])
		}
		if stats.ByProvider["email"] != 1 {
			t.Errorf("expected 1 email, got %d", stats.ByProvider["email"])
		}
	})
}

func TestNewDelivery(t *testing.T) {
	t.Run("empty status decodes as queued", func(t *testing.T) {
		d := NewDelivery(&core.DeliveryTask{ID: "x", Provider: "email"})
		if d.Status != string(core.StatusQueued) {
			t.Errorf("expected queued, got %q", d.Status)
		}
	})

	t.Run("snapshots retry progression", func(t *testing.T) {
		stamp := time.Now().Add(time.Minute)
		d := NewDelivery(&core.DeliveryTask{
			ID: "x", Provider: "email", Status: core.StatusRetrying,
			RetryCount: 2, MaxAttempts: 4, LastError: "boom", NextRetryAt: &stamp,
		})
		if d.RetryCount != 2 || d.MaxAttempts != 4 {
			t.Errorf("expected retry 2/4, got %d/%d", d.RetryCount, d.MaxAttempts)
		}
		if d.LastError != "boom" {
			t.Errorf("expected last error boom, got %q", d.LastError)
		}
		if d.NextRetryAt == nil || !d.NextRetryAt.Equal(stamp) {
			t.Error("expected next retry stamp preserved")
		}
	})
}
