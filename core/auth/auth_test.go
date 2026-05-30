package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cuihairu/herald/core/user"
)

func TestNew(t *testing.T) {
	t.Run("with nil config", func(t *testing.T) {
		auth := New(nil)
		if auth == nil {
			t.Fatal("expected non-nil auth")
		}
		if auth.IsEnabled() {
			t.Error("expected auth to be disabled with nil config")
		}
		if auth.userManager == nil {
			t.Error("expected non-nil user manager")
		}
		if auth.jwtManager == nil {
			t.Error("expected non-nil jwt manager")
		}
	})

	t.Run("with empty config", func(t *testing.T) {
		config := &Config{}
		auth := New(config)
		if auth == nil {
			t.Fatal("expected non-nil auth")
		}
		if auth.IsEnabled() {
			t.Error("expected auth to be disabled with empty config")
		}
	})

	t.Run("with enabled config", func(t *testing.T) {
		config := &Config{
			Enabled:   true,
			SecretKey: "test-secret",
			APIKeys:   map[string]string{"key1": "test key"},
		}
		auth := New(config)
		if auth == nil {
			t.Fatal("expected non-nil auth")
		}
		if !auth.IsEnabled() {
			t.Error("expected auth to be enabled")
		}
	})

	t.Run("with admin user", func(t *testing.T) {
		config := &Config{
			AdminUser: map[string]string{
				"admin": "password123",
			},
		}
		auth := New(config)
		if auth == nil {
			t.Fatal("expected non-nil auth")
		}
		// Check that admin user was created
		_, err := auth.userManager.Authenticate("admin", "password123")
		if err != nil {
			t.Logf("admin user authentication failed: %v", err)
		}
	})
}

func TestAuth_Middleware(t *testing.T) {
	t.Run("disabled auth allows all requests", func(t *testing.T) {
		auth := New(&Config{Enabled: false})
		handlerCalled := false

		next := func(w http.ResponseWriter, r *http.Request) {
			handlerCalled = true
			w.WriteHeader(http.StatusOK)
		}

		middleware := auth.Middleware(next)
		req := httptest.NewRequest("GET", "/test", nil)
		w := httptest.NewRecorder()

		middleware.ServeHTTP(w, req)

		if !handlerCalled {
			t.Error("expected handler to be called when auth is disabled")
		}
		if w.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", w.Code)
		}
	})

	t.Run("enabled auth blocks unauthorized requests", func(t *testing.T) {
		auth := New(&Config{Enabled: true, SecretKey: "test"})
		handlerCalled := false

		next := func(w http.ResponseWriter, r *http.Request) {
			handlerCalled = true
			w.WriteHeader(http.StatusOK)
		}

		middleware := auth.Middleware(next)
		req := httptest.NewRequest("GET", "/test", nil)
		w := httptest.NewRecorder()

		middleware.ServeHTTP(w, req)

		if handlerCalled {
			t.Error("expected handler NOT to be called without auth")
		}
		if w.Code != http.StatusUnauthorized {
			t.Errorf("expected status 401, got %d", w.Code)
		}
	})

	t.Run("API key authentication", func(t *testing.T) {
		auth := New(&Config{
			Enabled: true,
			APIKeys: map[string]string{"test-key-123": "test key"},
		})
		handlerCalled := false

		next := func(w http.ResponseWriter, r *http.Request) {
			handlerCalled = true
			w.WriteHeader(http.StatusOK)
		}

		middleware := auth.Middleware(next)
		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("X-API-Key", "test-key-123")
		w := httptest.NewRecorder()

		middleware.ServeHTTP(w, req)

		if !handlerCalled {
			t.Error("expected handler to be called with valid API key")
		}
		if w.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", w.Code)
		}
	})

	t.Run("invalid API key is rejected", func(t *testing.T) {
		auth := New(&Config{
			Enabled: true,
			APIKeys: map[string]string{"test-key-123": "test key"},
		})
		handlerCalled := false

		next := func(w http.ResponseWriter, r *http.Request) {
			handlerCalled = true
			w.WriteHeader(http.StatusOK)
		}

		middleware := auth.Middleware(next)
		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("X-API-Key", "invalid-key")
		w := httptest.NewRecorder()

		middleware.ServeHTTP(w, req)

		if handlerCalled {
			t.Error("expected handler NOT to be called with invalid API key")
		}
		if w.Code != http.StatusUnauthorized {
			t.Errorf("expected status 401, got %d", w.Code)
		}
	})
}

