package user

import "testing"

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

// Exemption note: the `if err != nil { return err }` branches after hashPassword
// in CreateDefaultUser (user.go:43), ChangePassword (user.go:104) and CreateUser
// (user.go:122) are unreachable — hashPassword never returns a non-nil error.
