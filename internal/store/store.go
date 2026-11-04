// Package store persists operator-managed credentials (dashboard API
// keys and rotated password hashes) in SQLite. Plain key material is
// never stored: keys carry a sha-256 hash plus a masked display prefix.
package store

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

// APIKey is the stored (masked) view of an operator API key.
type APIKey struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Prefix    string    `json:"prefix"`
	CreatedAt time.Time `json:"created_at"`
	Revoked   bool      `json:"revoked"`
}

// CreatedKey pairs the masked record with the plain key, which the API
// returns exactly once at creation time.
type CreatedKey struct {
	Info  APIKey `json:"info"`
	Plain string `json:"key"`
}

// DB wraps the sqlite handle.
type DB struct {
	sql *sql.DB
}

var ErrNotFound = errors.New("store: not found")

// sqliteDriver 是驱动名接缝：固定为 "sqlite"，测试换名以覆盖 Open 的
// 错误分支（真实程序里驱动名是编译期常量）。
var sqliteDriver = "sqlite"

// migrateFunc 是迁移接缝：生产路径即 runMigrations。IF NOT EXISTS 让
// 同名 view/表冲突静默通过，真实失败只能来自损坏/只读的库文件；测试
// 在此注入失败以覆盖 Open 的迁移错误分支。
var migrateFunc = func(ex executor) error { return runMigrations(ex) }

// randRead 是熵源接缝；默认 crypto/rand。
var randRead = rand.Read

// Open creates (or opens) the sqlite database and runs migrations.
func Open(path string) (*DB, error) {
	handle, err := sql.Open(sqliteDriver, path)
	if err != nil {
		return nil, fmt.Errorf("store: open %s: %w", path, err)
	}
	// modernc/sqlite serializes writes per connection; a small pool plus
	// a busy timeout keeps concurrent API traffic from tripping SQLITE_BUSY.
	handle.SetMaxOpenConns(1)
	for _, pragma := range []string{
		"PRAGMA busy_timeout = 5000",
		"PRAGMA journal_mode = WAL",
	} {
		if _, err := handle.Exec(pragma); err != nil {
			_ = handle.Close()
			return nil, fmt.Errorf("store: pragma: %w", err)
		}
	}
	db := &DB{sql: handle}
	if err := migrateFunc(handle); err != nil {
		_ = handle.Close()
		return nil, err
	}
	return db, nil
}

// executor 是迁移的依赖接缝：生产传 *sql.DB，测试传失败桩。
type executor interface {
	Exec(query string, args ...any) (sql.Result, error)
}

func runMigrations(ex executor) error {
	for _, stmt := range []string{
		`CREATE TABLE IF NOT EXISTS api_keys (
			id         TEXT PRIMARY KEY,
			name       TEXT NOT NULL DEFAULT '',
			prefix     TEXT NOT NULL,
			hash       TEXT NOT NULL UNIQUE,
			created_at INTEGER NOT NULL,
			revoked    INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE TABLE IF NOT EXISTS user_credentials (
			username TEXT PRIMARY KEY,
			hash     TEXT NOT NULL
		)`,
	} {
		if _, err := ex.Exec(stmt); err != nil {
			return fmt.Errorf("store: migrate: %w", err)
		}
	}
	return nil
}

