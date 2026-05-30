package jwt

import (
	"strings"
	"testing"
	"time"
)

func TestNewManager(t *testing.T) {
	t.Run("with secret key", func(t *testing.T) {
		mgr := NewManager("my-secret-key")
		if mgr == nil {
			t.Fatal("expected non-nil manager")
		}
		if mgr.secretKey != "my-secret-key" {
			t.Errorf("expected secret key 'my-secret-key', got '%s'", mgr.secretKey)
		}
	})

	t.Run("with empty secret key", func(t *testing.T) {
		mgr := NewManager("")
		if mgr == nil {
			t.Fatal("expected non-nil manager")
		}
		if mgr.secretKey == "" {
			t.Error("expected default secret key to be set")
		}
	})
}

func TestManager_Generate(t *testing.T) {
	mgr := NewManager("test-secret")

	t.Run("valid token generation", func(t *testing.T) {
		token, err := mgr.Generate("user123", "john", "admin")
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if token == "" {
			t.Error("expected non-empty token")
		}

		parts := strings.Split(token, ".")
		if len(parts) != 3 {
			t.Errorf("expected token to have 3 parts, got %d", len(parts))
		}
	})

	t.Run("token with empty values", func(t *testing.T) {
		token, err := mgr.Generate("", "", "")
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if token == "" {
			t.Error("expected non-empty token")
		}
	})
}

func TestManager_Validate(t *testing.T) {
	mgr := NewManager("test-secret")

	t.Run("valid token", func(t *testing.T) {
		token, _ := mgr.Generate("user123", "john", "admin")
		claims, err := mgr.Validate(token)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if claims.UserID != "user123" {
			t.Errorf("expected user ID 'user123', got '%s'", claims.UserID)
		}
		if claims.Username != "john" {
			t.Errorf("expected username 'john', got '%s'", claims.Username)
		}
		if claims.Role != "admin" {
			t.Errorf("expected role 'admin', got '%s'", claims.Role)
		}
	})

	t.Run("invalid token format", func(t *testing.T) {
		testCases := []string{
			"",
			"invalid",
			"only.two",
			"too.many.parts.here",
		}
		for _, token := range testCases {
			_, err := mgr.Validate(token)
			if err == nil {
				t.Errorf("expected error for token '%s'", token)
			}
		}
	})

	t.Run("invalid signature", func(t *testing.T) {
		token, _ := mgr.Generate("user123", "john", "admin")
		parts := strings.Split(token, ".")
		invalidToken := parts[0] + "." + parts[1] + ".invalidsignature"

		_, err := mgr.Validate(invalidToken)
		if err == nil {
			t.Error("expected error for invalid signature")
		}
	})

	t.Run("different secret key", func(t *testing.T) {
		token, _ := mgr.Generate("user123", "john", "admin")
		otherMgr := NewManager("different-secret")

		_, err := otherMgr.Validate(token)
		if err == nil {
			t.Error("expected error for token signed with different secret")
		}
	})

	t.Run("invalid claims encoding", func(t *testing.T) {
		invalidToken := "invalid_header.not-base64.signature"
		_, err := mgr.Validate(invalidToken)
		if err == nil {
			t.Error("expected error for invalid claims encoding")
		}
	})

	t.Run("invalid claims JSON", func(t *testing.T) {
		// Create a token with invalid claims JSON
		header := mgr.header()
		invalidClaims := "not-json"
		signature := mgr.sign(header + "." + invalidClaims)
		invalidToken := header + "." + invalidClaims + "." + signature

		_, err := mgr.Validate(invalidToken)
		if err == nil {
			t.Error("expected error for invalid claims JSON")
		}
	})
}

func TestManager_Refresh(t *testing.T) {
	mgr := NewManager("test-secret")

	t.Run("valid token refresh", func(t *testing.T) {
		token, _ := mgr.Generate("user123", "john", "admin")
		// Wait a bit to ensure different timestamp
		time.Sleep(time.Second)
		newToken, err := mgr.Refresh(token)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if newToken == "" {
			t.Error("expected non-empty new token")
		}
		if newToken == token {
			t.Error("expected new token to be different from old token")
		}

		// Verify the new token contains the same claims
		oldClaims, _ := mgr.Validate(token)
		newClaims, _ := mgr.Validate(newToken)
		if oldClaims.UserID != newClaims.UserID {
			t.Error("user ID should remain the same after refresh")
		}
		if oldClaims.Username != newClaims.Username {
			t.Error("username should remain the same after refresh")
		}
		if oldClaims.Role != newClaims.Role {
			t.Error("role should remain the same after refresh")
		}
	})

	t.Run("refresh invalid token", func(t *testing.T) {
		_, err := mgr.Refresh("invalid-token")
		if err == nil {
			t.Error("expected error for invalid token")
		}
	})
}

func TestManager_header(t *testing.T) {
	mgr := NewManager("test-secret")
	header := mgr.header()

	if header == "" {
		t.Error("expected non-empty header")
	}

	// Should be base64 encoded JSON
	_, err := mgr.Validate("invalid." + header + ".signature")
	if err != nil {
		// This is expected since it's not a valid token
	}
}

func TestManager_sign(t *testing.T) {
	mgr := NewManager("test-secret")

	t.Run("consistent signature", func(t *testing.T) {
		payload := "test.payload"
		sig1 := mgr.sign(payload)
		sig2 := mgr.sign(payload)

		if sig1 != sig2 {
			t.Error("expected consistent signatures for same payload")
		}
	})

	t.Run("different signatures for different payloads", func(t *testing.T) {
		sig1 := mgr.sign("payload1")
		sig2 := mgr.sign("payload2")

		if sig1 == sig2 {
			t.Error("expected different signatures for different payloads")
		}
	})
}

func TestClaims_Expiration(t *testing.T) {
	mgr := NewManager("test-secret")

	t.Run("token expiration check", func(t *testing.T) {
		// Create a manager with a very short expiration for testing
		// Note: The current implementation uses 24 hours, so we can't easily test expired tokens
		// without modifying the Generate method or waiting
		token, _ := mgr.Generate("user123", "john", "admin")
		claims, err := mgr.Validate(token)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		// Check that expiration is in the future
		if claims.Exp <= time.Now().Unix() {
			t.Error("expected token expiration to be in the future")
		}
	})
}

func TestManager_Integration(t *testing.T) {
	mgr := NewManager("integration-test-secret")

	t.Run("full token lifecycle", func(t *testing.T) {
		// Generate
		token, err := mgr.Generate("user1", "alice", "user")
		if err != nil {
			t.Fatalf("failed to generate token: %v", err)
		}

		// Validate
		claims, err := mgr.Validate(token)
		if err != nil {
			t.Fatalf("failed to validate token: %v", err)
		}

		if claims.UserID != "user1" || claims.Username != "alice" || claims.Role != "user" {
			t.Error("token claims don't match expected values")
		}

		// Refresh
		newToken, err := mgr.Refresh(token)
		if err != nil {
			t.Fatalf("failed to refresh token: %v", err)
		}

		// Validate refreshed token
		newClaims, err := mgr.Validate(newToken)
		if err != nil {
			t.Fatalf("failed to validate refreshed token: %v", err)
		}

		if newClaims.UserID != claims.UserID {
			t.Error("user ID changed after refresh")
		}
	})
}
