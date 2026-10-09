package user

import (
	"encoding/base64"
	"os"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

// TestMain drops the bcrypt cost for the whole test suite: cost 10 keeps
// logins fast in production but makes hashing the dominant cost of the
// tests under -race.
func TestMain(m *testing.M) {
	bcryptCost = bcrypt.MinCost
	os.Exit(m.Run())
}

// seedUserWithEmptyPassword registers a user whose stored password hash is the
// empty string. The real constructors always store a hashPassword result, so
// this is the only way to reach the verify-failure branches.
func seedUserWithEmptyPassword(m *Manager, username string) {
	m.users[username] = &User{
		ID:       "id-" + username,
		Username: username,
		Role:     "admin",
	}
}

// legacyHash encodes a password in the pre-bcrypt format
// base64(password+salt) + "." + base64(salt), with a fixed salt so tests
// are deterministic.
func legacyHash(password string) string {
	salt := []byte("0123456789abcdef")
	combined := append([]byte(password), salt...)
	return base64.StdEncoding.EncodeToString(combined) + "." + base64.StdEncoding.EncodeToString(salt)
}

func TestManager_AuthenticateRejectsUnverifiablePassword(t *testing.T) {
	mgr := NewManager()
	seedUserWithEmptyPassword(mgr, "ghost")

	user, err := mgr.Authenticate("ghost", "whatever")
	if err == nil {
		t.Fatal("expected error when the stored password hash cannot be verified")
	}
	if err.Error() != "invalid password" {
		t.Errorf("expected 'invalid password' error, got %v", err)
	}
	if user != nil {
		t.Errorf("expected nil user on failure, got %+v", user)
	}
}

func TestManager_ChangePasswordRejectsUnverifiableOldPassword(t *testing.T) {
	mgr := NewManager()
	seedUserWithEmptyPassword(mgr, "ghost")

	err := mgr.ChangePassword("ghost", "old", "new")
	if err == nil {
		t.Fatal("expected error when the old password hash cannot be verified")
	}
	if err.Error() != "invalid old password" {
		t.Errorf("expected 'invalid old password' error, got %v", err)
	}

	// The stored password must be untouched by the failed change.
	if got := mgr.users["ghost"].Password; got != "" {
		t.Errorf("expected stored password to remain empty, got %q", got)
	}
}

// TestManager_AuthenticateRejectsMalformedHashTail registers a user whose
// stored hash carries a dot but a tail that is not valid base64 — verify
// must fail on the undecodable salt, not accept the password.
func TestManager_AuthenticateRejectsMalformedHashTail(t *testing.T) {
	mgr := NewManager()
	mgr.users["broken"] = &User{
		ID:       "id-broken",
		Username: "broken",
		Role:     "admin",
		Password: "abc.!!!not-base64!!!",
	}

	if _, err := mgr.Authenticate("broken", "whatever"); err == nil {
		t.Error("expected error when the stored hash tail is not valid base64")
	}
}

// TestManager_AuthenticateAcceptsOnlyConfiguredPassword proves the
// configured password round-trips through the hash while a different one
// is rejected for the same stored hash.
func TestManager_AuthenticateAcceptsOnlyConfiguredPassword(t *testing.T) {
	mgr := NewManager()
	if err := mgr.CreateDefaultUser("ops", "s3cret!"); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := mgr.Authenticate("ops", "s3cret!"); err != nil {
		t.Errorf("configured password must authenticate, got %v", err)
	}
	if _, err := mgr.Authenticate("ops", "s3cret?"); err == nil {
		t.Error("a lookalike password must be rejected")
	}
	if _, err := mgr.Authenticate("ops", ""); err == nil {
		t.Error("an empty password must be rejected")
	}
}

// TestManager_AuthenticateMigratesLegacyHashOnLogin seeds the pre-bcrypt
// hash format and logs in: the login must succeed and the stored hash must
// come out as bcrypt, so the next login takes the bcrypt path.
func TestManager_AuthenticateMigratesLegacyHashOnLogin(t *testing.T) {
	mgr := NewManager()
	mgr.users["legacy"] = &User{
		ID:       "id-legacy",
		Username: "legacy",
		Role:     "admin",
		Password: legacyHash("oldpw"),
	}

	if _, err := mgr.Authenticate("legacy", "oldpw"); err != nil {
		t.Fatalf("legacy login: %v", err)
	}
	got := mgr.users["legacy"].Password
	if !isBcryptHash(got) {
		t.Fatalf("expected stored hash upgraded to bcrypt, got %q", got)
	}
	// The upgraded hash must verify on the next login.
	if _, err := mgr.Authenticate("legacy", "oldpw"); err != nil {
		t.Errorf("login after migration: %v", err)
	}
	if _, err := mgr.Authenticate("legacy", "wrong"); err == nil {
		t.Error("wrong password must still be rejected after migration")
	}
}

// TestManager_MigratePasswordKeepsLegacyHashWhenBcryptRefuses covers the
// bcrypt input limit: a legacy login with an over-72-byte password keeps
// the legacy hash — verification works, only the upgrade is deferred.
func TestManager_MigratePasswordKeepsLegacyHashWhenBcryptRefuses(t *testing.T) {
	mgr := NewManager()
	long := strings.Repeat("a", 73)
	mgr.users["legacy"] = &User{
		ID:       "id-legacy",
		Username: "legacy",
		Role:     "admin",
		Password: legacyHash(long),
	}

	if _, err := mgr.Authenticate("legacy", long); err != nil {
		t.Fatalf("legacy login with long password: %v", err)
	}
	if got := mgr.users["legacy"].Password; got != legacyHash(long) {
		t.Fatalf("expected legacy hash kept, got %q", got)
	}
}

// TestManager_MigratePasswordNoOpsOnDrift covers both guards of the
// migration write: a drifted stored hash and an already-deleted user.
func TestManager_MigratePasswordNoOpsOnDrift(t *testing.T) {
	mgr := NewManager()
	mgr.users["ops"] = &User{ID: "id-ops", Username: "ops", Role: "admin", Password: "current-hash"}

	// The stored hash no longer matches what the login verified.
	mgr.migratePassword("ops", "stale-hash", "pw")
	if mgr.users["ops"].Password != "current-hash" {
		t.Fatalf("expected drifted hash untouched, got %q", mgr.users["ops"].Password)
	}

	// The user vanished between the match and the migration.
	mgr.migratePassword("ghost", "stale-hash", "pw")

	// bcrypt refusing the input leaves the hash untouched as well.
	mgr.migratePassword("ops", "current-hash", strings.Repeat("b", 73))
	if mgr.users["ops"].Password != "current-hash" {
		t.Fatalf("expected hash untouched on bcrypt refusal, got %q", mgr.users["ops"].Password)
	}
}

// TestManager_CreatorsRejectTooLongPasswords drives the now-reachable
// error branches after hashPassword in CreateDefaultUser, CreateUser and
// ChangePassword: bcrypt refuses inputs over 72 bytes and the manager
// must surface that instead of storing a placeholder.
func TestManager_CreatorsRejectTooLongPasswords(t *testing.T) {
	long := strings.Repeat("a", 73)

	mgr := NewManager()
	if err := mgr.CreateDefaultUser("admin", long); err == nil {
		t.Error("CreateDefaultUser: expected error for a 73-byte password")
	}
	if _, err := mgr.GetUser("admin"); err == nil {
		t.Error("CreateDefaultUser: user must not be created")
	}

	if err := mgr.CreateUser("dev", long, "editor"); err == nil {
		t.Error("CreateUser: expected error for a 73-byte password")
	}
	if _, err := mgr.GetUser("dev"); err == nil {
		t.Error("CreateUser: user must not be created")
	}

	mgr2 := NewManager()
	if err := mgr2.CreateDefaultUser("ops", "oldpw"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := mgr2.ChangePassword("ops", "oldpw", long); err == nil {
		t.Error("ChangePassword: expected error for a 73-byte new password")
	}
	if _, err := mgr2.Authenticate("ops", "oldpw"); err != nil {
		t.Errorf("ChangePassword: old password must keep working, got %v", err)
	}
}
