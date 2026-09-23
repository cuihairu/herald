package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/cuihairu/herald/core/auth"
)

func TestNewServer(t *testing.T) {
	t.Run("default timeout", func(t *testing.T) {
		env := newTestEnv(t)
		if env.server.server.ReadTimeout != 30*time.Second {
			t.Errorf("expected read timeout 30s, got %v", env.server.server.ReadTimeout)
		}
		if env.server.server.WriteTimeout != 30*time.Second {
			t.Errorf("expected write timeout 30s, got %v", env.server.server.WriteTimeout)
		}
	})

	t.Run("custom timeout", func(t *testing.T) {
		env := newTestEnv(t, func(c *Config) { c.Timeout = 5 * time.Second })
		if env.server.server.ReadTimeout != 5*time.Second {
			t.Errorf("expected read timeout 5s, got %v", env.server.server.ReadTimeout)
		}
		if env.server.server.WriteTimeout != 5*time.Second {
			t.Errorf("expected write timeout 5s, got %v", env.server.server.WriteTimeout)
		}
	})

	t.Run("shutdown idle server", func(t *testing.T) {
		env := newTestEnv(t)
		if err := env.server.Shutdown(context.Background()); err != nil {
			t.Errorf("expected nil error, got %v", err)
		}
	})

	t.Run("start returns nil after shutdown", func(t *testing.T) {
		env := newTestEnv(t)
		done := make(chan error, 1)
		go func() {
			done <- env.server.Start(context.Background())
		}()
		time.Sleep(100 * time.Millisecond)
		if err := env.server.Shutdown(context.Background()); err != nil {
			t.Fatalf("shutdown failed: %v", err)
		}
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("expected nil error, got %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("Start did not return after Shutdown")
		}
	})

	t.Run("start returns error on invalid address", func(t *testing.T) {
		env := newTestEnv(t, func(c *Config) { c.Addr = "127.0.0.1:99999" })
		if err := env.server.Start(context.Background()); err == nil {
			t.Error("expected error for invalid address")
		}
	})

	t.Run("set websocket server", func(t *testing.T) {
		env := newTestEnv(t)
		env.server.SetWebSocketServer(nil)
		if env.server.handler._wsServer != nil {
			t.Error("expected nil websocket server")
		}
	})
}

func TestServer_Routes(t *testing.T) {
	env := newTestEnv(t)

	tests := []struct {
		name   string
		method string
		path   string
		status int
	}{
		{"status", http.MethodGet, "/api/v1/status", http.StatusOK},
		{"status wrong method", http.MethodPost, "/api/v1/status", http.StatusMethodNotAllowed},
		{"notify wrong method", http.MethodGet, "/api/v1/notify", http.StatusMethodNotAllowed},
		{"providers", http.MethodGet, "/api/v1/providers", http.StatusOK},
		{"providers wrong method", http.MethodPost, "/api/v1/providers", http.StatusMethodNotAllowed},
		{"workers", http.MethodGet, "/api/v1/workers", http.StatusOK},
		{"workers wrong method", http.MethodPost, "/api/v1/workers", http.StatusMethodNotAllowed},
		{"queue", http.MethodGet, "/api/v1/queue", http.StatusOK},
		{"queue wrong method", http.MethodPost, "/api/v1/queue", http.StatusMethodNotAllowed},
		{"logs", http.MethodGet, "/api/v1/logs", http.StatusOK},
		{"logs wrong method", http.MethodPost, "/api/v1/logs", http.StatusMethodNotAllowed},
		{"logs stats", http.MethodGet, "/api/v1/logs/stats", http.StatusOK},
		{"logs stats wrong method", http.MethodPost, "/api/v1/logs/stats", http.StatusMethodNotAllowed},
		{"log by id not found", http.MethodGet, "/api/v1/logs/missing", http.StatusNotFound},
		{"log by id wrong method", http.MethodPost, "/api/v1/logs/missing", http.StatusMethodNotAllowed},
		{"provider config not found", http.MethodGet, "/api/v1/config/missing", http.StatusNotFound},
		{"provider config wrong method", http.MethodDelete, "/api/v1/config/missing", http.StatusMethodNotAllowed},
		{"provider enable not found", http.MethodPost, "/api/v1/providers/missing/enable", http.StatusNotFound},
		{"provider enable wrong method", http.MethodGet, "/api/v1/providers/x/enable", http.StatusMethodNotAllowed},
		{"provider disable not found", http.MethodPost, "/api/v1/providers/missing/disable", http.StatusNotFound},
		{"provider disable wrong method", http.MethodGet, "/api/v1/providers/x/disable", http.StatusMethodNotAllowed},
		{"templates", http.MethodGet, "/api/v1/templates", http.StatusOK},
		{"template create invalid body", http.MethodPost, "/api/v1/templates/create", http.StatusBadRequest},
		{"template by id not found", http.MethodGet, "/api/v1/templates/missing", http.StatusNotFound},
		{"template by id wrong method", http.MethodPatch, "/api/v1/templates/x", http.StatusMethodNotAllowed},
		{"unknown path", http.MethodGet, "/api/v1/unknown", http.StatusNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, _ := env.do(t, tt.method, tt.path, "", nil)
			if code != tt.status {
				t.Errorf("expected status %d, got %d", tt.status, code)
			}
		})
	}
}

