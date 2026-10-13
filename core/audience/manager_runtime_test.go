package audience

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

// failingAudienceStore fails every operation with a non-ErrNotFound error,
// standing in for a store outage: the live table must not move behind a
// failed persist.
type failingAudienceStore struct{ err error }

func (s failingAudienceStore) List(context.Context) ([]Entry, error) { return nil, s.err }
func (s failingAudienceStore) Put(context.Context, Entry) error      { return s.err }
func (s failingAudienceStore) Delete(context.Context, string) error  { return s.err }

func runtimeRecipients() map[string]Recipient {
	return map[string]Recipient{
		"alice": {Endpoints: []Endpoint{{Type: "feishu", Target: "@alice"}}},
		"bob":   {Endpoints: []Endpoint{{Type: "email", Target: "bob@x.io"}}},
	}
}

// TestManagerPutValidates pins the write-path validation: the same rules
// NewManager enforces on the config seed apply to every runtime write.
func TestManagerPutValidates(t *testing.T) {
	ctx := context.Background()
	m, err := NewManager(nil, nil, runtimeRecipients())
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	cases := []struct {
		name string
		id   string
		recs []string
		want string
	}{
		{"invalid id", "bad id!", []string{"alice"}, "invalid audience id"},
		{"empty recipients", "ops", nil, "at least one"},
		{"unknown recipient", "ops", []string{"ghost"}, "unknown recipient"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := m.Put(ctx, tc.id, Audience{Recipients: tc.recs})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Put(%s) = %v, want error containing %q", tc.name, err, tc.want)
			}
		})
	}

	// A valid write lands in the live table and the resolver sees it at
	// once — the hot path reads the same snapshot.
	if err := m.Put(ctx, "ops", Audience{Recipients: []string{"alice", "bob"}}); err != nil {
		t.Fatalf("Put(valid): %v", err)
	}
	eps, ok := m.ExpandUser("ops")
	if !ok || len(eps) != 2 {
		t.Fatalf("ExpandUser after Put = %v, %v", eps, ok)
	}
	got, err := m.Get(ctx, "ops")
	if err != nil || len(got.Recipients) != 2 {
		t.Fatalf("Get after Put = %+v, %v", got, err)
	}
}

// TestManagerReloadNilStore: a memory-only manager (nil store) keeps its
// config-seeded table through Reload — the call is a no-op, never an
// error or a wipe.
func TestManagerReloadNilStore(t *testing.T) {
	m, err := NewManager(nil, map[string]Audience{"ops": {Recipients: []string{"alice"}}}, runtimeRecipients())
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	if err := m.Reload(context.Background()); err != nil {
		t.Fatalf("Reload on a memory-only manager: %v", err)
	}
	if got, err := m.Get(context.Background(), "ops"); err != nil || len(got.Recipients) != 1 {
		t.Fatalf("table must survive the no-op reload, got %+v, %v", got, err)
	}
}

// TestManagerPutPersistsAndReloads: a write through a store-backed manager
// survives a fresh manager reading the same file.
func TestManagerPutPersistsAndReloads(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "audiences.json")
	store, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	m, err := NewManager(store, nil, runtimeRecipients())
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	if err := m.Reload(ctx); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	if err := m.Put(ctx, "ops", Audience{Recipients: []string{"alice"}}); err != nil {
		t.Fatalf("Put: %v", err)
	}

	// A fresh manager over the same store picks the entry up on reload.
	store2, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	m2, err := NewManager(store2, nil, runtimeRecipients())
	if err != nil {
		t.Fatalf("NewManager#2: %v", err)
	}
	if err := m2.Reload(ctx); err != nil {
		t.Fatalf("Reload#2: %v", err)
	}
	if got, err := m2.Get(ctx, "ops"); err != nil || len(got.Recipients) != 1 {
		t.Fatalf("expected the persisted audience, got %+v, %v", got, err)
	}
}

// TestManagerReloadRejectsInvalidEntry: an entry referencing an unknown
// recipient aborts the reload keeping the previous table serving.
func TestManagerReloadRejectsInvalidEntry(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "audiences.json")
	store, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	if err := store.Put(ctx, testEntry("ops", "ghost")); err != nil {
		t.Fatalf("seed store: %v", err)
	}

	m, err := NewManager(store, nil, runtimeRecipients())
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	if err := m.Put(ctx, "live", Audience{Recipients: []string{"alice"}}); err != nil {
		t.Fatalf("seed live: %v", err)
	}
	err = m.Reload(ctx)
	if err == nil || !strings.Contains(err.Error(), "unknown recipient") {
		t.Fatalf("reload must name the offender, got %v", err)
	}
	// The previous table keeps serving.
	if _, err := m.Get(ctx, "live"); err != nil {
		t.Fatalf("previous table must keep serving, got %v", err)
	}
}

