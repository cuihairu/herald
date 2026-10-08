package api

import (
	"net/http"
	"testing"
)

// TestAppCallbackFace walks the §13.5 configuration endpoint: scope
// gate, payload validation, the masked read and the detach.
func TestAppCallbackFace(t *testing.T) {
	e := newTestEnv(t, func(c *Config) { c.Apps = seedAppsRegistry() })
	full := map[string]string{"Authorization": "Bearer demo-app-full"}
	trigger := map[string]string{"Authorization": "Bearer demo-app-trigger"}

	// Unset face reads as configured:false.
	code, body := e.do(t, http.MethodGet, "/api/v1/apps/demo-app/callback", "", full)
	if code != 200 {
		t.Fatalf("unset read: got %d", code)
	}
	if data, _ := body["data"].(map[string]any); data["url"] != "" || data["has_secret"] != false {
		t.Fatalf("unset read: want empty url and no secret, got %v", data)
	}

	// The query-scoped token may read but not write; the config-scoped
	// token may write.
	code, _ = e.do(t, http.MethodPut, "/api/v1/apps/demo-app/callback",
		`{"url":"https://app.example.com/hook","secret":"callback-secret-32bytes!!"}`, trigger)
	if code != 403 {
		t.Fatalf("put with query token: want 403, got %d", code)
	}

	code, _ = e.do(t, http.MethodPut, "/api/v1/apps/demo-app/callback", `{bad`, full)
	if code != 400 {
		t.Fatalf("bad json: want 400, got %d", code)
	}
	for _, tc := range []string{
		`{"url":"/relative","secret":"callback-secret-32bytes!!"}`,
		`{"url":"https://","secret":"callback-secret-32bytes!!"}`,
		`{"url":"https://app.example.com/hook","secret":"short"}`,
	} {
		code, _ = e.do(t, http.MethodPut, "/api/v1/apps/demo-app/callback", tc, full)
		if code != 422 {
			t.Fatalf("validation %s: want 422, got %d", tc, code)
		}
	}

	code, _ = e.do(t, http.MethodPut, "/api/v1/apps/demo-app/callback",
		`{"url":"https://app.example.com/hook","secret":"callback-secret-32bytes!!"}`, full)
	if code != 200 {
		t.Fatalf("set: got %d", code)
	}

	// The read never echoes the secret.
	code, body = e.do(t, http.MethodGet, "/api/v1/apps/demo-app/callback", "", full)
	if code != 200 {
		t.Fatalf("read: got %d", code)
	}
	data, _ := body["data"].(map[string]any)
	if data["url"] != "https://app.example.com/hook" || data["has_secret"] != true {
		t.Fatalf("read: want the url and has_secret, got %v", data)
	}
	// Detach closes the face again.
	code, _ = e.do(t, http.MethodDelete, "/api/v1/apps/demo-app/callback", "", trigger)
	if code != 403 {
		t.Fatalf("delete with query token: want 403, got %d", code)
	}
	code, _ = e.do(t, http.MethodDelete, "/api/v1/apps/demo-app/callback", "", full)
	if code != 200 {
		t.Fatalf("delete: got %d", code)
	}
	code, body = e.do(t, http.MethodGet, "/api/v1/apps/demo-app/callback", "", full)
	data, _ = body["data"].(map[string]any)
	if code != 200 || data["url"] != "" || data["has_secret"] != false {
		t.Fatalf("after delete: want unset, got %d/%v", code, data)
	}

	// Unknown namespace authenticates as nothing at all.
	code, _ = e.do(t, http.MethodGet, "/api/v1/apps/ghost/callback", "", full)
	if code != 401 {
		t.Fatalf("unknown app: want 401, got %d", code)
	}

	// Wrong methods refuse.
	for _, m := range []string{http.MethodPost, http.MethodPatch} {
		code, _ = e.do(t, m, "/api/v1/apps/demo-app/callback", `{}`, full)
		if code != 405 {
			t.Fatalf("%s: want 405, got %d", m, code)
		}
	}
}

// TestAppCallbackUnsetRegistry: the face is closed without the app
// registry, like every other app face.
func TestAppCallbackUnsetRegistry(t *testing.T) {
	e := newTestEnv(t)
	code, _ := e.do(t, http.MethodGet, "/api/v1/apps/demo-app/callback", "",
		map[string]string{"Authorization": "Bearer x"})
	if code != 404 {
		t.Fatalf("nil registry: want 404, got %d", code)
	}
}
