package api

import (
	"net/http"
	"testing"

	"github.com/cuihairu/herald/core/auth"
)

// passwordAuthEnv 起一个开了认证的服务：登录拿 token 后走改密端点。
func passwordAuthEnv(t *testing.T) (*testEnv, string) {
	t.Helper()
	a := auth.New(&auth.Config{
		Enabled:   true,
		AdminUser: map[string]string{"admin": "old-pass"},
		SecretKey: "test-secret",
	})
	env := newTestEnv(t, func(cfg *Config) { cfg.Auth = a })

	code, resp := env.do(t, http.MethodPost, "/api/v1/auth/login", `{"username":"admin","password":"old-pass"}`, nil)
	if code != http.StatusOK {
		t.Fatalf("login failed: %d %v", code, resp)
	}
	token, _ := resp["token"].(string)
	if token == "" {
		t.Fatalf("expected a token, got %v", resp)
	}
	return env, token
}

func TestAuthPasswordEndpointRotatesPassword(t *testing.T) {
	env, token := passwordAuthEnv(t)

	headers := map[string]string{"Authorization": "Bearer " + token}
	code, resp := env.do(t, http.MethodPost, "/api/v1/auth/password",
		`{"old_password":"old-pass","new_password":"brand-new"}`, headers)
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d %v", code, resp)
	}

	// 新密码重登成功。
	code, resp = env.do(t, http.MethodPost, "/api/v1/auth/login", `{"username":"admin","password":"brand-new"}`, nil)
	if code != http.StatusOK {
		t.Fatalf("expected the new password to log in, got %d %v", code, resp)
	}
}

func TestAuthPasswordEndpointRejectsBadRequests(t *testing.T) {
	env, token := passwordAuthEnv(t)
	headers := map[string]string{"Authorization": "Bearer " + token}

	code, _ := env.do(t, http.MethodGet, "/api/v1/auth/password", "", headers)
	if code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", code)
	}

	code, _ = env.do(t, http.MethodPost, "/api/v1/auth/password",
		`{"old_password":"wrong","new_password":"x"}`, headers)
	if code != http.StatusBadRequest {
		t.Fatalf("expected 400 for a wrong old password, got %d", code)
	}
}

func TestAuthPasswordEndpointWithoutTokenIsUnauthorized(t *testing.T) {
	env, _ := passwordAuthEnv(t)
	code, _ := env.do(t, http.MethodPost, "/api/v1/auth/password", `{}`, nil)
	if code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", code)
	}
}