// TestManagerDelete: deletion removes the live entry and persists; a
// "user:" reference to the deleted id fails loudly at expansion (false),
// never silently resolving to nothing.
func TestManagerDelete(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "audiences.json")
	store, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	m, err := NewManager(store, nil, runtimeRecipients())
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	if err := m.Reload(ctx); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	if err := m.Put(ctx, "ops", Audience{Recipients: []string{"alice"}}); err != nil {
		t.Fatalf("Put: %v", err)
	}

	if err := m.Delete(ctx, "ops"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := m.Get(ctx, "ops"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
	if _, ok := m.ExpandUser("ops"); ok {
		t.Fatal("deleted audience must not expand")
	}
	if err := m.Delete(ctx, "ops"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second delete: expected ErrNotFound, got %v", err)
	}

	// The store no longer holds it either.
	reopened, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	got, _ := reopened.List(ctx)
	if len(got) != 0 {
		t.Fatalf("store must be empty after delete, got %v", got)
	}
}

// TestManagerSeedOverStore pins the startup precedence: the config seed is
// Put after the store reload, so a same-id config entry overwrites the
// stored one and new ids append (the exact groups semantics).
func TestManagerSeedOverStore(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "audiences.json")
	store, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	if err := store.Put(ctx, testEntry("ops", "bob")); err != nil {
		t.Fatalf("seed store: %v", err)
	}

	m, err := NewManager(store, map[string]Audience{
		"ops": {Recipients: []string{"alice"}},
	}, runtimeRecipients())
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	if err := m.Reload(ctx); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	for id, aud := range map[string]Audience{"ops": {Recipients: []string{"alice"}}} {
		if err := m.Put(ctx, id, aud); err != nil {
			t.Fatalf("seed put: %v", err)
		}
	}

	got, err := m.Get(ctx, "ops")
	if err != nil || len(got.Recipients) != 1 || got.Recipients[0] != "alice" {
		t.Fatalf("config seed must win over the stored entry, got %+v, %v", got, err)
	}
	list, _ := m.List(ctx)
	if len(list) != 1 || list[0].Recipients[0] != "alice" {
		t.Fatalf("list = %v", list)
	}
}

// TestManagerStoreFailureKeepsLiveTable: a store outage must not move the
// live table — the failure reaches the caller and the previous snapshot
// keeps serving.
func TestManagerStoreFailureKeepsLiveTable(t *testing.T) {
	ctx := context.Background()
	m, err := NewManager(failingAudienceStore{err: errors.New("audience store offline")}, nil, runtimeRecipients())
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	if err := m.Reload(ctx); err == nil {
		t.Fatal("reload over a failing store must fail")
	}
	if err := m.Put(ctx, "ops", Audience{Recipients: []string{"alice"}}); err == nil {
		t.Fatal("put over a failing store must fail")
	}
	if _, ok := m.ExpandUser("ops"); ok {
		t.Fatal("live table must not move behind a failed persist")
	}
	if err := m.Delete(ctx, "ops"); err == nil {
		t.Fatal("delete over a failing store must fail")
	}
}

// TestManagerListOrderDeterministic: config-seeded ids list in sorted
// order (maps iterate randomly), a replace keeps position, a new id
// appends.
func TestManagerListOrderDeterministic(t *testing.T) {
	ctx := context.Background()
	m, err := NewManager(nil, map[string]Audience{
		"beta":  {Recipients: []string{"alice"}},
		"alpha": {Recipients: []string{"alice"}},
		"gamma": {Recipients: []string{"alice"}},
	}, runtimeRecipients())
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	list, err := m.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 3 || list[0].ID != "alpha" || list[1].ID != "beta" || list[2].ID != "gamma" {
		t.Fatalf("seed order must be sorted, got %v", list)
	}

	if err := m.Put(ctx, "alpha", Audience{Recipients: []string{"bob"}}); err != nil {
		t.Fatalf("Put(replace): %v", err)
	}
	if err := m.Put(ctx, "delta", Audience{Recipients: []string{"bob"}}); err != nil {
		t.Fatalf("Put(new): %v", err)
	}
	list, _ = m.List(ctx)
	if len(list) != 4 || list[0].ID != "alpha" || list[0].Recipients[0] != "bob" || list[3].ID != "delta" {
		t.Fatalf("replace keeps position, new id appends: %v", list)
	}
}