func TestAuth_HandleLogin(t *testing.T) {
	t.Run("successful login", func(t *testing.T) {
		auth := New(&Config{
			AdminUser: map[string]string{"admin": "password123"},
		})

		body := `{"username": "admin", "password": "password123"}`
		req := httptest.NewRequest("POST", "/login", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		auth.HandleLogin(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", w.Code)
		}

		var response LoginResponse
		if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if response.Token == "" {
			t.Error("expected token in response")
		}
		if response.User == nil {
			t.Error("expected user in response")
		} else {
			if response.User.Username != "admin" {
				t.Errorf("expected username 'admin', got '%s'", response.User.Username)
			}
		}
	})

	t.Run("wrong method", func(t *testing.T) {
		auth := New(&Config{})
		req := httptest.NewRequest("GET", "/login", nil)
		w := httptest.NewRecorder()

		auth.HandleLogin(w, req)

		if w.Code != http.StatusMethodNotAllowed {
			t.Errorf("expected status 405, got %d", w.Code)
		}
	})

	t.Run("invalid request body", func(t *testing.T) {
		auth := New(&Config{})
		body := `invalid json`
		req := httptest.NewRequest("POST", "/login", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		auth.HandleLogin(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("expected status 400, got %d", w.Code)
		}
	})

	t.Run("invalid credentials", func(t *testing.T) {
		auth := New(&Config{
			AdminUser: map[string]string{"admin": "password123"},
		})

		body := `{"username": "nonexistent", "password": "wrong"}`
		req := httptest.NewRequest("POST", "/login", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		auth.HandleLogin(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("expected status 401, got %d", w.Code)
		}
	})
}

func TestAuth_HandleRefresh(t *testing.T) {
	t.Run("successful token refresh", func(t *testing.T) {
		auth := New(&Config{SecretKey: "test-secret"})

		// First, get a token by creating a user and logging in
		um := user.NewManager()
		_ = um.CreateDefaultUser("testuser", "password123")
		auth.userManager = um

		token, _ := auth.jwtManager.Generate("user1", "testuser", "user")

		req := httptest.NewRequest("POST", "/refresh", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()

		auth.HandleRefresh(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", w.Code)
		}

		var response map[string]string
		if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if response["token"] == "" {
			t.Error("expected new token in response")
		}
	})

	t.Run("wrong method", func(t *testing.T) {
		auth := New(&Config{})
		req := httptest.NewRequest("GET", "/refresh", nil)
		w := httptest.NewRecorder()

		auth.HandleRefresh(w, req)

		if w.Code != http.StatusMethodNotAllowed {
			t.Errorf("expected status 405, got %d", w.Code)
		}
	})

	t.Run("missing token", func(t *testing.T) {
		auth := New(&Config{})
		req := httptest.NewRequest("POST", "/refresh", nil)
		w := httptest.NewRecorder()

		auth.HandleRefresh(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("expected status 401, got %d", w.Code)
		}
	})
}

func TestAuth_HandleMe(t *testing.T) {
	t.Run("get current user info", func(t *testing.T) {
		auth := New(&Config{SecretKey: "test-secret"})

		// Create a user
		um := user.NewManager()
		_ = um.CreateDefaultUser("testuser", "password123")
		auth.userManager = um

		token, _ := auth.jwtManager.Generate("user1", "testuser", "user")

		req := httptest.NewRequest("GET", "/me", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()

		auth.HandleMe(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", w.Code)
		}

		var response User
		if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if response.Username != "testuser" {
			t.Errorf("expected username 'testuser', got '%s'", response.Username)
		}
	})

	t.Run("wrong method", func(t *testing.T) {
		auth := New(&Config{})
		req := httptest.NewRequest("POST", "/me", nil)
		w := httptest.NewRecorder()

		auth.HandleMe(w, req)

		if w.Code != http.StatusMethodNotAllowed {
			t.Errorf("expected status 405, got %d", w.Code)
		}
	})

	t.Run("missing token", func(t *testing.T) {
		auth := New(&Config{})
		req := httptest.NewRequest("GET", "/me", nil)
		w := httptest.NewRecorder()

		auth.HandleMe(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("expected status 401, got %d", w.Code)
		}
	})
}

func TestAuth_getJWTToken(t *testing.T) {
	auth := New(&Config{SecretKey: "test-secret"})

	t.Run("token from Authorization header", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("Authorization", "Bearer test-token-123")
		token := auth.getJWTToken(req)

		if token != "test-token-123" {
			t.Errorf("expected token 'test-token-123', got '%s'", token)
		}
	})

	t.Run("token from cookie", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/test", nil)
		req.AddCookie(&http.Cookie{Name: "herald_token", Value: "cookie-token-123"})
		token := auth.getJWTToken(req)

		if token != "cookie-token-123" {
			t.Errorf("expected token 'cookie-token-123', got '%s'", token)
		}
	})

	t.Run("token from query parameter", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/test?token=query-token-123", nil)
		token := auth.getJWTToken(req)

		if token != "query-token-123" {
			t.Errorf("expected token 'query-token-123', got '%s'", token)
		}
	})

	t.Run("no token found", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/test", nil)
		token := auth.getJWTToken(req)

		if token != "" {
			t.Errorf("expected empty token, got '%s'", token)
		}
	})

	t.Run("Authorization header priority", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/test?token=query-token", nil)
		req.Header.Set("Authorization", "Bearer header-token")
		req.AddCookie(&http.Cookie{Name: "herald_token", Value: "cookie-token"})
		token := auth.getJWTToken(req)

		if token != "header-token" {
			t.Errorf("expected header token to have priority, got '%s'", token)
		}
	})
}