func TestServer_EmptyPathValues(t *testing.T) {
	env := newTestEnv(t)

	t.Run("provider config empty name", func(t *testing.T) {
		w := httptest.NewRecorder()
		env.server.handleProviderConfig(w, httptest.NewRequest(http.MethodGet, "/api/v1/config/", nil))
		if w.Code != http.StatusBadRequest {
			t.Errorf("expected status 400, got %d", w.Code)
		}
	})

	t.Run("provider enable empty name", func(t *testing.T) {
		w := httptest.NewRecorder()
		env.server.handleProviderEnable(w, httptest.NewRequest(http.MethodPost, "/api/v1/providers//enable", nil))
		if w.Code != http.StatusBadRequest {
			t.Errorf("expected status 400, got %d", w.Code)
		}
	})

	t.Run("provider disable empty name", func(t *testing.T) {
		w := httptest.NewRecorder()
		env.server.handleProviderDisable(w, httptest.NewRequest(http.MethodPost, "/api/v1/providers//disable", nil))
		if w.Code != http.StatusBadRequest {
			t.Errorf("expected status 400, got %d", w.Code)
		}
	})

	t.Run("log empty id", func(t *testing.T) {
		w := httptest.NewRecorder()
		env.server.handler.HandleLogByID(w, httptest.NewRequest(http.MethodGet, "/api/v1/logs/", nil))
		if w.Code != http.StatusBadRequest {
			t.Errorf("expected status 400, got %d", w.Code)
		}
	})
}

