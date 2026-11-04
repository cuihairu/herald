package auth

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func loginToken(t *testing.T, a *Auth, username, password string) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"username":"`+username+`","password":"`+password+`"}`))
	req.RemoteAddr = "10.0.0.1:1234"
	rec := httptest.NewRecorder()
	a.HandleLogin(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("login failed: %d %s", rec.Code, rec.Body.String())
	}
	var out LoginResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode login response: %v", err)
	}
	return out.Token
}

func TestHandlePasswordChangeRotatesCredentials(t *testing.T) {
	a := New(&Config{AdminUser: map[string]string{"admin": "old-pass"}})
	token := loginToken(t, a, "admin", "old-pass")

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/password", strings.NewReader(`{"old_password":"old-pass","new_password":"new-pass-2"}`))
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	a.HandlePasswordChange(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d %s", rec.Code, rec.Body.String())
	}

	// 新密码能登录，旧密码被拒。
	loginToken(t, a, "admin", "new-pass-2")
	req = httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"username":"admin","password":"old-pass"}`))
	req.RemoteAddr = "10.0.0.2:1234"
	rec = httptest.NewRecorder()
	a.HandleLogin(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected the old password to stop working, got %d", rec.Code)
	}
}

func TestHandlePasswordChangePersistsViaStore(t *testing.T) {
	a := New(&Config{AdminUser: map[string]string{"admin": "old-pass"}})
	store := &stubPasswordStore{hashes: map[string]string{}}
	a.SetPasswordStore(store)

	token := loginToken(t, a, "admin", "old-pass")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/password", strings.NewReader(`{"old_password":"old-pass","new_password":"brand-new"}`))
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	a.HandlePasswordChange(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d %s", rec.Code, rec.Body.String())
	}
	hash, ok := store.hashes["admin"]
	if !ok || !strings.HasPrefix(hash, "$2") {
		t.Fatalf("expected a persisted bcrypt hash for admin, got %q (ok=%v)", hash, ok)
	}

	// 新进程用同一 store：存好的 hash 覆盖配置里的初始密码。
	reborn := New(&Config{AdminUser: map[string]string{"admin": "old-pass"}})
	reborn.SetPasswordStore(store)
	loginToken(t, reborn, "admin", "brand-new")
}

func TestHandlePasswordChangeRejections(t *testing.T) {
	a := New(&Config{AdminUser: map[string]string{"admin": "old-pass"}})
	token := loginToken(t, a, "admin", "old-pass")

	cases := []struct {
		name   string
		token  string
		body   string
		method string
		want   int
	}{
		{"wrong method", token, `{"old_password":"old-pass","new_password":"x-2"}`, http.MethodGet, http.StatusMethodNotAllowed},
		{"missing token", "", `{"old_password":"old-pass","new_password":"x-2"}`, http.MethodPost, http.StatusUnauthorized},
		{"invalid token", "bad.token.here", `{"old_password":"old-pass","new_password":"x-2"}`, http.MethodPost, http.StatusUnauthorized},
		{"bad body", token, `not-json`, http.MethodPost, http.StatusBadRequest},
		{"empty new password", token, `{"old_password":"old-pass","new_password":""}`, http.MethodPost, http.StatusBadRequest},
		{"wrong old password", token, `{"old_password":"nope","new_password":"x-2"}`, http.MethodPost, http.StatusBadRequest},
		{"over bcrypt limit", token, `{"old_password":"old-pass","new_password":"` + strings.Repeat("x", 100) + `"}`, http.MethodPost, http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, "/api/v1/auth/password", strings.NewReader(tc.body))
			if tc.token != "" {
				req.Header.Set("Authorization", "Bearer "+tc.token)
			}
			rec := httptest.NewRecorder()
			a.HandlePasswordChange(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("expected %d, got %d (%s)", tc.want, rec.Code, rec.Body.String())
			}
		})
	}
}

func TestHandlePasswordChangePersistFailureIs500(t *testing.T) {
	a := New(&Config{AdminUser: map[string]string{"admin": "old-pass"}})
	a.SetPasswordStore(&stubPasswordStore{failSet: true})

	token := loginToken(t, a, "admin", "old-pass")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/password", strings.NewReader(`{"old_password":"old-pass","new_password":"fresh-pass"}`))
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	a.HandlePasswordChange(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d (%s)", rec.Code, rec.Body.String())
	}
}

