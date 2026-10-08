package api

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/cuihairu/herald/core/apps"
	"github.com/cuihairu/herald/core/audience"
	"github.com/cuihairu/herald/core/audit"
)

func seedAppsRegistry() *apps.Registry {
	r, _ := apps.NewRegistry([]apps.SeedApp{
		{Name: "demo-app", Tokens: []apps.SeedToken{
			{Secret: "demo-app-full", Scopes: []string{"config", "trigger", "query"}},
			{Secret: "demo-app-trigger", Scopes: []string{"trigger"}},
			{Secret: "demo-app-config", Scopes: []string{"config"}},
		}},
	})
	return r
}

// TestAppShowAuthZ walks the §13.1 gate: face closed, bad credentials,
// scope enforcement and the success read.
func TestAppShowAuthZ(t *testing.T) {
	t.Run("unconfigured face is 404", func(t *testing.T) {
		e := newTestEnv(t)
		code, _ := e.do(t, "GET", "/api/v1/apps/demo-app", "", map[string]string{"Authorization": "Bearer x"})
		if code != 404 {
			t.Fatalf("code = %d, want 404 (nil registry keeps the face closed)", code)
		}
	})

	e := newTestEnv(t, func(c *Config) { c.Apps = seedAppsRegistry() })

	t.Run("unknown app is one uniform 401", func(t *testing.T) {
		code, _ := e.do(t, "GET", "/api/v1/apps/ghost", "", map[string]string{"Authorization": "Bearer demo-app-full"})
		if code != 401 {
			t.Fatalf("code = %d, want 401", code)
		}
	})

	t.Run("wrong secret is 401", func(t *testing.T) {
		code, _ := e.do(t, "GET", "/api/v1/apps/demo-app", "", map[string]string{"Authorization": "Bearer nope"})
		if code != 401 {
			t.Fatalf("code = %d, want 401", code)
		}
	})

	t.Run("missing credential is 401", func(t *testing.T) {
		code, _ := e.do(t, "GET", "/api/v1/apps/demo-app", "", nil)
		if code != 401 {
			t.Fatalf("code = %d, want 401", code)
		}
	})

	t.Run("non-bearer credential is 401", func(t *testing.T) {
		code, _ := e.do(t, "GET", "/api/v1/apps/demo-app", "", map[string]string{"Authorization": "Basic abc"})
		if code != 401 {
			t.Fatalf("code = %d, want 401 (the app face takes bearer or X-API-Key only)", code)
		}
	})

	t.Run("insufficient scope is 403", func(t *testing.T) {
		code, body := e.do(t, "GET", "/api/v1/apps/demo-app", "", map[string]string{"Authorization": "Bearer demo-app-trigger"})
		if code != 403 {
			t.Fatalf("code = %d, want 403", code)
		}
		if msg, _ := body["message"].(string); msg == "" {
			t.Errorf("body = %v, want a scope refusal message", body)
		}
	})

	t.Run("query scope reads back the namespace", func(t *testing.T) {
		code, body := e.do(t, "GET", "/api/v1/apps/demo-app", "", map[string]string{"Authorization": "Bearer demo-app-full"})
		if code != 200 {
			t.Fatalf("code = %d, want 200", code)
		}
		data, _ := body["data"].(map[string]any)
		if data == nil || data["name"] != "demo-app" {
			t.Fatalf("data = %v, want the demo-app namespace", data)
		}
		scopes, _ := data["scopes"].([]any)
		if len(scopes) != 3 || scopes[0] != "config" || scopes[1] != "trigger" || scopes[2] != "query" {
			t.Errorf("scopes = %v, want [config trigger query] in contract order", scopes)
		}
	})

	t.Run("X-API-Key carries the credential too", func(t *testing.T) {
		code, _ := e.do(t, "GET", "/api/v1/apps/demo-app", "", map[string]string{"X-API-Key": "demo-app-full"})
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
	code, body := e.do(t, "GET", "/api/v1/apps/demo-app/categories", "", map[string]string{"Authorization": "Bearer demo-app-full"})
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
		return e.do(t, "POST", "/api/v1/apps/demo-app/categories", body, map[string]string{"Authorization": "Bearer " + secret})
	}

	t.Run("post needs config scope", func(t *testing.T) {
		code, _ := post(t, "demo-app-trigger", `{"name":"alerts","default_urgency":"urgent"}`)
		if code != 403 {
			t.Fatalf("code = %d, want 403", code)
		}
	})

	t.Run("get needs query scope", func(t *testing.T) {
		code, _ := e.do(t, "GET", "/api/v1/apps/demo-app/categories", "", map[string]string{"Authorization": "Bearer demo-app-config"})
		if code != 403 {
			t.Fatalf("code = %d, want 403", code)
		}
	})

	t.Run("broken json is 400", func(t *testing.T) {
		code, _ := post(t, "demo-app-full", `{oops`)
		if code != 400 {
			t.Fatalf("code = %d, want 400", code)
		}
	})

	t.Run("missing fields are 422", func(t *testing.T) {
		code, _ := post(t, "demo-app-full", `{"name":"alerts"}`)
		if code != 422 {
			t.Fatalf("code = %d, want 422", code)
		}
	})

	t.Run("unknown urgency is 422 naming the category", func(t *testing.T) {
		code, body := post(t, "demo-app-full", `{"name":"alerts","default_urgency":"hourly"}`)
		if code != 422 {
			t.Fatalf("code = %d, want 422", code)
		}
		if msg, _ := body["message"].(string); !strings.Contains(msg, "alerts") {
			t.Errorf("message = %q, want the category named", msg)
		}
	})

	t.Run("register, re-confirm, conflict, list", func(t *testing.T) {
		code, _ := post(t, "demo-app-full", `{"name":"alerts","default_urgency":"urgent"}`)
		if code != 200 {
			t.Fatalf("create = %d, want 200", code)
		}
		code, _ = post(t, "demo-app-full", `{"name":"alerts","default_urgency":"urgent"}`)
		if code != 200 {
			t.Fatalf("idempotent re-register = %d, want 200", code)
		}
		code, _ = post(t, "demo-app-full", `{"name":"alerts","default_urgency":"critical"}`)
		if code != 409 {
			t.Fatalf("conflict = %d, want 409", code)
		}
		post(t, "demo-app-full", `{"name":"billing","default_urgency":"normal"}`)
		code, body := e.do(t, "GET", "/api/v1/apps/demo-app/categories", "", map[string]string{"Authorization": "Bearer demo-app-full"})
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
		code, _ := e.do(t, "DELETE", "/api/v1/apps/demo-app/categories", "", map[string]string{"Authorization": "Bearer demo-app-full"})
		if code != 405 {
			t.Fatalf("code = %d, want 405", code)
		}
	})
}

// TestAppPolicies walks the §13.2 policy override face: per-family PUT
// with refuse-not-clamp validation, PUT-is-replace semantics, family
// isolation and the aggregate read-back.
func TestAppPolicies(t *testing.T) {
	e := newTestEnv(t, func(c *Config) { c.Apps = seedAppsRegistry() })
	put := func(t *testing.T, path, body string) (int, map[string]any) {
		t.Helper()
		return e.do(t, "PUT", "/api/v1/apps/demo-app"+path, body, map[string]string{"Authorization": "Bearer demo-app-full"})
	}

	t.Run("write needs config scope", func(t *testing.T) {
		code, _ := e.do(t, "PUT", "/api/v1/apps/demo-app/policies/intensity", `{"sms":"L4"}`, map[string]string{"Authorization": "Bearer demo-app-trigger"})
		if code != 403 {
			t.Fatalf("code = %d, want 403", code)
		}
	})

	t.Run("read needs query scope", func(t *testing.T) {
		code, _ := e.do(t, "GET", "/api/v1/apps/demo-app/policies", "", map[string]string{"Authorization": "Bearer demo-app-config"})
		if code != 403 {
			t.Fatalf("code = %d, want 403", code)
		}
	})

	t.Run("broken json is 400", func(t *testing.T) {
		code, _ := put(t, "/policies/intensity", `{oops`)
		if code != 400 {
			t.Fatalf("code = %d, want 400", code)
		}
	})

	t.Run("intensity bad value refuses naming the channel", func(t *testing.T) {
		code, body := put(t, "/policies/intensity", `{"sms":"L9"}`)
		if code != 422 {
			t.Fatalf("code = %d, want 422", code)
		}
		if msg, _ := body["message"].(string); !strings.Contains(msg, "sms") {
			t.Errorf("message = %q, want the channel named", msg)
		}
	})

	t.Run("mode bad value refuses naming the category", func(t *testing.T) {
		code, body := put(t, "/policies/delivery-mode", `{"alerts":"cascade"}`)
		if code != 422 {
			t.Fatalf("code = %d, want 422", code)
		}
		if msg, _ := body["message"].(string); !strings.Contains(msg, "alerts") {
			t.Errorf("message = %q, want the category named", msg)
		}
	})

	t.Run("escalation validates the duration", func(t *testing.T) {
		code, _ := put(t, "/policies/escalation", `{}`)
		if code != 422 {
			t.Fatalf("missing ack_timeout = %d, want 422", code)
		}
		code, _ = put(t, "/policies/escalation", `{"ack_timeout":"later"}`)
		if code != 422 {
			t.Fatalf("bad ack_timeout = %d, want 422", code)
		}
		code, _ = put(t, "/policies/escalation", `{"ack_timeout":"-5m"}`)
		if code != 422 {
			t.Fatalf("negative ack_timeout = %d, want 422", code)
		}
		code, _ = put(t, "/policies/escalation", `{"ack_timeout":"3m"}`)
		if code != 200 {
			t.Fatalf("ack_timeout 3m = %d, want 200", code)
		}
	})

	t.Run("dedup validates tiers and windows", func(t *testing.T) {
		code, body := put(t, "/policies/dedup", `{"tiers":{"alerts":"hourly"}}`)
		if code != 422 {
			t.Fatalf("bad tier = %d, want 422", code)
		}
		if msg, _ := body["message"].(string); !strings.Contains(msg, "alerts") {
			t.Errorf("message = %q, want the category named", msg)
		}
		code, _ = put(t, "/policies/dedup", `{"windows":{"alerts":"0s"}}`)
		if code != 422 {
			t.Fatalf("non-positive window = %d, want 422", code)
		}
		code, _ = put(t, "/policies/dedup", `{"tiers":{"alerts":"always","bills":"once"},"windows":{"alerts":"30m"}}`)
		if code != 200 {
			t.Fatalf("dedup put = %d, want 200", code)
		}
	})

	t.Run("mode put succeeds and non-string value refuses", func(t *testing.T) {
		code, _ := put(t, "/policies/delivery-mode", `{"alerts":3}`)
		if code != 422 {
			t.Fatalf("non-string mode = %d, want 422", code)
		}
		code, _ = put(t, "/policies/delivery-mode", `{"alerts":"escalation","marketing":"fixed"}`)
		if code != 200 {
			t.Fatalf("mode put = %d, want 200", code)
		}
	})

	t.Run("families accumulate and read back", func(t *testing.T) {
		code, _ := put(t, "/policies/intensity", `{"telegram":"L0"}`)
		if code != 200 {
			t.Fatalf("intensity put = %d, want 200", code)
		}
		code, body := e.do(t, "GET", "/api/v1/apps/demo-app/policies", "", map[string]string{"Authorization": "Bearer demo-app-full"})
		if code != 200 {
			t.Fatalf("read = %d, want 200", code)
		}
		data, _ := body["data"].(map[string]any)
		if data["ack_timeout"] != "3m0s" {
			t.Errorf("ack_timeout = %v, want 3m0s from the escalation family", data["ack_timeout"])
		}
		intensity, _ := data["channel_intensity"].(map[string]any)
		if intensity["telegram"] != "L0" {
			t.Errorf("intensity = %v, want the L0 override", intensity)
		}
		tiers, _ := data["dedup_tiers"].(map[string]any)
		if tiers["alerts"] != "always" || tiers["bills"] != "once" {
			t.Errorf("tiers = %v, want alerts=always bills=once", tiers)
		}
		modes, _ := data["mode_by_category"].(map[string]any)
		if modes["alerts"] != "escalation" || modes["marketing"] != "fixed" {
			t.Errorf("modes = %v, want alerts=escalation marketing=fixed", modes)
		}
	})

	t.Run("family replace clears absent keys", func(t *testing.T) {
		code, _ := put(t, "/policies/dedup", `{"windows":{"alerts":"10m"}}`)
		if code != 200 {
			t.Fatalf("dedup put = %d, want 200", code)
		}
		_, body := e.do(t, "GET", "/api/v1/apps/demo-app/policies", "", map[string]string{"Authorization": "Bearer demo-app-full"})
		data, _ := body["data"].(map[string]any)
		if _, has := data["dedup_tiers"]; has {
			t.Errorf("dedup_tiers = %v, want cleared by the family PUT", data["dedup_tiers"])
		}
		windows, _ := data["dedup_windows"].(map[string]any)
		if windows["alerts"] != "10m0s" {
			t.Errorf("windows = %v, want alerts 10m0s", windows)
		}
	})

	t.Run("empty set reads back empty", func(t *testing.T) {
		e2 := newTestEnv(t, func(c *Config) { c.Apps = seedAppsRegistry() })
		code, body := e2.do(t, "GET", "/api/v1/apps/demo-app/policies", "", map[string]string{"Authorization": "Bearer demo-app-full"})
		if code != 200 {
			t.Fatalf("read = %d, want 200", code)
		}
		data, _ := body["data"].(map[string]any)
		if len(data) != 0 {
			t.Errorf("data = %v, want empty object", data)
		}
	})
}

// validTemplateJSON is the §13.2 body shape: id/title/name plus typed
// fields; bindings ride along per channel when present.
const validTemplateJSON = `{
	"id": "node_down",
	"name": "节点下线",
	"title": "节点 {{host}} 下线",
	"level": "error",
	"fields": [{"label": "主机", "value": "{{host}}", "type": "string"}]
}`

func TestAppTemplates(t *testing.T) {
	e := newTestEnv(t, func(c *Config) { c.Apps = seedAppsRegistry() })
	bearer := map[string]string{"Authorization": "Bearer demo-app-full"}

	// Empty list first — a fresh namespace answers [], not null.
	code, body := e.do(t, "GET", "/api/v1/apps/demo-app/templates", "", bearer)
	if code != 200 {
		t.Fatalf("empty list: got %d", code)
	}
	if got, _ := body["data"].([]any); got == nil || len(got) != 0 {
		t.Fatalf("empty list: want [], got %v", body["data"])
	}

	// GET is query power: trigger-only and config-only tokens lack it.
	for _, secret := range []string{"demo-app-trigger", "demo-app-config"} {
		code, _ := e.do(t, "GET", "/api/v1/apps/demo-app/templates", "", map[string]string{"Authorization": "Bearer " + secret})
		if code != 403 {
			t.Fatalf("GET with %s: want 403, got %d", secret, code)
		}
	}
	// POST is config power: the trigger-only token lacks it.
	code, _ = e.do(t, "POST", "/api/v1/apps/demo-app/templates", validTemplateJSON, map[string]string{"Authorization": "Bearer demo-app-trigger"})
	if code != 403 {
		t.Fatalf("POST with trigger token: want 403, got %d", code)
	}

	// Bad JSON and an invalid template refuse.
	code, _ = e.do(t, "POST", "/api/v1/apps/demo-app/templates", "{not json", bearer)
	if code != 400 {
		t.Fatalf("bad JSON: want 400, got %d", code)
	}
	code, _ = e.do(t, "POST", "/api/v1/apps/demo-app/templates", `{"id":"x","name":"y"}`, bearer)
	if code != 422 {
		t.Fatalf("template missing title: want 422, got %d", code)
	}

	// Register, re-confirm (idempotent upsert), list, read one.
	code, _ = e.do(t, "POST", "/api/v1/apps/demo-app/templates", validTemplateJSON, bearer)
	if code != 200 {
		t.Fatalf("register: got %d", code)
	}
	code, _ = e.do(t, "POST", "/api/v1/apps/demo-app/templates", validTemplateJSON, bearer)
	if code != 200 {
		t.Fatalf("re-confirm: got %d", code)
	}
	code, body = e.do(t, "GET", "/api/v1/apps/demo-app/templates", "", bearer)
	if code != 200 {
		t.Fatalf("list: got %d", code)
	}
	if got, _ := body["data"].([]any); len(got) != 1 {
		t.Fatalf("list: want 1 template, got %v", body["data"])
	}
	code, body = e.do(t, "GET", "/api/v1/apps/demo-app/templates/node_down", "", bearer)
	if code != 200 {
		t.Fatalf("get: got %d", code)
	}
	if data, _ := body["data"].(map[string]any); data["title"] != "节点 {{host}} 下线" {
		t.Fatalf("get: want round-tripped title, got %v", body["data"])
	}
	code, _ = e.do(t, "GET", "/api/v1/apps/demo-app/templates/ghost", "", bearer)
	if code != 404 {
		t.Fatalf("get ghost: want 404, got %d", code)
	}

	// DELETE is config power.
	code, _ = e.do(t, "DELETE", "/api/v1/apps/demo-app/templates/node_down", "", map[string]string{"Authorization": "Bearer demo-app-trigger"})
	if code != 403 {
		t.Fatalf("DELETE with trigger token: want 403, got %d", code)
	}
	code, _ = e.do(t, "DELETE", "/api/v1/apps/demo-app/templates/node_down", "", bearer)
	if code != 200 {
		t.Fatalf("delete: got %d", code)
	}
	code, _ = e.do(t, "DELETE", "/api/v1/apps/demo-app/templates/node_down", "", bearer)
	if code != 404 {
		t.Fatalf("double delete: want 404, got %d", code)
	}
	code, _ = e.do(t, "GET", "/api/v1/apps/demo-app/templates/node_down", "", bearer)
	if code != 404 {
		t.Fatalf("get after delete: want 404, got %d", code)
	}

	// Method not allowed on both endpoints.
	for _, path := range []string{"/api/v1/apps/demo-app/templates", "/api/v1/apps/demo-app/templates/node_down"} {
		code, _ = e.do(t, "PUT", path, "{}", bearer)
		if code != 405 {
			t.Fatalf("PUT %s: want 405, got %d", path, code)
		}
	}
}

// TestAppTemplatesNilRegistry keeps the whole face closed when no
// namespaces are configured.
func TestAppTemplatesNilRegistry(t *testing.T) {
	e := newTestEnv(t)
	for _, tc := range []struct{ method, path string }{
		{"GET", "/api/v1/apps/demo-app/templates"},
		{"POST", "/api/v1/apps/demo-app/templates"},
		{"GET", "/api/v1/apps/demo-app/templates/x"},
		{"DELETE", "/api/v1/apps/demo-app/templates/x"},
	} {
		code, _ := e.do(t, tc.method, tc.path, "{}", map[string]string{"Authorization": "Bearer demo-app-full"})
		if code != 404 {
			t.Fatalf("%s %s without registry: want 404, got %d", tc.method, tc.path, code)
		}
	}
}

// dispatchEnv wires the full §13.3 face: apps registry, the §6 policy,
// the 关系×联系面 pair (alice holds an active email surface plus her
// subscription), and one enabled email provider.
func dispatchEnv(t *testing.T) *testEnv {
	t.Helper()
	relations := audience.NewRegistry()
	surfaces := audience.NewSurfaceRegistry()
	if _, err := surfaces.Activate("alice", "email", "alice@example.com", "test"); err != nil {
		t.Fatalf("activate alice: %v", err)
	}
	if err := relations.Subscribe(audience.Relation{
		AudienceID: "alice", Category: "alerts", Channel: "email",
		Type: audience.RelationSubscription, Source: "test",
	}); err != nil {
		t.Fatalf("subscribe alice: %v", err)
	}
	policy, err := audience.NewDeliveryPolicy(map[string]string{"alerts": "urgent"}, nil, nil, false)
	if err != nil {
		t.Fatalf("policy: %v", err)
	}
	e := newTestEnv(t, func(c *Config) {
		c.Apps = seedAppsRegistry()
		c.Delivery = policy
		c.Filter = audience.NewFilter(relations, surfaces)
		c.FeedRelations = relations
		c.FeedSurfaces = surfaces
	})
	if err := e.runtime.RegisterProvider("email", &stubProvider{name: "email", pType: "email"}, true); err != nil {
		t.Fatalf("register provider: %v", err)
	}
	return e
}

func TestAppDispatch(t *testing.T) {
	e := dispatchEnv(t)
	trigger := map[string]string{"Authorization": "Bearer demo-app-trigger"}

	// AuthZ: dispatch is trigger power; config-only and query-less
	// tokens stay out. (demo-app-trigger holds exactly the right scope.)
	code, _ := e.do(t, "POST", "/api/v1/apps/demo-app/dispatch", `{"category":"alerts","audiences":["alice"]}`, map[string]string{"Authorization": "Bearer demo-app-config"})
	if code != 403 {
		t.Fatalf("config token: want 403, got %d", code)
	}

	// Body validation.
	code, _ = e.do(t, "POST", "/api/v1/apps/demo-app/dispatch", "{bad", trigger)
	if code != 400 {
		t.Fatalf("bad JSON: want 400, got %d", code)
	}
	code, _ = e.do(t, "POST", "/api/v1/apps/demo-app/dispatch", `{"audiences":["alice"]}`, trigger)
	if code != 422 {
		t.Fatalf("missing category: want 422, got %d", code)
	}
	code, _ = e.do(t, "POST", "/api/v1/apps/demo-app/dispatch", `{"category":"alerts"}`, trigger)
	if code != 422 {
		t.Fatalf("missing audiences: want 422, got %d", code)
	}
	// Register the category before the vocabulary checks: urgency and
	// relation validation run against a registered category, in request
	// order (taxonomy gate first).
	reg := map[string]string{"Authorization": "Bearer demo-app-full"}
	code, _ = e.do(t, "POST", "/api/v1/apps/demo-app/categories", `{"name":"alerts","default_urgency":"urgent"}`, reg)
	if code != 200 {
		t.Fatalf("category register: got %d", code)
	}
	code, _ = e.do(t, "POST", "/api/v1/apps/demo-app/dispatch", `{"category":"alerts","audiences":["alice"],"urgency":"whenever"}`, trigger)
	if code != 422 {
		t.Fatalf("bad urgency: want 422, got %d", code)
	}
	code, _ = e.do(t, "POST", "/api/v1/apps/demo-app/dispatch", `{"category":"alerts","audiences":["alice"],"relation_type":"possession"}`, trigger)
	if code != 422 {
		t.Fatalf("bad relation_type: want 422, got %d", code)
	}
	// The namespace taxonomy is the point: an unregistered category is
	// refused even if the operator's global tables know it.
	code, _ = e.do(t, "POST", "/api/v1/apps/demo-app/dispatch", `{"category":"never_registered","audiences":["alice"]}`, trigger)
	if code != 422 {
		t.Fatalf("unregistered category: want 422, got %d", code)
	}

	// Dispatch with the category default urgency (缺省取品类默认).
	code, body := e.do(t, "POST", "/api/v1/apps/demo-app/dispatch", `{"category":"alerts","audiences":["alice"],"dedup_key":"node-17"}`, trigger)
	if code != 200 {
		t.Fatalf("dispatch: got %d %v", code, body)
	}
	data, _ := body["data"].(map[string]any)
	if data["category"] != "alerts" || data["urgency"] != "urgent" || data["mode"] != "escalation" {
		t.Fatalf("outcome: want alerts/urgent/escalation, got %v", data)
	}
	dispatched, _ := data["dispatched"].([]any)
	if len(dispatched) != 1 {
		t.Fatalf("dispatched: want alice, got %v", data["dispatched"])
	}
	if len(data["task_ids"].([]any)) != 1 {
		t.Fatalf("tasks: want 1, got %v", data["task_ids"])
	}

	// The dedup gate folds the repeat — 受理 still succeeds.
	code, body = e.do(t, "POST", "/api/v1/apps/demo-app/dispatch", `{"category":"alerts","audiences":["alice"],"dedup_key":"node-17"}`, trigger)
	if code != 200 {
		t.Fatalf("repeat dispatch: got %d", code)
	}
	data, _ = body["data"].(map[string]any)
	if data["suppressed"] != true {
		t.Fatalf("repeat: want suppressed, got %v", data)
	}
	if _, ok := data["task_ids"]; ok {
		t.Fatalf("repeat: want no task ids, got %v", data["task_ids"])
	}

	// A payload urgency overrides the category default.
	code, body = e.do(t, "POST", "/api/v1/apps/demo-app/dispatch", `{"category":"alerts","urgency":"critical","audiences":["alice"],"event_id":"evt-1"}`, trigger)
	if code != 200 {
		t.Fatalf("urgency override: got %d", code)
	}
	data, _ = body["data"].(map[string]any)
	if data["urgency"] != "critical" {
		t.Fatalf("urgency override: want critical, got %v", data["urgency"])
	}

	// Explicit relation vocabulary: both spellings are accepted —
	// 指派型触发必须显式声明 is a contract, not a refusal.
	for _, rt := range []string{"subscription", "enrollment"} {
		code, _ = e.do(t, "POST", "/api/v1/apps/demo-app/dispatch",
			`{"category":"alerts","audiences":["alice"],"relation_type":"`+rt+`","event_id":"evt-`+rt+`"}`, trigger)
		if code != 200 {
			t.Fatalf("relation_type %s: want 200, got %d", rt, code)
		}
	}

	// A template the namespace does not hold refuses the dispatch (the
	// global table stays invisible) — the service error surfaces 422.
	code, _ = e.do(t, "POST", "/api/v1/apps/demo-app/dispatch", `{"category":"alerts","audiences":["alice"],"template":"ghost","event_id":"evt-tpl"}`, trigger)
	if code != 422 {
		t.Fatalf("unknown template: want 422, got %d", code)
	}

	// Nil delivery policy keeps the face closed.
	e2 := newTestEnv(t, func(c *Config) { c.Apps = seedAppsRegistry() })
	code, _ = e2.do(t, "POST", "/api/v1/apps/demo-app/dispatch", `{"category":"alerts","audiences":["alice"]}`, trigger)
	if code != 404 {
		t.Fatalf("nil delivery policy: want 404, got %d", code)
	}
}

// TestAppQueryFaces covers §13.4: the namespace's delivery attempts,
// its audit trail, and the operator-side audience relations read.
func TestAppQueryFaces(t *testing.T) {
	e := dispatchEnv(t)
	trigger := map[string]string{"Authorization": "Bearer demo-app-trigger"}
	full := map[string]string{"Authorization": "Bearer demo-app-full"}

	// Dispatch once so the logstore holds exactly one app:demo-app row.
	code, _ := e.do(t, "POST", "/api/v1/apps/demo-app/categories", `{"name":"alerts","default_urgency":"urgent"}`, full)
	if code != 200 {
		t.Fatalf("category register: got %d", code)
	}
	code, dispBody := e.do(t, "POST", "/api/v1/apps/demo-app/dispatch", `{"category":"alerts","audiences":["alice"],"dedup_key":"k1"}`, trigger)
	if code != 200 {
		t.Fatalf("dispatch: got %d", code)
	}
	// The row the query face reads appears when the task is delivered —
	// walk the one accepted task through the real Deliver path (the
	// worker's Pop→Deliver→Ack sequence) so its snapped query
	// dimensions land in the logstore.
	dispData, _ := dispBody["data"].(map[string]any)
	if ids, _ := dispData["task_ids"].([]any); len(ids) != 1 {
		t.Fatalf("dispatch: want 1 task, got %v", dispData["task_ids"])
	}
	for e.queue.Size() > 0 {
		task, err := e.queue.Pop(context.Background())
		if err != nil || task == nil {
			break
		}
		if err := e.runtime.Deliver(context.Background(), task); err != nil {
			t.Fatalf("deliver queued task: %v", err)
		}
		_ = e.queue.Ack(context.Background(), task.ID)
	}

	// Deliveries: query power; the trigger-only token stays out.
	code, _ = e.do(t, "GET", "/api/v1/apps/demo-app/deliveries", "", trigger)
	if code != 403 {
		t.Fatalf("deliveries with trigger token: want 403, got %d", code)
	}
	code, body := e.do(t, "GET", "/api/v1/apps/demo-app/deliveries", "", full)
	if code != 200 {
		t.Fatalf("deliveries: got %d", code)
	}
	data, _ := body["data"].(map[string]any)
	if data["total"].(float64) != 1 {
		t.Fatalf("total: want 1, got %v", data["total"])
	}
	logs, _ := data["logs"].([]any)
	row, _ := logs[0].(map[string]any)
	if row["audience_id"] != "alice" || row["category"] != "alerts" || row["source"] != "app:demo-app" {
		t.Fatalf("row: want alice/alerts/app:demo-app, got %v", row)
	}

	// Narrows.
	code, body = e.do(t, "GET", "/api/v1/apps/demo-app/deliveries?audience=bob", "", full)
	data, _ = body["data"].(map[string]any)
	if code != 200 || data["total"].(float64) != 0 {
		t.Fatalf("audience=bob: want 200/0, got %d/%v", code, data["total"])
	}
	code, body = e.do(t, "GET", "/api/v1/apps/demo-app/deliveries?category=billing", "", full)
	data, _ = body["data"].(map[string]any)
	if code != 200 || data["total"].(float64) != 0 {
		t.Fatalf("category=billing: want 200/0, got %d/%v", code, data["total"])
	}
	// Page size clamps at the ceiling.
	code, body = e.do(t, "GET", "/api/v1/apps/demo-app/deliveries?limit=1000", "", full)
	data, _ = body["data"].(map[string]any)
	if code != 200 || data["limit"].(float64) != 500 {
		t.Fatalf("limit clamp: want 200/500, got %d/%v", code, data["limit"])
	}

	// Audit: nil store keeps the endpoint 404 (dispatchEnv sets none).
	code, _ = e.do(t, "GET", "/api/v1/apps/demo-app/audit", "", full)
	if code != 404 {
		t.Fatalf("audit without store: want 404, got %d", code)
	}

	// Audience relations: operator read of one audience's standing
	// relations; the audience with a subscription lists exactly it.
	code, body = e.do(t, "GET", "/api/v1/audiences/alice/relations", "", nil)
	if code != 200 {
		t.Fatalf("relations: got %d", code)
	}
	data, _ = body["data"].(map[string]any)
	rels, _ := data["relations"].([]any)
	if len(rels) != 1 {
		t.Fatalf("relations: want 1, got %v", data["relations"])
	}
	rel, _ := rels[0].(map[string]any)
	if rel["type"] != "subscription" || rel["channel"] != "email" ||
		rel["audience_id"] != "alice" || rel["source"] != "test" {
		t.Fatalf("relation: want alice/subscription/email from test, got %v", rel)
	}
	policy, _ := rel["policy"].(map[string]any)
	if policy["allow_unsubscribe"] != true || policy["must_deliver"] != false {
		t.Fatalf("relation policy: want default bits, got %v", policy)
	}
	code, body = e.do(t, "GET", "/api/v1/audiences/ghost/relations", "", nil)
	if code != 200 {
		t.Fatalf("ghost relations: want 200, got %d", code)
	}
	data, _ = body["data"].(map[string]any)
	if rels, _ = data["relations"].([]any); rels == nil || len(rels) != 0 {
		t.Fatalf("ghost relations: want [], got %v", data["relations"])
	}
	code, _ = e.do(t, "PUT", "/api/v1/audiences/alice/relations", "", nil)
	if code != 405 {
		t.Fatalf("PUT relations: want 405, got %d", code)
	}

	// Nil relations registry keeps the read closed.
	e2 := newTestEnv(t)
	code, _ = e2.do(t, "GET", "/api/v1/audiences/alice/relations", "", nil)
	if code != 404 {
		t.Fatalf("relations without registry: want 404, got %d", code)
	}
}

// TestAppAuditTrail wires the §12 store and reads the namespace's trail
// through it: a suppressed repeat lands its deduped row with the
// app:demo-app source, and ?since= narrows.
func TestAppAuditTrail(t *testing.T) {
	relations := audience.NewRegistry()
	surfaces := audience.NewSurfaceRegistry()
	if _, err := surfaces.Activate("alice", "email", "alice@example.com", "test"); err != nil {
		t.Fatalf("activate: %v", err)
	}
	if err := relations.Subscribe(audience.Relation{
		AudienceID: "alice", Category: "alerts", Channel: "email",
		Type: audience.RelationSubscription, Source: "test",
	}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	policy, _ := audience.NewDeliveryPolicy(map[string]string{"alerts": "urgent"}, nil, nil, false)
	e := newTestEnv(t, func(c *Config) {
		c.Apps = seedAppsRegistry()
		c.Delivery = policy
		c.Filter = audience.NewFilter(relations, surfaces)
		c.FeedSurfaces = surfaces
		c.Audit = audit.New(0)
	})
	if err := e.runtime.RegisterProvider("email", &stubProvider{name: "email", pType: "email"}, true); err != nil {
		t.Fatalf("register provider: %v", err)
	}
	full := map[string]string{"Authorization": "Bearer demo-app-full"}
	trigger := map[string]string{"Authorization": "Bearer demo-app-trigger"}

	e.do(t, "POST", "/api/v1/apps/demo-app/categories", `{"name":"alerts","default_urgency":"urgent"}`, full)
	e.do(t, "POST", "/api/v1/apps/demo-app/dispatch", `{"category":"alerts","audiences":["alice"],"dedup_key":"k2"}`, trigger)
	// The repeat folds — that is what the trail answers 为什么这条没投.
	e.do(t, "POST", "/api/v1/apps/demo-app/dispatch", `{"category":"alerts","audiences":["alice"],"dedup_key":"k2"}`, trigger)

	code, body := e.do(t, "GET", "/api/v1/apps/demo-app/audit", "", full)
	if code != 200 {
		t.Fatalf("audit: got %d", code)
	}
	data, _ := body["data"].(map[string]any)
	events, _ := data["events"].([]any)
	if len(events) != 1 {
		t.Fatalf("events: want the deduped row, got %v", data["events"])
	}
	ev, _ := events[0].(map[string]any)
	if ev["kind"] != "delivery.deduped" || ev["source"] != "app:demo-app" {
		t.Fatalf("event: want deduped from app:demo-app, got %v", ev)
	}

	// since in the future reads empty; a malformed stamp is 400.
	future := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	code, body = e.do(t, "GET", "/api/v1/apps/demo-app/audit?since="+future, "", full)
	data, _ = body["data"].(map[string]any)
	if code != 200 || len(data["events"].([]any)) != 0 {
		t.Fatalf("future since: want 200/0, got %d/%v", code, data["events"])
	}
	code, _ = e.do(t, "GET", "/api/v1/apps/demo-app/audit?since=yesterday", "", full)
	if code != 400 {
		t.Fatalf("bad since: want 400, got %d", code)
	}
	// Query power gates the read too.
	code, _ = e.do(t, "GET", "/api/v1/apps/demo-app/audit", "", trigger)
	if code != 403 {
		t.Fatalf("audit with trigger token: want 403, got %d", code)
	}
}