func TestServer_Auth(t *testing.T) {
	env := newTestEnv(t, func(c *Config) {
		c.Auth = auth.New(&auth.Config{
			Enabled:   true,
			SecretKey: "test-secret",
			AdminUser: map[string]string{"admin": "pass123"},
			APIKeys:   map[string]string{"key-1": "testing"},
		})
	})

	code, resp := env.do(t, http.MethodPost, "/api/v1/auth/login", `{"username":"admin","password":"pass123"}`, nil)
	if code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", code)
	}
	token, _ := resp["token"].(string)
	if token == "" {
		t.Fatal("expected token in login response")
	}
	bearer := map[string]string{"Authorization": "Bearer " + token}

	t.Run("login wrong method", func(t *testing.T) {
		code, _ := env.do(t, http.MethodGet, "/api/v1/auth/login", "", nil)
		if code != http.StatusMethodNotAllowed {
			t.Errorf("expected status 405, got %d", code)
		}
	})

	t.Run("login invalid body", func(t *testing.T) {
		code, _ := env.do(t, http.MethodPost, "/api/v1/auth/login", "not-json", nil)
		if code != http.StatusBadRequest {
			t.Errorf("expected status 400, got %d", code)
		}
	})

	t.Run("login bad credentials", func(t *testing.T) {
		code, _ := env.do(t, http.MethodPost, "/api/v1/auth/login", `{"username":"nobody","password":"wrong"}`, nil)
		if code != http.StatusUnauthorized {
			t.Errorf("expected status 401, got %d", code)
		}
	})

	t.Run("protected without credentials", func(t *testing.T) {
		code, _ := env.do(t, http.MethodGet, "/api/v1/providers", "", nil)
		if code != http.StatusUnauthorized {
			t.Errorf("expected status 401, got %d", code)
		}
	})

	t.Run("protected with invalid api key", func(t *testing.T) {
		code, _ := env.do(t, http.MethodGet, "/api/v1/providers", "", map[string]string{"X-API-Key": "bad"})
		if code != http.StatusUnauthorized {
			t.Errorf("expected status 401, got %d", code)
		}
	})

	t.Run("protected with api key", func(t *testing.T) {
		code, resp := env.do(t, http.MethodGet, "/api/v1/providers", "", map[string]string{"X-API-Key": "key-1"})
		if code != http.StatusOK {
			t.Fatalf("expected status 200, got %d", code)
		}
		if num(t, resp["code"]) != 0 {
			t.Errorf("expected code 0, got %v", resp["code"])
		}
	})

	t.Run("protected with bearer token", func(t *testing.T) {
		code, _ := env.do(t, http.MethodGet, "/api/v1/providers", "", bearer)
		if code != http.StatusOK {
			t.Errorf("expected status 200, got %d", code)
		}
	})

	t.Run("public status without credentials", func(t *testing.T) {
		code, _ := env.do(t, http.MethodGet, "/api/v1/status", "", nil)
		if code != http.StatusOK {
			t.Errorf("expected status 200, got %d", code)
		}
	})

	t.Run("refresh wrong method", func(t *testing.T) {
		code, _ := env.do(t, http.MethodGet, "/api/v1/auth/refresh", "", nil)
		if code != http.StatusMethodNotAllowed {
			t.Errorf("expected status 405, got %d", code)
		}
	})

	t.Run("refresh missing token", func(t *testing.T) {
		code, _ := env.do(t, http.MethodPost, "/api/v1/auth/refresh", "", nil)
		if code != http.StatusUnauthorized {
			t.Errorf("expected status 401, got %d", code)
		}
	})

	t.Run("refresh success", func(t *testing.T) {
		code, resp := env.do(t, http.MethodPost, "/api/v1/auth/refresh", "", bearer)
		if code != http.StatusOK {
			t.Fatalf("expected status 200, got %d", code)
		}
		if resp["token"] == "" || resp["token"] == nil {
			t.Error("expected new token in refresh response")
		}
	})

	t.Run("me wrong method", func(t *testing.T) {
		code, _ := env.do(t, http.MethodPost, "/api/v1/auth/me", "", nil)
		if code != http.StatusMethodNotAllowed {
			t.Errorf("expected status 405, got %d", code)
		}
	})

	t.Run("me missing token", func(t *testing.T) {
		code, _ := env.do(t, http.MethodGet, "/api/v1/auth/me", "", nil)
		if code != http.StatusUnauthorized {
			t.Errorf("expected status 401, got %d", code)
		}
	})

	t.Run("me success", func(t *testing.T) {
		code, resp := env.do(t, http.MethodGet, "/api/v1/auth/me", "", bearer)
		if code != http.StatusOK {
			t.Fatalf("expected status 200, got %d", code)
		}
		if resp["username"] != "admin" {
			t.Errorf("expected username 'admin', got %v", resp["username"])
		}
	})
}