func TestSetPasswordStoreOverridesSeededPassword(t *testing.T) {
	store := &stubPasswordStore{hashes: map[string]string{"admin": "$2a$10$storedhashvaluefortest0000000000000000000000000000000000"}}
	// 换一个 hash 校验不过的密码做对照：覆盖后旧配置密码必然失效。
	a := New(&Config{AdminUser: map[string]string{"admin": "config-pass"}})
	a.SetPasswordStore(store)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"username":"admin","password":"config-pass"}`))
	req.RemoteAddr = "10.0.0.3:1"
	rec := httptest.NewRecorder()
	a.HandleLogin(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected the seeded password to be overridden, got %d", rec.Code)
	}
}

func TestSetPasswordStoreNilIsTolerated(t *testing.T) {
	a := New(&Config{AdminUser: map[string]string{"admin": "pass"}})
	a.SetPasswordStore(nil)
	loginToken(t, a, "admin", "pass")
}

type stubPasswordStore struct {
	hashes   map[string]string
	failSet  bool
	failRead bool
}

var errStubPersist = errors.New("stub persist failure")

func (s *stubPasswordStore) SetPasswordHash(username, hash string) error {
	if s.failSet {
		return errStubPersist
	}
	s.hashes[username] = hash
	return nil
}

func (s *stubPasswordStore) PasswordHash(username string) (string, bool, error) {
	if s.failRead {
		return "", false, errors.New("stub read failure")
	}
	hash, ok := s.hashes[username]
	return hash, ok, nil
}

func TestHandlePasswordChangePersistFailureKeepsOldPassword(t *testing.T) {
	a := New(&Config{AdminUser: map[string]string{"admin": "old-pass"}})
	a.SetPasswordStore(&stubPasswordStore{failSet: true})

	token := loginToken(t, a, "admin", "old-pass")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/password", strings.NewReader(`{"old_password":"old-pass","new_password":"fresh-pass"}`))
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	a.HandlePasswordChange(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 on persist failure, got %d", rec.Code)
	}
	loginToken(t, a, "admin", "old-pass")
}

func TestHandlePasswordChangeRateLimitsOldPasswordAttempts(t *testing.T) {
	a := New(&Config{AdminUser: map[string]string{"admin": "correct-pass"}, LoginBurst: 1})
	token := loginToken(t, a, "admin", "correct-pass")

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/password", strings.NewReader(`{"old_password":"wrong","new_password":"fresh-pass"}`))
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	a.HandlePasswordChange(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected the first bad attempt to reach verification (400), got %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/auth/password", strings.NewReader(`{"old_password":"wrong-again","new_password":"fresh-pass"}`))
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	a.HandlePasswordChange(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected the second attempt to be rate limited (429), got %d", rec.Code)
	}
}

func TestHandlePasswordChangeSerializesPersistence(t *testing.T) {
	a := New(&Config{AdminUser: map[string]string{"admin": "old-pass"}})
	store := &stubPasswordStore{hashes: map[string]string{}}
	a.SetPasswordStore(store)
	token := loginToken(t, a, "admin", "old-pass")

	// The lock is process-wide; a second change must validate against the
	// credential produced by the first, never persist an older hash after it.
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/password", strings.NewReader(`{"old_password":"old-pass","new_password":"first-new"}`))
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	a.HandlePasswordChange(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/auth/password", strings.NewReader(`{"old_password":"first-new","new_password":"second-new"}`))
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	a.HandlePasswordChange(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected chained 200, got %d: %s", rec.Code, rec.Body.String())
	}
	loginToken(t, a, "admin", "second-new")
}

func TestSetPasswordStoreReadFailureFailsClosed(t *testing.T) {
	store := &stubPasswordStore{failRead: true}
	a := New(&Config{AdminUser: map[string]string{"admin": "config-pass"}})
	a.SetPasswordStore(store)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"username":"admin","password":"config-pass"}`))
	req.RemoteAddr = "10.0.0.4:1"
	rec := httptest.NewRecorder()
	a.HandleLogin(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected the config seed to be disabled on a store read error, got %d", rec.Code)
	}
}
