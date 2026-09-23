package rules

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func fileStoreRule(id, match string) Rule {
	return Rule{
		ID:    id,
		Match: match,
		Mode:  ModeActive,
		Route: []RouteStep{{Channels: []string{"c"}}},
	}
}

func TestFileStore(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "rules.json")

	// Missing file starts empty.
	s, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	got, err := s.List(ctx)
	if err != nil || len(got) != 0 {
		t.Fatalf("empty store: got %v err %v", got, err)
	}
	if _, err := s.Get(ctx, "a"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}

	a := fileStoreRule("a", `level == "error"`)
	b := fileStoreRule("b", `level == "info"`)
	if err := s.Put(ctx, a); err != nil {
		t.Fatalf("Put(a): %v", err)
	}
	if err := s.Put(ctx, b); err != nil {
		t.Fatalf("Put(b): %v", err)
	}

	// Persistence across reopen.
	s2, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	got, err = s2.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 2 || got[0].ID != "a" || got[1].ID != "b" {
		t.Fatalf("expected [a b], got %v", got)
	}

	// No stray temp file left behind.
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("temp file should not survive a save, stat err = %v", err)
	}

	// Update in place.
	a2 := a
	a2.Match = `level == "warning"`
	if err := s2.Put(ctx, a2); err != nil {
		t.Fatalf("Put(a2): %v", err)
	}
	got, _ = s2.List(ctx)
	if got[0].Match != a2.Match || got[1].ID != "b" {
		t.Fatalf("expected in-place update, got %v", got)
	}

	// Delete removes and keeps the rest.
	if err := s2.Delete(ctx, "a"); err != nil {
		t.Fatalf("Delete(a): %v", err)
	}
	if err := s2.Delete(ctx, "a"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound on double delete, got %v", err)
	}
	got, _ = s2.List(ctx)
	if len(got) != 1 || got[0].ID != "b" {
		t.Fatalf("expected [b], got %v", got)
	}
}

func TestFileStoreMalformedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rules.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := NewFileStore(path); err == nil {
		t.Fatal("expected error for malformed file")
	}
}

func TestFileStoreBadDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing-dir", "rules.json")
	if _, err := NewFileStore(path); err != nil {
		t.Fatalf("missing parent dir should start empty, got %v", err)
	}
	s, _ := NewFileStore(path)
	if err := s.Put(context.Background(), fileStoreRule("a", `1 == 1`)); err == nil {
		t.Fatal("expected write error into missing directory")
	}
}

func TestFileStoreFormat(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rules.json")
	s, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	if err := s.Put(context.Background(), fileStoreRule("a", `1 == 1`)); err != nil {
		t.Fatalf("Put: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var f map[string]any
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if f["version"] != float64(fileFormatVersion) {
		t.Errorf("expected version %d, got %v", fileFormatVersion, f["version"])
	}
	rulesList, ok := f["rules"].([]any)
	if !ok || len(rulesList) != 1 {
		t.Fatalf("expected one rule in file, got %v", f["rules"])
	}
}

func TestEngineWithFileStore(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "rules.json")
	store, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}

	engine := NewEngine(store)
	r := fileStoreRule("hot", `level == "error"`)
	if err := engine.Put(ctx, &r); err != nil {
		t.Fatalf("Put: %v", err)
	}

	// CRUD through the engine takes effect immediately (hot reload) and
	// persists (a fresh engine over the same file sees the rule).
	fresh := NewEngine(store)
	if err := fresh.Reload(ctx); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	got, err := fresh.Get(ctx, "hot")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ID != "hot" {
		t.Fatalf("expected rule hot, got %+v", got)
	}

	if err := fresh.Delete(ctx, "hot"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := fresh.Get(ctx, "hot"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestFileStoreLoadNonNotExistError(t *testing.T) {
	// A directory as the store path makes ReadFile fail with EISDIR —
	// neither success nor NotExist — which must abort construction.
	dir := t.TempDir()
	if _, err := NewFileStore(dir); err == nil {
		t.Fatal("expected error when the store path is a directory")
	}
}

func TestFileStoreMethodErrorsAfterLoadFailure(t *testing.T) {
	ctx := context.Background()
	s, err := NewFileStore(filepath.Join(t.TempDir(), "rules.json"))
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	if err := s.Put(ctx, fileStoreRule("a", `1 == 1`)); err != nil {
		t.Fatalf("Put: %v", err)
	}

	// Point the store at a directory: load now fails for every method.
	s.path = t.TempDir()
	if _, err := s.Get(ctx, "a"); err == nil {
		t.Error("Get: expected load error")
	}
	if err := s.Put(ctx, fileStoreRule("b", `1 == 1`)); err == nil {
		t.Error("Put: expected load error")
	}
	if err := s.Delete(ctx, "a"); err == nil {
		t.Error("Delete: expected load error")
	}
}

func TestFileStoreSaveNilList(t *testing.T) {
	// save(nil) is a defensive path: it must still produce a valid,
	// versioned file with an empty rules array.
	s, err := NewFileStore(filepath.Join(t.TempDir(), "rules.json"))
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	if err := s.save(nil); err != nil {
		t.Fatalf("save(nil): %v", err)
	}
	got, err := s.load()
	if err != nil || len(got) != 0 {
		t.Fatalf("expected empty list after save(nil), got %v err %v", got, err)
	}
}
