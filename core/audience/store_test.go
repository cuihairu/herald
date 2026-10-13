package audience

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testEntry(id string, recips ...string) Entry {
	return Entry{ID: id, Recipients: recips}
}

func TestAudienceMemoryStore(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()

	if err := s.Delete(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound on delete, got %v", err)
	}

	if err := s.Put(ctx, testEntry("ops", "alice", "bob")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := s.Put(ctx, testEntry("devs", "carol")); err != nil {
		t.Fatalf("Put: %v", err)
	}

	got, err := s.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 2 || got[0].ID != "ops" || got[1].ID != "devs" {
		t.Fatalf("expected insertion order, got %v", got)
	}

	// Replace keeps position.
	if err := s.Put(ctx, testEntry("ops", "alice")); err != nil {
		t.Fatalf("Put(replace): %v", err)
	}
	got, _ = s.List(ctx)
	if len(got) != 2 || got[0].ID != "ops" || len(got[0].Recipients) != 1 {
		t.Fatalf("replace must keep position, got %v", got)
	}

	// Delete closes the gap and keeps the order dense.
	if err := s.Delete(ctx, "ops"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	got, _ = s.List(ctx)
	if len(got) != 1 || got[0].ID != "devs" {
		t.Fatalf("expected [devs] after delete, got %v", got)
	}
}

func TestAudienceFileStore(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "audiences.json")

	s, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("NewFileStore on missing file: %v", err)
	}
	empty, err := s.List(ctx)
	if err != nil || len(empty) != 0 {
		t.Fatalf("missing file starts empty, got %v, %v", empty, err)
	}

	if err := s.Put(ctx, testEntry("ops", "alice")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := s.Put(ctx, testEntry("ops", "alice", "bob")); err != nil {
		t.Fatalf("Put(replace): %v", err)
	}
	if err := s.Put(ctx, testEntry("devs", "carol")); err != nil {
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
	if len(got) != 2 || got[0].ID != "ops" || len(got[0].Recipients) != 2 || got[1].ID != "devs" {
		t.Fatalf("expected persisted state, got %v", got)
	}

	if err := reopened.Delete(ctx, "ops"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	got, _ = reopened.List(ctx)
	if len(got) != 1 || got[0].ID != "devs" {
		t.Fatalf("expected [devs] after delete, got %v", got)
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

func TestAudienceFileStoreErrorPaths(t *testing.T) {
	ctx := context.Background()

	t.Run("directory path aborts construction", func(t *testing.T) {
		if _, err := NewFileStore(t.TempDir()); err == nil {
			t.Fatal("a directory must not work as an audiences file")
		}
	})

	t.Run("put into a missing directory fails", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "missing-dir", "audiences.json")
		s, err := NewFileStore(path)
		if err != nil {
			t.Fatalf("NewFileStore: %v", err)
		}
		if err := s.Put(ctx, testEntry("ops", "alice")); err == nil {
			t.Fatal("expected write error into missing directory")
		}
	})

	t.Run("corruption after construction fails reads and writes", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "audiences.json")
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
		if err := s.Put(ctx, testEntry("ops", "alice")); err == nil {
			t.Fatal("Put over a corrupted file must fail")
		}
		if err := s.Delete(ctx, "ops"); err == nil {
			t.Fatal("Delete over a corrupted file must fail")
		}
	})

	t.Run("delete unknown id reports not found", func(t *testing.T) {
		s, err := NewFileStore(filepath.Join(t.TempDir(), "audiences.json"))
		if err != nil {
			t.Fatalf("NewFileStore: %v", err)
		}
		if err := s.Delete(ctx, "ghost"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("expected ErrNotFound, got %v", err)
		}
	})
}

// Saving onto a path that is an existing directory fails at the final
// rename; saving nil must normalize to an empty list, not "audiences": null.
func TestAudienceFileStoreSaveDirect(t *testing.T) {
	t.Run("nil list saves as empty", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "audiences.json")
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
		err := s.save([]Entry{testEntry("ops", "alice")})
		if err == nil || !strings.Contains(err.Error(), "audience: replace") {
			t.Fatalf("expected a rename failure, got %v", err)
		}
	})
}
