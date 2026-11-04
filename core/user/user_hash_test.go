package user

import (
	"errors"
	"testing"
)

func TestSetPasswordHashOverridesAndRejectsUnknownUsers(t *testing.T) {
	m := NewManager()
	if err := m.CreateDefaultUser("admin", "initial"); err != nil {
		t.Fatal(err)
	}

	if err := m.SetPasswordHash("admin", "$2a$10$abcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefab"); err != nil {
		t.Fatalf("set hash: %v", err)
	}
	hash, ok := m.StoredHash("admin")
	if !ok || hash != "$2a$10$abcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefab" {
		t.Fatalf("unexpected stored hash %q (ok=%v)", hash, ok)
	}

	if err := m.SetPasswordHash("ghost", "$2a$10$x"); err == nil {
		t.Fatal("expected an error for an unknown user")
	}
	if _, ok := m.StoredHash("ghost"); ok {
		t.Fatal("expected no hash for an unknown user")
	}
}

func TestChangePasswordStoresTheNewHash(t *testing.T) {
	m := NewManager()
	if err := m.CreateDefaultUser("admin", "old-pass"); err != nil {
		t.Fatal(err)
	}
	if err := m.ChangePassword("admin", "old-pass", "new-pass"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Authenticate("admin", "new-pass"); err != nil {
		t.Fatalf("expected the new password to authenticate: %v", err)
	}
	hash, ok := m.StoredHash("admin")
	if !ok || hash == "" {
		t.Fatal("expected a stored hash after the change")
	}
}

var _ = errors.New
