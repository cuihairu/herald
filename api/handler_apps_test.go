package api

import (
	"strings"
	"testing"

	"github.com/cuihairu/herald/core/apps"
)

func seedAppsRegistry() *apps.Registry {
	r, _ := apps.NewRegistry([]apps.SeedApp{
		{Name: "ferry", Tokens: []apps.SeedToken{
			{Secret: "ferry-full", Scopes: []string{"config", "trigger", "query"}},
			{Secret: "ferry-trigger", Scopes: []string{"trigger"}},
			{Secret: "ferry-config", Scopes: []string{"config"}},
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

// TestAppCategories walks the §13.2 品类注册 face: scope enforcement on
// both methods, body validation, idempotent re-registration, conflict
// refusal and the sorted read-back.
func TestAppCategoriesEmptyList(t *testing.T) {
	e := newTestEnv(t, func(c *Config) { c.Apps = seedAppsRegistry() })
	code, body := e.do(t, "GET", "/api/v1/apps/ferry/categories", "", map[string]string{"Authorization": "Bearer ferry-full"})
	if code != 200 {
		t.Fatalf("list = %d, want 200", code)
	}
	data, _ := body["data"].(map[string]any)
	cats, _ := data["categories"].([]any)
	if len(cats) != 0 {
		t.Errorf("categories = %v, want empty before any registration", cats)
	}
}
func TestAppCategories(t *testing.T) {
	e := newTestEnv(t, func(c *Config) { c.Apps = seedAppsRegistry() })
	post := func(t *testing.T, secret, body string) (int, map[string]any) {
		t.Helper()
		return e.do(t, "POST", "/api/v1/apps/ferry/categories", body, map[string]string{"Authorization": "Bearer " + secret})
	}

	t.Run("post needs config scope", func(t *testing.T) {
		code, _ := post(t, "ferry-trigger", `{"name":"alerts","default_urgency":"urgent"}`)
		if code != 403 {
			t.Fatalf("code = %d, want 403", code)
		}
	})

	t.Run("get needs query scope", func(t *testing.T) {
		code, _ := e.do(t, "GET", "/api/v1/apps/ferry/categories", "", map[string]string{"Authorization": "Bearer ferry-config"})
		if code != 403 {
			t.Fatalf("code = %d, want 403", code)
		}
	})

	t.Run("broken json is 400", func(t *testing.T) {
		code, _ := post(t, "ferry-full", `{oops`)
		if code != 400 {
			t.Fatalf("code = %d, want 400", code)
		}
	})

	t.Run("missing fields are 422", func(t *testing.T) {
		code, _ := post(t, "ferry-full", `{"name":"alerts"}`)
		if code != 422 {
			t.Fatalf("code = %d, want 422", code)
		}
	})

	t.Run("unknown urgency is 422 naming the category", func(t *testing.T) {
		code, body := post(t, "ferry-full", `{"name":"alerts","default_urgency":"hourly"}`)
		if code != 422 {
			t.Fatalf("code = %d, want 422", code)
		}
		if msg, _ := body["message"].(string); !strings.Contains(msg, "alerts") {
			t.Errorf("message = %q, want the category named", msg)
		}
	})

	t.Run("register, re-confirm, conflict, list", func(t *testing.T) {
		code, _ := post(t, "ferry-full", `{"name":"alerts","default_urgency":"urgent"}`)
		if code != 200 {
			t.Fatalf("create = %d, want 200", code)
		}
		code, _ = post(t, "ferry-full", `{"name":"alerts","default_urgency":"urgent"}`)
		if code != 200 {
			t.Fatalf("idempotent re-register = %d, want 200", code)
		}
		code, _ = post(t, "ferry-full", `{"name":"alerts","default_urgency":"critical"}`)
		if code != 409 {
			t.Fatalf("conflict = %d, want 409", code)
		}
		post(t, "ferry-full", `{"name":"billing","default_urgency":"normal"}`)
		code, body := e.do(t, "GET", "/api/v1/apps/ferry/categories", "", map[string]string{"Authorization": "Bearer ferry-full"})
		if code != 200 {
			t.Fatalf("list = %d, want 200", code)
		}
		data, _ := body["data"].(map[string]any)
		cats, _ := data["categories"].([]any)
		if len(cats) != 2 {
			t.Fatalf("categories = %v, want two", cats)
		}
		first, _ := cats[0].(map[string]any)
		if first["name"] != "alerts" || first["default_urgency"] != "urgent" {
			t.Errorf("first = %v, want alerts/urgent (sorted)", first)
		}
	})

	t.Run("other methods are 405", func(t *testing.T) {
		code, _ := e.do(t, "DELETE", "/api/v1/apps/ferry/categories", "", map[string]string{"Authorization": "Bearer ferry-full"})
		if code != 405 {
			t.Fatalf("code = %d, want 405", code)
		}
	})
}
