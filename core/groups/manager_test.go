package groups

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func TestManagerCRUD(t *testing.T) {
	ctx := context.Background()
	m := NewManager(nil)

	if _, err := m.Get(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}

	g := validGroup()
	if err := m.Put(ctx, &g); err != nil {
		t.Fatalf("Put: %v", err)
	}
	// Invalid group never reaches the live table.
	bad := Group{ID: "bad", Members: nil}
	if err := m.Put(ctx, &bad); err == nil {
		t.Fatal("expected validation rejection")
	}
	if _, err := m.Get(ctx, "bad"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("invalid group must not be stored, got %v", err)
	}

	got, err := m.Get(ctx, "ops-oncall")
	if err != nil || len(got.Members) != 2 {
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

func TestManagerResolverSnapshot(t *testing.T) {
	ctx := context.Background()
	m := NewManager(nil)
	g := validGroup()
	if err := m.Put(ctx, &g); err != nil {
		t.Fatalf("Put: %v", err)
	}

	r := m.Resolver()
	members, ok := r.ExpandGroup("ops-oncall")
	if !ok || len(members) != 2 || members[0].Channel != "feishu-oncall" {
		t.Fatalf("ExpandGroup = %v, %v", members, ok)
	}
	if len(members[1].Recipients) != 1 || members[1].Recipients[0] != "13800000000" {
		t.Fatalf("recipient pins must ride along, got %v", members[1])
	}

	// Returned members are copies: mutating them must not leak into the
	// live table.
	members[0].Channel = "tampered"
	again, _ := r.ExpandGroup("ops-oncall")
	if again[0].Channel != "feishu-oncall" {
		t.Fatalf("resolver must return copies, got %v", again[0])
	}

	if _, ok := r.ExpandGroup("nope"); ok {
		t.Fatal("unknown group must report not-ok")
	}

	// Live updates are visible immediately — the hot path reads the same
	// table writes refresh.
	rotated := Group{ID: "ops-oncall", Members: []Member{{Channel: "wecom"}}}
	if err := m.Put(ctx, &rotated); err != nil {
		t.Fatalf("Put(rotate): %v", err)
	}
	members, _ = r.ExpandGroup("ops-oncall")
	if len(members) != 1 || members[0].Channel != "wecom" {
		t.Fatalf("rotation must be live, got %v", members)
	}

	if err := m.Delete(ctx, "ops-oncall"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, ok := r.ExpandGroup("ops-oncall"); ok {
		t.Fatal("deleted group must stop resolving")
	}
}

func TestManagerPersistence(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "groups.json")
	store, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}

	// Manager over an empty store: reload is a no-op that keeps serving.
	m := NewManager(store)
	if err := m.Reload(ctx); err != nil {
		t.Fatalf("Reload empty: %v", err)
	}

	g := validGroup()
	if err := m.Put(ctx, &g); err != nil {
		t.Fatalf("Put: %v", err)
	}

	// A second manager over the same file picks the persisted table up.
	m2 := NewManager(store)
	if err := m2.Reload(ctx); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	if _, err := m2.Get(ctx, "ops-oncall"); err != nil {
		t.Fatalf("persisted group must reload, got %v", err)
	}

	// Deleting through one manager is visible after the other reloads.
	if err := m.Delete(ctx, "ops-oncall"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if err := m2.Reload(ctx); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	if _, err := m2.Get(ctx, "ops-oncall"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted group must not reload, got %v", err)
	}
}

func TestManagerReloadRejectsInvalidStoredGroup(t *testing.T) {
	ctx := context.Background()
	// Sneak an invalid group past validation straight into the store (as
	// a manual file edit would).
	store := NewMemoryStoreWith(Group{ID: "rotten", Members: nil})
	m := NewManager(store)
	if err := m.Reload(ctx); err == nil {
		t.Fatal("invalid stored group must abort reload")
	}

	// The previous table keeps serving (here: it was empty).
	if got, _ := m.List(ctx); len(got) != 0 {
		t.Fatalf("reload failure must keep the old table, got %v", got)
	}
}

func TestManagerListNonEmpty(t *testing.T) {
	ctx := context.Background()
	m := NewManager(nil)
	if err := m.Put(ctx, &Group{ID: "a", Members: []Member{{Channel: "log"}}}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := m.Put(ctx, &Group{ID: "b", Members: []Member{{Channel: "log"}}}); err != nil {
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

func (failingStore) List(context.Context) ([]Group, error) { return nil, errStoreDown }
func (failingStore) Get(context.Context, string) (Group, error) {
	return Group{}, errStoreDown
}
func (failingStore) Put(context.Context, Group) error    { return errStoreDown }
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
		g := validGroup()
		if err := m.Put(ctx, &g); err == nil {
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
