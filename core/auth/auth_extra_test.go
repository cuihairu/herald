package auth

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cuihairu/herald/core/jwt"
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

// Note: the `if err != nil` branch after jwtManager.Generate in HandleLogin
// (auth.go:156) is only reachable by injecting a marshal failure — see
// TestAuth_HandleLoginGenerateError, which overrides jwt.JSONMarshal.

// TestAuth_HandleLoginGenerateError drives the jwtManager.Generate failure
// branch in HandleLogin by injecting a failing claims marshaler. The override
// window is kept minimal and the package uses no t.Parallel, so no other test
// in this binary runs a JWT call concurrently.
func TestAuth_HandleLoginGenerateError(t *testing.T) {
	a := New(&Config{
		AdminUser: map[string]string{"admin": "password123"},
	})

	orig := jwt.JSONMarshal
	jwt.JSONMarshal = func(v any) ([]byte, error) { return nil, errors.New("boom") }
	defer func() { jwt.JSONMarshal = orig }()

	body := `{"username": "admin", "password": "password123"}`
	req := httptest.NewRequest("POST", "/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	a.HandleLogin(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "failed to generate token") {
		t.Errorf("expected 'failed to generate token' message, got %q", w.Body.String())
	}
}

func TestLoginRateLimiterEvictsIdleKeys(t *testing.T) {
	limiter := newLoginRateLimiter(1, 100)

	// Simulate a cap-sized table where every key has gone idle. allow
	// must keep the table bounded instead of inserting the 10001st key.
	for i := 0; i < loginLimiterMaxKeys; i++ {
		key := "192.0.2.1|user-" + string(rune('a'+i%26)) + time.Duration(i).String()
		limiter.keys[key] = nil
		limiter.lastSeen[key] = time.Now().Add(-loginLimiterIdleTTL - time.Second)
	}

	if !limiter.allow("192.0.2.2|new-user") {
		t.Fatal("new key must be allowed after eviction")
	}
	if got := len(limiter.keys); got > loginLimiterMaxKeys {
		t.Fatalf("limiter key table = %d, want <= %d", got, loginLimiterMaxKeys)
	}
	if _, ok := limiter.keys["192.0.2.2|new-user"]; !ok {
		t.Fatal("new key was not retained after eviction")
	}
}

// TestLoginRateLimiterEvictsRecentFallback covers the cap-path fallback: when
// every key is still active (none idle), the sweep drops the oldest quarter
// instead of letting the table grow past the cap.
func TestLoginRateLimiterEvictsRecentFallback(t *testing.T) {
	limiter := newLoginRateLimiter(1, 100)
	now := time.Now()
	for i := 0; i < loginLimiterMaxKeys; i++ {
		key := "192.0.2.1|user-" + string(rune('a'+i%26)) + time.Duration(i).String()
		limiter.keys[key] = nil
		limiter.lastSeen[key] = now
	}

	if !limiter.allow("192.0.2.2|new-user") {
		t.Fatal("new key must be allowed after fallback eviction")
	}
	if got := len(limiter.keys); got >= loginLimiterMaxKeys {
		t.Fatalf("limiter key table = %d, want < %d after fallback", got, loginLimiterMaxKeys)
	}
}

// TestLoginRateLimiterEvictFallbackFloorOne covers the floor guard: a table
// smaller than four active keys drops exactly the oldest one, never zero.
func TestLoginRateLimiterEvictFallbackFloorOne(t *testing.T) {
	limiter := newLoginRateLimiter(1, 100)
	now := time.Now()
	limiter.keys["a"] = nil
	limiter.lastSeen["a"] = now.Add(-2 * time.Second)
	limiter.keys["b"] = nil
	limiter.lastSeen["b"] = now.Add(-1 * time.Second)

	limiter.evictIdleLocked(time.Hour)
	if len(limiter.keys) != 1 {
		t.Fatalf("keys after evict = %d, want 1 (drop floors at one)", len(limiter.keys))
	}
	if _, ok := limiter.keys["a"]; ok {
		t.Fatal("oldest key was not dropped")
	}
}

// TestLoginRateLimiterNegativeRateDisables covers the disable path: a negative
// configured rate yields a disabled limiter that admits every request, and a
// nil limiter admits too.
func TestLoginRateLimiterNegativeRateDisables(t *testing.T) {
	limiter := newLoginRateLimiter(-1, 5)
	if limiter.enabled {
		t.Fatal("negative rate must disable the limiter")
	}
	if !limiter.allow("192.0.2.1|user-a") {
		t.Fatal("disabled limiter must admit")
	}
	if !(*loginRateLimiter)(nil).allow("192.0.2.1|user-b") {
		t.Fatal("nil limiter must admit")
	}
}

// TestClientIP covers the RemoteAddr fallbacks: a host:port split, a raw value
// that cannot be split, and an empty RemoteAddr.
func TestClientIP(t *testing.T) {
	if got := clientIP(&http.Request{RemoteAddr: "10.0.0.1:1234"}); got != "10.0.0.1" {
		t.Errorf("host:port = %q, want host", got)
	}
	if got := clientIP(&http.Request{RemoteAddr: "no-port"}); got != "no-port" {
		t.Errorf("unsplittable RemoteAddr = %q, want raw", got)
	}
	if got := clientIP(&http.Request{}); got != "unknown" {
		t.Errorf("empty RemoteAddr = %q, want unknown", got)
	}
}