// Close releases the sqlite handle.
func (db *DB) Close() error {
	return db.sql.Close()
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func newKeyID() string {
	return randomToken(12)
}

func randomToken(n int) string {
	b := make([]byte, n)
	if _, err := randRead(b); err != nil {
		// crypto/rand 不可读在 Go 运行时里基本不可能；退化为时间戳熵。
		return fmt.Sprintf("%x", time.Now().UnixNano())
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// CreateAPIKey mints a new key: the plain value is returned once, only
// the hash and a display prefix are stored.
func (db *DB) CreateAPIKey(name string) (*CreatedKey, error) {
	plain := "wm_" + randomToken(24)
	id := newKeyID()
	prefix := plain[:10]
	createdAt := time.Now().Unix()

	hash := sha256Hex(plain)
	if _, err := db.sql.Exec(
		`INSERT INTO api_keys (id, name, prefix, hash, created_at, revoked) VALUES (?, ?, ?, ?, ?, 0)`,
		id, name, prefix, hash, createdAt,
	); err != nil {
		return nil, fmt.Errorf("store: insert api key: %w", err)
	}
	return &CreatedKey{
		Info: APIKey{
			ID:        id,
			Name:      name,
			Prefix:    prefix,
			CreatedAt: time.Unix(createdAt, 0),
		},
		Plain: plain,
	}, nil
}

// rowScanner 抽象 *sql.Rows 的迭代/扫描面：生产传 *sql.Rows，测试注入
// 失败桩以覆盖行级扫描与迭代错误分支（真实 sqlite 连接上无法确定性构造）。
type rowScanner interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}

// ListAPIKeys returns the masked records, newest first.
func (db *DB) ListAPIKeys() ([]*APIKey, error) {
	rows, err := db.sql.Query(`SELECT id, name, prefix, created_at, revoked FROM api_keys ORDER BY created_at DESC, id`)
	if err != nil {
		return nil, fmt.Errorf("store: list api keys: %w", err)
	}
	defer rows.Close()
	return scanAPIKeys(rows)
}

func scanAPIKeys(rows rowScanner) ([]*APIKey, error) {
	keys := []*APIKey{}
	for rows.Next() {
		var (
			k         APIKey
			createdAt int64
			revoked   int
		)
		if err := rows.Scan(&k.ID, &k.Name, &k.Prefix, &createdAt, &revoked); err != nil {
			return nil, fmt.Errorf("store: scan api key: %w", err)
		}
		k.CreatedAt = time.Unix(createdAt, 0)
		k.Revoked = revoked != 0
		keys = append(keys, &k)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list api keys: %w", err)
	}
	return keys, nil
}

// RevokeAPIKey marks the key inactive. Unknown ids are an error.
func (db *DB) RevokeAPIKey(id string) error {
	res, err := db.sql.Exec(`UPDATE api_keys SET revoked = 1 WHERE id = ? AND revoked = 0`, id)
	if err != nil {
		return fmt.Errorf("store: revoke api key: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ActiveKeyHashes returns the sha-256 hex hashes of all non-revoked keys.
func (db *DB) ActiveKeyHashes() (map[string]bool, error) {
	rows, err := db.sql.Query(`SELECT hash FROM api_keys WHERE revoked = 0`)
	if err != nil {
		return nil, fmt.Errorf("store: active hashes: %w", err)
	}
	defer rows.Close()
	return scanActiveHashes(rows)
}

func scanActiveHashes(rows rowScanner) (map[string]bool, error) {
	hashes := map[string]bool{}
	for rows.Next() {
		var hash string
		if err := rows.Scan(&hash); err != nil {
			return nil, fmt.Errorf("store: scan hash: %w", err)
		}
		hashes[hash] = true
	}
	return hashes, rows.Err()
}

// SetPasswordHash stores (or replaces) the bcrypt hash for a user.
func (db *DB) SetPasswordHash(username, hash string) error {
	if _, err := db.sql.Exec(
		`INSERT INTO user_credentials (username, hash) VALUES (?, ?)
		 ON CONFLICT(username) DO UPDATE SET hash = excluded.hash`,
		username, hash,
	); err != nil {
		return fmt.Errorf("store: set password hash: %w", err)
	}
	return nil
}

// PasswordHash returns the persisted hash for the user.
func (db *DB) PasswordHash(username string) (string, bool, error) {
	var hash string
	err := db.sql.QueryRow(`SELECT hash FROM user_credentials WHERE username = ?`, username).Scan(&hash)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("store: password hash: %w", err)
	}
	return hash, true, nil
}
