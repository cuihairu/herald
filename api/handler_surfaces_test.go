package api

import (
	"errors"
	"net/http"
	"testing"

	"github.com/cuihairu/herald/core/audience"
	"github.com/cuihairu/herald/core/auth"
)

// surfacesEnv wires the §8 source adapter over live registries — the
// surfaces face is its operator-side machine entry.
func surfacesEnv(t *testing.T) (*testEnv, *audience.Registry, *audience.SurfaceRegistry) {
	t.Helper()
	relations := audience.NewRegistry()
	surfaces := audience.NewSurfaceRegistry()
	e := newTestEnv(t, func(c *Config) {
		c.Sources = audience.NewSourceAdapter(surfaces, relations)
		c.SourceSurfaces = surfaces
		c.FeedRelations = relations
		c.FeedSurfaces = surfaces
	})
	return e, relations, surfaces
}

// TestAudienceSurfacesBind walks the 联系面绑定 face: bind lays the
// surface plus the default subscriptions down, a conflicting rebind
// refuses, and the read-back answers through the relations face.
func TestAudienceSurfacesBind(t *testing.T) {
	e, relations, surfaces := surfacesEnv(t)
	h := map[string]string{"Authorization": "Bearer operator-token"}

	code, resp := e.do(t, "POST", "/api/v1/audiences/user.1/surfaces",
		`{"channel":"email","target":"u1@example.com","categories":["notice"]}`, h)
	if code != http.StatusOK {
		t.Fatalf("bind: code = %d, resp = %v", code, resp)
	}
	data := resp["data"].(map[string]any)
	if data["bind_changed"] != true || data["subscribed"] != float64(1) {
		t.Fatalf("bind result: %v", data)
	}
	surface := data["surface"].(map[string]any)
	if surface["status"] != string(audience.SurfaceActive) || surface["target"] != "u1@example.com" {
		t.Fatalf("surface: %v", surface)
	}
	if _, ok := surfaces.Surface("user.1", "email"); !ok {
		t.Fatalf("surface not in registry")
	}
	if len(relations.Relations("user.1")) != 1 {
		t.Fatalf("default subscription missing: %v", relations.Relations("user.1"))
	}

	// A live slot holding a different target refuses — the §3.1 rebind
	// guard applies to this entry like every other.
	code, _ = e.do(t, "POST", "/api/v1/audiences/user.1/surfaces",
		`{"channel":"email","target":"other@example.com"}`, h)
	if code != http.StatusUnprocessableEntity {
		t.Fatalf("conflicting rebind: code = %d, want 422", code)
	}
}

