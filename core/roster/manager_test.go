package roster

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestManagerCRUD(t *testing.T) {
	ctx := context.Background()
	m := NewManager(nil)

	if _, err := m.Get(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}

	r := validRoster()
	if err := m.Put(ctx, &r); err != nil {
		t.Fatalf("Put: %v", err)
	}
	// Invalid roster never reaches the live table.
	bad := Roster{ID: "bad", Periods: nil}
	if err := m.Put(ctx, &bad); err == nil {
		t.Fatal("expected validation rejection")
	}
	if _, err := m.Get(ctx, "bad"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("invalid roster must not be stored, got %v", err)
	}

	got, err := m.Get(ctx, "ops-oncall")
	if err != nil || len(got.Periods) != 2 {
		t.Fatalf("Get = %+v, %v", got, err)
	}

	if err := m.Delete(ctx, "ops-oncall"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if err := m.Delete(ctx, "ops-oncall"); !errors.Is(err, ErrNotFound) {
		// Memory-only manager: no store to reject first, the live table
		// miss must surface as not-found anyway.
		t.Fatalf("double delete must report not-found, got %v", err)
	}
}

func TestManagerCovers(t *testing.T) {
	ctx := context.Background()
	m := NewManager(nil)
	r := validRoster() // first period: 2026-10-05 09:00-17:00 UTC
	if err := m.Put(ctx, &r); err != nil {
		t.Fatalf("Put: %v", err)
	}

	at := r.Periods[0].Start.Add(time.Hour)
	if !m.Covers("ops-oncall", at) {
		t.Error("expected the pushed period to cover")
	}
	if m.Covers("ops-oncall", r.Periods[0].End.Add(time.Hour)) {
		t.Error("expected no cover outside the pushed periods")
	}

	// Live updates are visible immediately — the silence gate reads the
	// same table writes refresh.
	rotated := Roster{ID: "ops-oncall", Periods: []Period{
		{Start: r.Periods[0].End, End: r.Periods[0].End.Add(time.Hour)},
	}}
	if err := m.Put(ctx, &rotated); err != nil {
		t.Fatalf("Put(rotate): %v", err)
	}
	if m.Covers("ops-oncall", at) {
		t.Error("rotation must take effect immediately")
	}
	if !m.Covers("ops-oncall", rotated.Periods[0].Start.Add(time.Minute)) {
		t.Error("the new period must cover")
	}

	// Deleting the roster fails the silence gate open: no data, no cover.
	if err := m.Delete(ctx, "ops-oncall"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if m.Covers("ops-oncall", rotated.Periods[0].Start.Add(time.Minute)) {
		t.Error("deleted roster must cover nothing")
	}

	// An unknown roster (never pushed) covers nothing — the forward
	// reference stays inert, not silenced.
	if m.Covers("never-pushed", time.Now()) {
		t.Error("unknown roster must cover nothing")
	}
}

func TestManagerPersistence(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "rosters.json")
	store, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}

	// Manager over an empty store: reload is a no-op that keeps serving.
	m := NewManager(store)
	if err := m.Reload(ctx); err != nil {
		t.Fatalf("Reload empty: %v", err)
	}

	r := validRoster()
	if err := m.Put(ctx, &r); err != nil {
		t.Fatalf("Put: %v", err)
	}

	// A second manager over the same file picks the persisted table up.
	m2 := NewManager(store)
	if err := m2.Reload(ctx); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	if _, err := m2.Get(ctx, "ops-oncall"); err != nil {
		t.Fatalf("persisted roster must reload, got %v", err)
	}

	// Deleting through one manager is visible after the other reloads.
	if err := m.Delete(ctx, "ops-oncall"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if err := m2.Reload(ctx); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	if _, err := m2.Get(ctx, "ops-oncall"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted roster must not reload, got %v", err)
	}
}

func TestManagerReloadRejectsInvalidStoredRoster(t *testing.T) {
	ctx := context.Background()
	// Sneak an invalid roster past validation straight into the store (as
	// a manual file edit would).
	store := NewMemoryStoreWith(Roster{ID: "rotten", Periods: nil})
	m := NewManager(store)
	if err := m.Reload(ctx); err == nil {
		t.Fatal("invalid stored roster must abort reload")
	}

	// The previous table keeps serving (here: it was empty).
	if got, _ := m.List(ctx); len(got) != 0 {
		t.Fatalf("reload failure must keep the old table, got %v", got)
	}
}

func TestManagerListNonEmpty(t *testing.T) {
	ctx := context.Background()
	m := NewManager(nil)
	if err := m.Put(ctx, &Roster{ID: "a", Periods: validPeriods()}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := m.Put(ctx, &Roster{ID: "b", Periods: validPeriods()}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, err := m.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 2 || got[0].ID != "a" || got[1].ID != "b" {
		t.Fatalf("expected [a b], got %v", got)
	}
}

// failingStore errors on every operation, exercising the manager's
// store-failure paths.
type failingStore struct{}

func (failingStore) List(context.Context) ([]Roster, error) { return nil, errStoreDown }
func (failingStore) Get(context.Context, string) (Roster, error) {
	return Roster{}, errStoreDown
}
func (failingStore) Put(context.Context, Roster) error     { return errStoreDown }
func (failingStore) Delete(context.Context, string) error { return errStoreDown }

func TestManagerStoreFailures(t *testing.T) {
	ctx := context.Background()

	t.Run("reload surfaces a dead store", func(t *testing.T) {
		m := NewManager(failingStore{})
		if err := m.Reload(ctx); err == nil || !errors.Is(err, errStoreDown) {
			t.Fatalf("expected store list error, got %v", err)
		}
	})

	t.Run("put surfaces a dead store", func(t *testing.T) {
		m := NewManager(failingStore{})
		r := validRoster()
		if err := m.Put(ctx, &r); err == nil {
			t.Fatal("expected store put error")
		}
	})

	t.Run("delete surfaces a dead store", func(t *testing.T) {
		m := NewManager(failingStore{})
		if err := m.Delete(ctx, "ops-oncall"); err == nil {
			t.Fatal("expected store delete error")
		}
	})

	t.Run("reload without a store is a no-op", func(t *testing.T) {
		m := NewManager(nil)
		if err := m.Reload(ctx); err != nil {
			t.Fatalf("Reload(nil store): %v", err)
		}
	})
}

// errStoreDown lets failingStore errors be matched with errors.Is.
type storeDownError struct{}

func (storeDownError) Error() string { return "store down" }

var errStoreDown = storeDownError{}
