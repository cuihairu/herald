package api

import (
	"testing"

	"github.com/cuihairu/herald/core/apps"
)

func seedAppsRegistry() *apps.Registry {
	r, _ := apps.NewRegistry([]apps.SeedApp{
		{Name: "ferry", Tokens: []apps.SeedToken{
			{Secret: "ferry-full", Scopes: []string{"config", "trigger", "query"}},
			{Secret: "ferry-trigger", Scopes: []string{"trigger"}},
		}},
	})
	return r
}

// TestAppShowAuthZ walks the §13.1 gate: face closed, bad credentials,
// scope enforcement and the success read.
func TestAppShowAuthZ(t *testing.T) {
	t.Run("unconfigured face is 404", func(t *testing.T) {
		e := newTestEnv(t)
		code, _ := e.do(t, "GET", "/api/v1/apps/ferry", "", map[string]string{"Authorization": "Bearer x"})
		if code != 404 {
			t.Fatalf("code = %d, want 404 (nil registry keeps the face closed)", code)
		}
	})

	e := newTestEnv(t, func(c *Config) { c.Apps = seedAppsRegistry() })

	t.Run("unknown app is one uniform 401", func(t *testing.T) {
		code, _ := e.do(t, "GET", "/api/v1/apps/ghost", "", map[string]string{"Authorization": "Bearer ferry-full"})
		if code != 401 {
			t.Fatalf("code = %d, want 401", code)
		}
	})

	t.Run("wrong secret is 401", func(t *testing.T) {
		code, _ := e.do(t, "GET", "/api/v1/apps/ferry", "", map[string]string{"Authorization": "Bearer nope"})
		if code != 401 {
			t.Fatalf("code = %d, want 401", code)
		}
	})

	t.Run("missing credential is 401", func(t *testing.T) {
		code, _ := e.do(t, "GET", "/api/v1/apps/ferry", "", nil)
		if code != 401 {
			t.Fatalf("code = %d, want 401", code)
		}
	})

	t.Run("non-bearer credential is 401", func(t *testing.T) {
		code, _ := e.do(t, "GET", "/api/v1/apps/ferry", "", map[string]string{"Authorization": "Basic abc"})
		if code != 401 {
			t.Fatalf("code = %d, want 401 (the app face takes bearer or X-API-Key only)", code)
		}
	})

	t.Run("insufficient scope is 403", func(t *testing.T) {
		code, body := e.do(t, "GET", "/api/v1/apps/ferry", "", map[string]string{"Authorization": "Bearer ferry-trigger"})
		if code != 403 {
			t.Fatalf("code = %d, want 403", code)
		}
		if msg, _ := body["message"].(string); msg == "" {
			t.Errorf("body = %v, want a scope refusal message", body)
		}
	})

	t.Run("query scope reads back the namespace", func(t *testing.T) {
		code, body := e.do(t, "GET", "/api/v1/apps/ferry", "", map[string]string{"Authorization": "Bearer ferry-full"})
		if code != 200 {
			t.Fatalf("code = %d, want 200", code)
		}
		data, _ := body["data"].(map[string]any)
		if data == nil || data["name"] != "ferry" {
			t.Fatalf("data = %v, want the ferry namespace", data)
		}
		scopes, _ := data["scopes"].([]any)
		if len(scopes) != 3 || scopes[0] != "config" || scopes[1] != "trigger" || scopes[2] != "query" {
			t.Errorf("scopes = %v, want [config trigger query] in contract order", scopes)
		}
	})

	t.Run("X-API-Key carries the credential too", func(t *testing.T) {
		code, _ := e.do(t, "GET", "/api/v1/apps/ferry", "", map[string]string{"X-API-Key": "ferry-full"})
		if code != 200 {
			t.Fatalf("code = %d, want 200", code)
		}
	})
}
