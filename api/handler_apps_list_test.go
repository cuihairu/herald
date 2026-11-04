package api

import (
	"net/http"
	"testing"

	"github.com/cuihairu/herald/core/apps"
)

// TestAppList lists the configured integration namespaces for the
// dashboard apps page (operator JWT auth, not app-token auth).
func TestAppList(t *testing.T) {
	t.Run("unconfigured registry is 503", func(t *testing.T) {
		e := newTestEnv(t)
		code, _ := e.do(t, "GET", "/api/v1/apps", "", nil)
		if code != http.StatusServiceUnavailable {
			t.Fatalf("code = %d, want 503", code)
		}
	})

	r, _ := apps.NewRegistry([]apps.SeedApp{
		{Name: "zeta-app", Tokens: []apps.SeedToken{{Secret: "z", Scopes: []string{"query"}}}},
		{Name: "alpha-app", Tokens: []apps.SeedToken{{Secret: "a", Scopes: []string{"query"}}}},
	})
	e := newTestEnv(t, func(c *Config) { c.Apps = r })

	t.Run("method not allowed", func(t *testing.T) {
		code, _ := e.do(t, "POST", "/api/v1/apps", "{}", nil)
		if code != http.StatusMethodNotAllowed {
			t.Fatalf("code = %d, want 405", code)
		}
	})

	t.Run("lists names sorted", func(t *testing.T) {
		code, resp := e.do(t, "GET", "/api/v1/apps", "", nil)
		if code != http.StatusOK {
			t.Fatalf("code = %d, want 200", code)
		}
		data := dataOf(t, resp)
		names := data["apps"].([]any)
		if len(names) != 2 || names[0].(string) != "alpha-app" || names[1].(string) != "zeta-app" {
			t.Fatalf("unexpected app list: %v", names)
		}
		if data["count"] != float64(2) {
			t.Fatalf("unexpected count: %v", data["count"])
		}
	})
}
