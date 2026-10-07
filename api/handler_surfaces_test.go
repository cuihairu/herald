package api

import (
	"net/http"
	"testing"

	"github.com/cuihairu/herald/core/audience"
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
