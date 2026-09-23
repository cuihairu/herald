package auth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAuth_MiddlewareJWTAuthentication(t *testing.T) {
	t.Run("valid JWT grants access", func(t *testing.T) {
		a := New(&Config{
			Enabled:   true,
			SecretKey: "jwt-secret",
			AdminUser: map[string]string{"admin": "password123"},
		})

		token, err := a.jwtManager.Generate("u1", "admin", "admin")
		if err != nil {
			t.Fatalf("failed to generate token: %v", err)
		}

		handlerCalled := false
		next := func(w http.ResponseWriter, r *http.Request) {
			handlerCalled = true
			w.WriteHeader(http.StatusOK)
		}

		req := httptest.NewRequest("GET", "/protected", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()

		a.Middleware(next).ServeHTTP(w, req)

		if !handlerCalled {
			t.Error("expected handler to be called with a valid JWT")
		}
		if w.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", w.Code)
		}
	})

	t.Run("invalid JWT is rejected", func(t *testing.T) {
		a := New(&Config{Enabled: true, SecretKey: "jwt-secret"})

		handlerCalled := false
		next := func(w http.ResponseWriter, r *http.Request) {
			handlerCalled = true
			w.WriteHeader(http.StatusOK)
		}

		req := httptest.NewRequest("GET", "/protected", nil)
		req.Header.Set("Authorization", "Bearer not-a-real-token")
		w := httptest.NewRecorder()

		a.Middleware(next).ServeHTTP(w, req)

		if handlerCalled {
			t.Error("expected handler NOT to be called with an invalid JWT")
		}
		if w.Code != http.StatusUnauthorized {
			t.Errorf("expected status 401, got %d", w.Code)
		}
	})
}

func TestAuth_HandleRefreshInvalidToken(t *testing.T) {
	a := New(&Config{SecretKey: "jwt-secret"})

	req := httptest.NewRequest("POST", "/refresh", nil)
	req.Header.Set("Authorization", "Bearer tampered.token.value")
	w := httptest.NewRecorder()

	a.HandleRefresh(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "invalid token") {
		t.Errorf("expected 'invalid token' message, got %q", w.Body.String())
	}
}

func TestAuth_HandleMeInvalidToken(t *testing.T) {
	a := New(&Config{SecretKey: "jwt-secret"})

	req := httptest.NewRequest("GET", "/me", nil)
	req.Header.Set("Authorization", "Bearer tampered.token.value")
	w := httptest.NewRecorder()

	a.HandleMe(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "invalid token") {
		t.Errorf("expected 'invalid token' message, got %q", w.Body.String())
	}
}

func TestAuth_HandleMeUserNotFound(t *testing.T) {
	// The token is valid, but no user record exists for its username.
	a := New(&Config{SecretKey: "jwt-secret"})

	token, err := a.jwtManager.Generate("u404", "ghost", "user")
	if err != nil {
		t.Fatalf("failed to generate token: %v", err)
	}

	req := httptest.NewRequest("GET", "/me", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()

	a.HandleMe(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "user not found") {
		t.Errorf("expected 'user not found' message, got %q", w.Body.String())
	}
}

// Exemption note: the `if err != nil` branch after jwtManager.Generate in
// HandleLogin (auth.go:156) is unreachable — Generate only marshals a fixed
// struct of strings and int64s, which json.Marshal cannot fail on.
