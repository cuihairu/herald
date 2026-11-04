package store

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

func openTempStore(t *testing.T) (*DB, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "herald.db")
	db, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db, path
}

func TestCreateAPIKeyReturnsPlainOnceAndListsMasked(t *testing.T) {
	db, _ := openTempStore(t)

	created, err := db.CreateAPIKey("CI 发布机")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if !strings.HasPrefix(created.Plain, "wm_") {
		t.Fatalf("expected a wm_ prefixed key, got %q", created.Plain)
	}
	if strings.Contains(created.Info.Prefix, created.Plain) || len(created.Info.Prefix) >= len(created.Plain) {
		t.Fatalf("expected a masked prefix, got %q for %q", created.Info.Prefix, created.Plain)
	}
	if created.Info.Name != "CI 发布机" || created.Info.Revoked {
		t.Fatalf("unexpected info %+v", created.Info)
	}

	keys, err := db.ListAPIKeys()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(keys) != 1 {
		t.Fatalf("expected 1 key, got %d", len(keys))
	}
	if keys[0].ID != created.Info.ID || keys[0].Prefix != created.Info.Prefix {
		t.Errorf("list mismatch: %+v vs %+v", keys[0], created.Info)
	}

	// 明文只出现一次：库里查不到明文，只有 hash。
	hashes, err := db.ActiveKeyHashes()
	if err != nil {
		t.Fatalf("hashes: %v", err)
	}
	if len(hashes) != 1 {
		t.Fatalf("expected 1 active hash, got %d", len(hashes))
	}
	for h := range hashes {
		if strings.Contains(created.Plain, h) {
			t.Fatal("the plain key must not be stored")
		}
	}
}

func TestKeyHashAuthenticatesThePresentedKey(t *testing.T) {
	db, _ := openTempStore(t)
	created, err := db.CreateAPIKey("ops")
	if err != nil {
		t.Fatal(err)
	}

	hashes, _ := db.ActiveKeyHashes()
	if !hashes[sha256Hex(created.Plain)] {
		t.Fatal("expected the presented key's hash to be active")
	}
}

func TestRevokeRemovesTheKeyFromActiveHashes(t *testing.T) {
	db, _ := openTempStore(t)
	first, _ := db.CreateAPIKey("a")
	second, _ := db.CreateAPIKey("b")

	if err := db.RevokeAPIKey(first.Info.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	hashes, _ := db.ActiveKeyHashes()
	if hashes[sha256Hex(first.Plain)] {
		t.Fatal("expected the revoked key to be inactive")
	}
	if !hashes[sha256Hex(second.Plain)] {
		t.Fatal("expected the other key to stay active")
	}

	keys, _ := db.ListAPIKeys()
	sort.Slice(keys, func(i, j int) bool { return keys[i].Name < keys[j].Name })
	if !keys[0].Revoked || keys[1].Revoked {
		t.Fatalf("expected revoked flag on the revoked key only: %+v %+v", keys[0], keys[1])
	}

	if err := db.RevokeAPIKey("missing-id"); err == nil {
		t.Fatal("expected an error revoking an unknown id")
	}
}

func TestPasswordHashRoundTrip(t *testing.T) {
	db, _ := openTempStore(t)
	if err := db.SetPasswordHash("admin", "$2a$10$hash"); err != nil {
		t.Fatalf("set: %v", err)
	}
	hash, ok, err := db.PasswordHash("admin")
	if err != nil || !ok || hash != "$2a$10$hash" {
		t.Fatalf("unexpected read: %q ok=%v err=%v", hash, ok, err)
	}
	if _, ok, _ := db.PasswordHash("ghost"); ok {
		t.Fatal("expected no hash for an unknown user")
	}
	// 覆写：改密后旧 hash 被替换。
	if err := db.SetPasswordHash("admin", "$2a$10$new"); err != nil {
		t.Fatal(err)
	}
	if hash, _, _ := db.PasswordHash("admin"); hash != "$2a$10$new" {
		t.Fatalf("expected the replaced hash, got %q", hash)
	}
}

func TestPersistenceAcrossReopen(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "herald.db")

	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	created, err := db.CreateAPIKey("persistent")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SetPasswordHash("admin", "$2a$10$kept"); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer reopened.Close()

	keys, _ := reopened.ListAPIKeys()
	if len(keys) != 1 || keys[0].Name != "persistent" || keys[0].ID != created.Info.ID {
		t.Fatalf("expected the key to survive the reopen, got %+v", keys)
	}
	if hash, ok, _ := reopened.PasswordHash("admin"); !ok || hash != "$2a$10$kept" {
		t.Fatalf("expected the hash to survive the reopen, got %q ok=%v", hash, ok)
	}
}

func TestOpenRejectsUnwritablePath(t *testing.T) {
	if _, err := Open(filepath.Join(t.TempDir(), "missing-dir", "herald.db")); err == nil {
		t.Fatal("expected an error for an unwritable path")
	}
}

func TestOpenRejectsUnknownDriver(t *testing.T) {
	restore := sqliteDriver
	sqliteDriver = "bogus-driver"
	defer func() { sqliteDriver = restore }()

	if _, err := Open(filepath.Join(t.TempDir(), "herald.db")); err == nil {
		t.Fatal("expected an error for an unknown driver")
	}
}

