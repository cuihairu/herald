package jwt

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"
)

// forgeToken builds a token whose signature is valid for the given claims
// segment, allowing the claim-decoding error paths to be exercised.
func forgeToken(m *Manager, claimsEncoded string) string {
	payload := m.header() + "." + claimsEncoded
	return payload + "." + m.sign(payload)
}

func TestManagerInitFailureFailsClosed(t *testing.T) {
	m := &Manager{initErr: errors.New("no entropy")}

	if token, err := m.Generate("u1", "alice", "admin"); err == nil || token != "" {
		t.Fatalf("Generate() = %q, %v; want empty token and the init error", token, err)
	}
	if claims, err := m.Validate("a.b.c"); err == nil || claims != nil {
		t.Fatalf("Validate() = %v, %v; want nil claims and the init error", claims, err)
	}
}

// RandRead is swapped for the duration of this test, so it must not run
// concurrently with any other NewManager("") call in this package.
func TestNewManagerEntropyFailureFailsClosed(t *testing.T) {
	orig := RandRead
	RandRead = func([]byte) (int, error) { return 0, errors.New("no entropy") }
	defer func() { RandRead = orig }()

	m := NewManager("")
	if m.initErr == nil {
		t.Fatal("NewManager(\"\") with failing entropy must record initErr")
	}
	if token, err := m.Generate("u1", "alice", "admin"); err == nil || token != "" {
		t.Fatalf("Generate() = %q, %v; want empty token and the init error", token, err)
	}
}

func TestManager_ValidateInvalidClaimsEncoding(t *testing.T) {
	m := NewManager("test-secret-key")

	token := forgeToken(m, "!!!not-valid-base64-url!!!")
	_, err := m.Validate(token)
	if err == nil || !strings.Contains(err.Error(), "invalid claims encoding") {
		t.Errorf("Validate() error = %v, want invalid claims encoding", err)
	}
}

func TestManager_ValidateInvalidClaimsJSON(t *testing.T) {
	m := NewManager("test-secret-key")

	claimsEncoded := base64.URLEncoding.EncodeToString([]byte("this is not json"))
	token := forgeToken(m, claimsEncoded)

	_, err := m.Validate(token)
	if err == nil || !strings.Contains(err.Error(), "invalid claims json") {
		t.Errorf("Validate() error = %v, want invalid claims json", err)
	}
}

func TestManager_ValidateExpiredToken(t *testing.T) {
	m := NewManager("test-secret-key")

	// Forge a token whose claims carry an expiration in the past.
	claimsJSON := `{"user_id":"u1","username":"alice","role":"admin","exp":1000000000,"iat":999999999}`
	claimsEncoded := base64.URLEncoding.EncodeToString([]byte(claimsJSON))
	token := forgeToken(m, claimsEncoded)

	_, err := m.Validate(token)
	if err == nil || !strings.Contains(err.Error(), "token expired") {
		t.Errorf("Validate() error = %v, want token expired", err)
	}
}

// The JSONMarshal override must not run concurrently with any other test in
// this package, so this file deliberately uses no t.Parallel.
func TestManager_GenerateMarshalError(t *testing.T) {
	m := NewManager("test-secret-key")

	orig := JSONMarshal
	JSONMarshal = func(v any) ([]byte, error) { return nil, errors.New("boom") }
	defer func() { JSONMarshal = orig }()

	token, err := m.Generate("u1", "alice", "admin")
	if err == nil {
		t.Fatal("Generate() with failing marshal should fail")
	}
	if token != "" {
		t.Errorf("Generate() token = %q, want empty on failure", token)
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Errorf("Generate() error = %v, want injected marshal failure", err)
	}
}
