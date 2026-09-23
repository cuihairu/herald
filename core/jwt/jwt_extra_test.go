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