func TestOpenRejectsUnopenableTarget(t *testing.T) {
	// 目录打不开为数据库：迁移即失败。
	if _, err := Open(t.TempDir()); err == nil {
		t.Fatal("expected an error opening a directory as a database")
	}
}

func TestOpenRejectsCorruptFile(t *testing.T) {
	// 非数据库文件：busy_timeout 可过，journal_mode 失败 → 关闭句柄返回错误。
	path := filepath.Join(t.TempDir(), "herald.db")
	if err := os.WriteFile(path, []byte("this is not a sqlite database"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(path); err == nil {
		t.Fatal("expected an error opening a corrupt file")
	}
}

func TestOpenReportsMigrationFailure(t *testing.T) {
	// IF NOT EXISTS 让同名 view/表冲突静默通过，真实迁移失败只能来自
	// 损坏或只读的库文件；这里直接在迁移接缝上注入失败，验证 Open 把
	// 错误上抛并关闭句柄。
	restore := migrateFunc
	migrateFunc = func(executor) error { return errors.New("migrate down") }
	defer func() { migrateFunc = restore }()

	if _, err := Open(filepath.Join(t.TempDir(), "herald.db")); err == nil {
		t.Fatal("expected an error from a failing migration")
	}
}

// scanFailRows 在首次 Scan 即报错；iterFailRows 正常结束但 Err() 报错。
// 两者接在提取出的扫描辅助函数上，覆盖 ListAPIKeys/ActiveKeyHashes 的
// 扫描与迭代错误分支（真实 sqlite 连接上无法确定性构造行级错误）。
type scanFailRows struct{ err error }

func (r scanFailRows) Next() bool        { return true }
func (r scanFailRows) Scan(...any) error { return r.err }
func (r scanFailRows) Err() error        { return nil }

type iterFailRows struct{ err error }

func (r iterFailRows) Next() bool        { return false }
func (r iterFailRows) Scan(...any) error { return nil }
func (r iterFailRows) Err() error        { return r.err }

func TestListAPIKeysReportsRowErrors(t *testing.T) {
	if keys, err := scanAPIKeys(scanFailRows{err: errors.New("bad row")}); err == nil || !strings.Contains(err.Error(), "scan api key") || keys != nil {
		t.Fatalf("expected a scan error, got keys=%v err=%v", keys, err)
	}
	if keys, err := scanAPIKeys(iterFailRows{err: errors.New("iteration blew up")}); err == nil || !strings.Contains(err.Error(), "iteration blew up") || keys != nil {
		t.Fatalf("expected an iteration error, got keys=%v err=%v", keys, err)
	}
}

func TestActiveKeyHashesReportsRowErrors(t *testing.T) {
	hashes, err := scanActiveHashes(scanFailRows{err: errors.New("bad row")})
	if err == nil || !strings.Contains(err.Error(), "scan hash") || hashes != nil {
		t.Fatalf("expected a scan error, got hashes=%v err=%v", hashes, err)
	}
}

func TestMigrateReportsExecutorFailure(t *testing.T) {
	err := runMigrations(failExecutor{})
	if err == nil {
		t.Fatal("expected a migrate error from a failing executor")
	}
}

type failExecutor struct{}

func (failExecutor) Exec(string, ...any) (sql.Result, error) {
	return nil, errors.New("executor down")
}

func TestClosedDBReturnsErrors(t *testing.T) {
	db, _ := openTempStore(t)
	_ = db.Close()

	if _, err := db.CreateAPIKey("x"); err == nil {
		t.Error("expected an insert error on a closed handle")
	}
	if _, err := db.ListAPIKeys(); err == nil {
		t.Error("expected a list error on a closed handle")
	}
	if err := db.RevokeAPIKey("x"); err == nil {
		t.Error("expected a revoke error on a closed handle")
	}
	if _, err := db.ActiveKeyHashes(); err == nil {
		t.Error("expected an active-hash error on a closed handle")
	}
	if err := db.SetPasswordHash("u", "h"); err == nil {
		t.Error("expected a set error on a closed handle")
	}
	if _, _, err := db.PasswordHash("u"); err == nil {
		t.Error("expected a read error on a closed handle")
	}
}

func TestEntropyFallbackUsesTheClock(t *testing.T) {
	restore := randRead
	randRead = func([]byte) (int, error) { return 0, errors.New("no entropy") }
	defer func() { randRead = restore }()

	token := randomToken(8)
	if token == "" {
		t.Fatal("expected the clock-based fallback token")
	}
}

func TestCreateManyKeysKeepDistinctIDs(t *testing.T) {
	db, _ := openTempStore(t)
	seen := map[string]bool{}
	for i := 0; i < 5; i++ {
		created, err := db.CreateAPIKey("k")
		if err != nil {
			t.Fatal(err)
		}
		if seen[created.Info.ID] {
			t.Fatalf("duplicate id %q", created.Info.ID)
		}
		seen[created.Info.ID] = true
	}
}

var _ = time.Now