// TestAudienceSurfacesUnbind: the DELETE arm invalidates the surface and
// terminates the channel's subscriptions.
func TestAudienceSurfacesUnbind(t *testing.T) {
	e, relations, surfaces := surfacesEnv(t)
	h := map[string]string{"Authorization": "Bearer operator-token"}
	if _, err := surfaces.Activate("user.2", "email", "u2@example.com", "test"); err != nil {
		t.Fatalf("activate: %v", err)
	}
	if err := relations.Subscribe(audience.Relation{
		AudienceID: "user.2", Category: "notice", Channel: "email",
		Type: audience.RelationSubscription, Source: audience.SourceAdmin,
	}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	code, resp := e.do(t, "DELETE", "/api/v1/audiences/user.2/surfaces?channel=email", "", h)
	if code != http.StatusOK {
		t.Fatalf("unbind: code = %d, resp = %v", code, resp)
	}
	data := resp["data"].(map[string]any)
	if data["surface_invalidated"] != true || data["terminated"] != float64(1) {
		t.Fatalf("unbind result: %v", data)
	}
	if s, _ := surfaces.Surface("user.2", "email"); s.Status == audience.SurfaceActive {
		t.Fatalf("surface still active: %+v", s)
	}
}

// TestAudienceSurfacesAuthZ: malformed ids and the unconfigured face.
func TestAudienceSurfacesAuthZ(t *testing.T) {
	e := newTestEnv(t)
	h := map[string]string{"Authorization": "Bearer operator-token"}
	// nil adapter keeps the face closed.
	if code, _ := e.do(t, "POST", "/api/v1/audiences/user.1/surfaces",
		`{"channel":"email","target":"x"}`, h); code != http.StatusNotFound {
		t.Fatalf("unconfigured: code = %d, want 404", code)
	}

	e, _, _ = surfacesEnv(t)
	if code, _ := e.do(t, "POST", "/api/v1/audiences/user.1/surfaces",
		`{"channel":"email","target":"x"`, h); code != http.StatusBadRequest {
		t.Fatalf("broken JSON: code = %d, want 400", code)
	}
	if code, _ := e.do(t, "POST", "/api/v1/audiences/bad:id/surfaces",
		`{"channel":"email","target":"x"}`, h); code != http.StatusUnprocessableEntity {
		t.Fatalf("bad id: code = %d, want 422", code)
	}
	if code, _ := e.do(t, "POST", "/api/v1/audiences/user.1/surfaces",
		`{"channel":"","target":"x"}`, h); code != http.StatusUnprocessableEntity {
		t.Fatalf("bad channel: code = %d, want 422", code)
	}
	if code, _ := e.do(t, "POST", "/api/v1/audiences/user.1/surfaces",
		`{"channel":"email","target":" "}`, h); code != http.StatusUnprocessableEntity {
		t.Fatalf("blank target: code = %d, want 422", code)
	}
	if code, _ := e.do(t, "PUT", "/api/v1/audiences/user.1/surfaces",
		`{}`, h); code != http.StatusMethodNotAllowed {
		t.Fatalf("method: code = %d, want 405", code)
	}
	// DELETE arm: the same id/channel guards, bad input before the call.
	if code, _ := e.do(t, "DELETE", "/api/v1/audiences/bad:id/surfaces?channel=email", "", h); code != http.StatusUnprocessableEntity {
		t.Fatalf("delete bad id: code = %d, want 422", code)
	}
	if code, _ := e.do(t, "DELETE", "/api/v1/audiences/user.1/surfaces", "", h); code != http.StatusUnprocessableEntity {
		t.Fatalf("delete missing channel: code = %d, want 422", code)
	}
}

// TestAudienceSurfacesRead: the GET arm answers the audience's bound
// surfaces plus its private RSS feed token — minted on first read and
// stable for the audience's lifetime (§9). The unknown-audience 404 runs
// before the mint, so a read cannot create registry entries for
// arbitrary ids.
func TestAudienceSurfacesRead(t *testing.T) {
	e, _, surfaces := surfacesEnv(t)
	h := map[string]string{"Authorization": "Bearer operator-token"}

	// Unknown audience: 404, and the mint stays untouched — the token the
	// read would have created does not exist (the later bind's token is
	// issued fresh, not replayed from a ghost mint).
	if code, _ := e.do(t, "GET", "/api/v1/audiences/ghost/surfaces", "", h); code != http.StatusNotFound {
		t.Fatalf("unknown audience: code = %d, want 404", code)
	}

	if code, _ := e.do(t, "POST", "/api/v1/audiences/user.1/surfaces",
		`{"channel":"email","target":"u1@example.com"}`, h); code != http.StatusOK {
		t.Fatalf("bind: code = %d", code)
	}

	code, resp := e.do(t, "GET", "/api/v1/audiences/user.1/surfaces", "", h)
	if code != http.StatusOK {
		t.Fatalf("read: code = %d, resp = %v", code, resp)
	}
	data := dataOf(t, resp)
	rows, ok := data["surfaces"].([]any)
	if !ok || len(rows) != 1 {
		t.Fatalf("surfaces: %v", data["surfaces"])
	}
	row := rows[0].(map[string]any)
	if row["audience_id"] != "user.1" || row["channel"] != "email" ||
		row["target"] != "u1@example.com" || row["status"] != string(audience.SurfaceActive) {
		t.Fatalf("surface row: %v", row)
	}
	token, _ := data["rss_token"].(string)
	if token == "" {
		t.Fatalf("rss_token missing: %v", data)
	}
	if got, ok := surfaces.RSSAudience(token); !ok || got != "user.1" {
		t.Fatalf("token does not resolve back: %q -> %q, %v", token, got, ok)
	}

	// §3.1 受众级稳定: a second read answers the same token.
	_, resp2 := e.do(t, "GET", "/api/v1/audiences/user.1/surfaces", "", h)
	if dataOf(t, resp2)["rss_token"] != token {
		t.Fatalf("token not stable: %v vs %v", dataOf(t, resp2)["rss_token"], token)
	}

	// Invalid id format: 422 like the write arms.
	if code, _ := e.do(t, "GET", "/api/v1/audiences/bad:id/surfaces", "", h); code != http.StatusUnprocessableEntity {
		t.Fatalf("bad id: code = %d, want 422", code)
	}
}

// TestAudienceSurfacesReadTokenFailure: an entropy failure inside
// RSSToken surfaces as a 500 instead of a half-written registry entry —
// injected through the exported audience.RandRead seam.
func TestAudienceSurfacesReadTokenFailure(t *testing.T) {
	e, _, _ := surfacesEnv(t)
	h := map[string]string{"Authorization": "Bearer operator-token"}
	if code, _ := e.do(t, "POST", "/api/v1/audiences/user.1/surfaces",
		`{"channel":"email","target":"u1@example.com"}`, h); code != http.StatusOK {
		t.Fatalf("bind: code = %d", code)
	}

	orig := audience.RandRead
	audience.RandRead = func([]byte) (int, error) { return 0, errors.New("no entropy") }
	defer func() { audience.RandRead = orig }()

	code, resp := e.do(t, "GET", "/api/v1/audiences/user.1/surfaces", "", h)
	if code != http.StatusInternalServerError {
		t.Fatalf("entropy failure: code = %d, want 500", code)
	}
	if msg, _ := resp["message"].(string); msg != "rss token generation failed" {
		t.Fatalf("message = %v, want rss token generation failed", resp["message"])
	}

	// The failed mint left nothing behind: with the entropy source
	// restored, the same read answers normally with a fresh token.
	audience.RandRead = orig
	code, resp = e.do(t, "GET", "/api/v1/audiences/user.1/surfaces", "", h)
	if code != http.StatusOK {
		t.Fatalf("read after restore: code = %d, resp = %v", code, resp)
	}
	if token, _ := dataOf(t, resp)["rss_token"].(string); token == "" {
		t.Fatalf("rss_token missing after restore: %v", dataOf(t, resp))
	}
}

// TestAudienceSurfacesReadFace: the read arm's face guard and its
// placement behind the operator-token middleware.
func TestAudienceSurfacesReadFace(t *testing.T) {
	// No registry wired: the face is closed, GET answers 404 like the
	// write arms do.
	e := newTestEnv(t)
	h := map[string]string{"Authorization": "Bearer operator-token"}
	if code, _ := e.do(t, "GET", "/api/v1/audiences/user.1/surfaces", "", h); code != http.StatusNotFound {
		t.Fatalf("unconfigured: code = %d, want 404", code)
	}

	// Auth enabled: no credentials is 401 — the read face sits behind the
	// same middleware as every operator face; a valid API key passes and
	// reaches the handler (unknown audience 404).
	relations := audience.NewRegistry()
	surfaces := audience.NewSurfaceRegistry()
	e2 := newTestEnv(t, func(c *Config) {
		c.Auth = auth.New(&auth.Config{
			Enabled:   true,
			SecretKey: "test-secret",
			AdminUser: map[string]string{"admin": "pass123"},
			APIKeys:   map[string]string{"key-1": "testing"},
		})
		c.Sources = audience.NewSourceAdapter(surfaces, relations)
		c.SourceSurfaces = surfaces
	})
	if code, _ := e2.do(t, "GET", "/api/v1/audiences/user.1/surfaces", "", nil); code != http.StatusUnauthorized {
		t.Fatalf("no auth: code = %d, want 401", code)
	}
	if code, _ := e2.do(t, "GET", "/api/v1/audiences/user.1/surfaces", "", map[string]string{"X-API-Key": "key-1"}); code != http.StatusNotFound {
		t.Fatalf("api key: code = %d, want 404 (unknown audience)", code)
	}
}