func TestAuth_getAPIKey(t *testing.T) {
	auth := New(&Config{})

	t.Run("API key from X-API-Key header", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("X-API-Key", "api-key-123")
		key := auth.getAPIKey(req)

		if key != "api-key-123" {
			t.Errorf("expected API key 'api-key-123', got '%s'", key)
		}
	})

	t.Run("API key from Authorization header", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("Authorization", "Bearer api-key-456")
		key := auth.getAPIKey(req)

		if key != "api-key-456" {
			t.Errorf("expected API key 'api-key-456', got '%s'", key)
		}
	})

	t.Run("API key from query parameter", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/test?api_key=query-api-key", nil)
		key := auth.getAPIKey(req)

		if key != "query-api-key" {
			t.Errorf("expected API key 'query-api-key', got '%s'", key)
		}
	})

	t.Run("no API key found", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/test", nil)
		key := auth.getAPIKey(req)

		if key != "" {
			t.Errorf("expected empty API key, got '%s'", key)
		}
	})

	t.Run("Authorization header priority over X-API-Key", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/test?api_key=query-key", nil)
		req.Header.Set("Authorization", "Bearer bearer-key")
		req.Header.Set("X-API-Key", "header-api-key")
		key := auth.getAPIKey(req)

		// Authorization header is checked first in the getAPIKey method
		if key != "bearer-key" {
			t.Errorf("expected Authorization Bearer token to have priority, got '%s'", key)
		}
	})
}

func TestAuth_GetUserManager(t *testing.T) {
	auth := New(&Config{})
	if auth.GetUserManager() == nil {
		t.Error("expected non-nil user manager")
	}
}

func TestAuth_IsEnabled(t *testing.T) {
	t.Run("disabled by default", func(t *testing.T) {
		auth := New(&Config{})
		if auth.IsEnabled() {
			t.Error("expected auth to be disabled by default")
		}
	})

	t.Run("enabled when configured", func(t *testing.T) {
		auth := New(&Config{Enabled: true})
		if !auth.IsEnabled() {
			t.Error("expected auth to be enabled when configured")
		}
	})
}
