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

// TestAppPolicies walks the §13.2 policy override face: per-family PUT
// with refuse-not-clamp validation, PUT-is-replace semantics, family
// isolation and the aggregate read-back.
func TestAppPolicies(t *testing.T) {
	e := newTestEnv(t, func(c *Config) { c.Apps = seedAppsRegistry() })
	put := func(t *testing.T, path, body string) (int, map[string]any) {
		t.Helper()
		return e.do(t, "PUT", "/api/v1/apps/ferry"+path, body, map[string]string{"Authorization": "Bearer ferry-full"})
	}

	t.Run("write needs config scope", func(t *testing.T) {
		code, _ := e.do(t, "PUT", "/api/v1/apps/ferry/policies/intensity", `{"sms":"L4"}`, map[string]string{"Authorization": "Bearer ferry-trigger"})
		if code != 403 {
			t.Fatalf("code = %d, want 403", code)
		}
	})

	t.Run("read needs query scope", func(t *testing.T) {
		code, _ := e.do(t, "GET", "/api/v1/apps/ferry/policies", "", map[string]string{"Authorization": "Bearer ferry-config"})
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
		code, body := e.do(t, "GET", "/api/v1/apps/ferry/policies", "", map[string]string{"Authorization": "Bearer ferry-full"})
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
		_, body := e.do(t, "GET", "/api/v1/apps/ferry/policies", "", map[string]string{"Authorization": "Bearer ferry-full"})
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
		code, body := e2.do(t, "GET", "/api/v1/apps/ferry/policies", "", map[string]string{"Authorization": "Bearer ferry-full"})
		if code != 200 {
			t.Fatalf("read = %d, want 200", code)
		}
		data, _ := body["data"].(map[string]any)
		if len(data) != 0 {
			t.Errorf("data = %v, want empty object", data)
		}
	})
}
