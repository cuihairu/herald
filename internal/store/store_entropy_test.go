package store

import (
	"errors"
	"testing"
)

// TestCreateAPIKeySecondEntropyFailure covers the id-token draw: when the
// entropy source fails on the second randomToken call (after the secret
// token succeeded), CreateAPIKey must fail closed instead of minting a
// key with a guessable display id.
func TestCreateAPIKeySecondEntropyFailure(t *testing.T) {
	restore := randRead
	defer func() { randRead = restore }()

	calls := 0
	randRead = func(b []byte) (int, error) {
		calls++
		if calls == 2 {
			return 0, errors.New("entropy pool exhausted")
		}
		return len(b), nil
	}

	db, err := Open(t.TempDir() + "/creds.db")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateAPIKey("dashboard"); err == nil {
		t.Fatal("expected an error when the second entropy draw fails, got nil")
	}
}
