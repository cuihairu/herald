package groups

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMemoryStore(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()

	if _, err := s.Get(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	if err := s.Delete(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound on delete, got %v", err)
	}

	g1 := validGroup()
	if err := s.Put(ctx, g1); err != nil {
		t.Fatalf("Put: %v", err)
	}
	g2 := Group{ID: "devs", Members: []Member{{Channel: "log"}}}
	if err := s.Put(ctx, g2); err != nil {
		t.Fatalf("Put: %v", err)
	}

	got, err := s.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 2 || got[0].ID != "ops-oncall" || got[1].ID != "devs" {
		t.Fatalf("expected insertion order, got %v", got)
	}

	one, err := s.Get(ctx, "devs")
	if err != nil || one.ID != "devs" {
		t.Fatalf("Get(devs) = %+v, %v", one, err)
	}

	// Replace keeps position.
	g2b := g2
	g2b.Description = "the dev crowd"
	if err := s.Put(ctx, g2b); err != nil {
		t.Fatalf("Put(replace): %v", err)
	}
	got, _ = s.List(ctx)
	if len(got) != 2 || got[1].Description != "the dev crowd" {
		t.Fatalf("replace must keep position, got %v", got)
	}

	// Delete closes the gap and keeps ids dense.
	if err := s.Delete(ctx, "ops-oncall"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	got, _ = s.List(ctx)
	if len(got) != 1 || got[0].ID != "devs" {
		t.Fatalf("expected [devs] after delete, got %v", got)
	}
	devs, err := s.Get(ctx, "devs")
	if err != nil || devs.ID != "devs" {
		t.Fatalf("post-delete index must stay consistent: %+v, %v", devs, err)
	}
}

func TestFileStore(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "groups.json")

	s, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("NewFileStore on missing file: %v", err)
	}
	empty, err := s.List(ctx)
	if err != nil || len(empty) != 0 {
		t.Fatalf("missing file starts empty, got %v, %v", empty, err)
	}

	g := validGroup()
	if err := s.Put(ctx, g); err != nil {
		t.Fatalf("Put: %v", err)
	}
	g.Description = "rotated"
	if err := s.Put(ctx, g); err != nil {
		t.Fatalf("Put(replace): %v", err)
	}
	if err := s.Put(ctx, Group{ID: "devs", Members: []Member{{Channel: "log"}}}); err != nil {
		t.Fatalf("Put(devs): %v", err)
	}

	reopened, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	got, err := reopened.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 2 || got[0].ID != "ops-oncall" || got[0].Description != "rotated" || got[1].ID != "devs" {
		t.Fatalf("expected persisted state, got %v", got)
	}

	if err := reopened.Delete(ctx, "ops-oncall"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := reopened.Get(ctx, "ops-oncall"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}

	// A malformed file aborts construction — configuration errors surface
	// at startup, not on first write.
	malformed := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(malformed, []byte("{not json"), 0o644); err != nil {
		t.Fatalf("write malformed: %v", err)
	}
	if _, err := NewFileStore(malformed); err == nil {
		t.Fatal("malformed store file must abort construction")
	}
}

func TestFileStoreErrorPaths(t *testing.T) {
	ctx := context.Background()

	t.Run("unreadable path aborts construction", func(t *testing.T) {
		// A directory reads as an error (not as missing), which surfaces
		// at construction time.
		if _, err := NewFileStore(t.TempDir()); err == nil {
			t.Fatal("a directory must not work as a groups file")
		}
	})

	t.Run("put into a missing directory fails", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "missing-dir", "groups.json")
		s, err := NewFileStore(path)
		if err != nil {
			t.Fatalf("NewFileStore: %v", err)
		}
		if err := s.Put(ctx, validGroup()); err == nil {
			t.Fatal("expected write error into missing directory")
		}
	})

	t.Run("corruption after construction fails reads and writes", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "groups.json")
		s, err := NewFileStore(path)
		if err != nil {
			t.Fatalf("NewFileStore: %v", err)
		}
		if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
			t.Fatalf("corrupt: %v", err)
		}
		if _, err := s.List(ctx); err == nil {
			t.Fatal("List over a corrupted file must fail")
		}
		if _, err := s.Get(ctx, "ops-oncall"); err == nil {
			t.Fatal("Get over a corrupted file must fail")
		}
		if err := s.Put(ctx, validGroup()); err == nil {
			t.Fatal("Put over a corrupted file must fail")
		}
		if err := s.Delete(ctx, "ops-oncall"); err == nil {
			t.Fatal("Delete over a corrupted file must fail")
		}
	})

	t.Run("delete unknown id reports not found", func(t *testing.T) {
		s, err := NewFileStore(filepath.Join(t.TempDir(), "groups.json"))
		if err != nil {
			t.Fatalf("NewFileStore: %v", err)
		}
		if err := s.Delete(ctx, "ghost"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("expected ErrNotFound, got %v", err)
		}
	})
}

func TestFileStoreGetFound(t *testing.T) {
	ctx := context.Background()
	s, err := NewFileStore(filepath.Join(t.TempDir(), "groups.json"))
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	if err := s.Put(ctx, validGroup()); err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, err := s.Get(ctx, "ops-oncall")
	if err != nil || got.ID != "ops-oncall" || len(got.Members) != 2 {
		t.Fatalf("Get = %+v, %v", got, err)
	}
}

// Saving onto a path that is an existing directory fails at the final
// rename; saving nil must normalize to an empty list, not "groups": null.
// Both go through save directly — the public API always hands it non-nil
// lists except through paths covered above.
func TestFileStoreSaveDirect(t *testing.T) {
	t.Run("nil list saves as empty", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "groups.json")
		s := &FileStore{path: path}
		if err := s.save(nil); err != nil {
			t.Fatalf("save(nil): %v", err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if strings.Contains(string(data), "null") {
			t.Fatalf("nil must serialize as [], got %s", data)
		}
		reopened, err := NewFileStore(path)
		if err != nil {
			t.Fatalf("reopen: %v", err)
		}
		if got, _ := reopened.List(context.Background()); len(got) != 0 {
			t.Fatalf("expected empty list, got %v", got)
		}
	})

	t.Run("rename onto a directory fails", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "as-dir")
		if err := os.Mkdir(path, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		s := &FileStore{path: path}
		err := s.save([]Group{{ID: "g", Members: []Member{{Channel: "c"}}}})
		if err == nil || !strings.Contains(err.Error(), "groups: replace") {
			t.Fatalf("expected a rename failure, got %v", err)
		}
	})
}
