package auth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestHandleLoginRateLimitsPerClientIP exercises the login endpoint's
// per-client+username limiter: after the burst is spent, further attempts
// from the same address get 429 with a Retry-After hint, while a different
// address still reaches credential verification.
func TestHandleLoginRateLimitsPerClientIP(t *testing.T) {
	a := New(&Config{AdminUser: map[string]string{"admin": "correct-pass"}, LoginBurst: 1})

	login := func(remoteAddr, password string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login",
			strings.NewReader(`{"username":"admin","password":"`+password+`"}`))
		req.RemoteAddr = remoteAddr
		rec := httptest.NewRecorder()
		a.HandleLogin(rec, req)
		return rec
	}

	// First attempt spends the burst and reaches credential verification.
	if rec := login("10.9.9.9:1000", "wrong"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected the first attempt to hit verification (401), got %d", rec.Code)
	}
	// Second attempt from the same address is rate limited.
	rec := login("10.9.9.9:1000", "wrong-2")
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 after the burst is spent, got %d", rec.Code)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Error("expected a Retry-After hint on the 429 response")
	}
	// A different client address is not collateral damage.
	if rec := login("10.9.9.10:1000", "wrong"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected a different address to reach verification (401), got %d", rec.Code)
	}
}
