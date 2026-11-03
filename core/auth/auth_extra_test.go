package auth

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cuihairu/herald/core/jwt"
	"github.com/cuihairu/herald/core/limiter"
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

// TestLoginRateLimiterRejectsNewWhenFullAndActive: when the table is at
// cap and every key is still active (none idle), a new key must not evict
// an active bucket. The evicted bucket would be rebuilt full the next
// time it is used, so a flood of distinct keys could reset the rate
// budget of any key the attacker chooses. The new key is refused instead.
func TestLoginRateLimiterRejectsNewWhenFullAndActive(t *testing.T) {
	limiter := newLoginRateLimiter(1, 100)
	now := time.Now()
	for i := 0; i < loginLimiterMaxKeys; i++ {
		key := "192.0.2.1|user-" + string(rune('a'+i%26)) + time.Duration(i).String()
		limiter.keys[key] = nil
		limiter.lastSeen[key] = now
	}

	if limiter.allow("192.0.2.2|new-user") {
		t.Fatal("new key must be refused when the table is full with active keys")
	}
	if got := len(limiter.keys); got != loginLimiterMaxKeys {
		t.Fatalf("limiter key table = %d, want unchanged %d", got, loginLimiterMaxKeys)
	}
}

// TestLoginRateLimiterFallbackPreservesActiveBudget: a key whose bucket has
// been exhausted stays exhausted when the table is full with active keys —
// another key's arrival cannot reset it.
func TestLoginRateLimiterFallbackPreservesActiveBudget(t *testing.T) {
	limiter := newLoginRateLimiter(1, 1)
	now := time.Now()
	limiter.keys["192.0.2.1|exhausted"] = newTokenBucketForTest(1, 1)
	limiter.lastSeen["192.0.2.1|exhausted"] = now
	_ = limiter.allow("192.0.2.1|exhausted") // spends the single token
	if limiter.allow("192.0.2.1|exhausted") {
		t.Fatal("exhausted key must be denied before table pressure")
	}
	// Fill the remaining table so the exhausted key is surrounded by
	// active keys and cannot be evicted to make room.
	for i := 1; i < loginLimiterMaxKeys; i++ {
		key := "192.0.2.1|filler-" + time.Duration(i).String()
		// Fillers are never admitted; a nil bucket is enough to occupy
		// the table and keeps the test light under -race.
		limiter.keys[key] = nil
		limiter.lastSeen[key] = now
	}
	if limiter.allow("192.0.2.1|new-key") {
		t.Fatal("new key must be refused when full and active")
	}
	if limiter.allow("192.0.2.1|exhausted") {
		t.Fatal("exhausted key must stay denied; table pressure must not reset it")
	}
}

// TestLoginRateLimiterEvictIdleFloorOne covers the idle floor: a table with
// only two idle keys drops the older one, never zero.
func TestLoginRateLimiterEvictIdleFloorOne(t *testing.T) {
	limiter := newLoginRateLimiter(1, 100)
	now := time.Now()
	limiter.keys["a"] = newTokenBucketForTest(1, 1)
	limiter.lastSeen["a"] = now.Add(-2 * time.Hour)
	limiter.keys["b"] = newTokenBucketForTest(1, 1)
	limiter.lastSeen["b"] = now.Add(-1 * time.Hour)

	limiter.evictIdleLocked(time.Hour)
	if len(limiter.keys) != 0 {
		t.Fatalf("keys after evict = %d, want 0 (both idle past ttl)", len(limiter.keys))
	}
}

func newTokenBucketForTest(rate float64, burst int) *limiter.TokenBucket {
	return limiter.NewTokenBucket(rate, burst)
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
