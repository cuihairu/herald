package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cuihairu/herald/core"
	"github.com/cuihairu/herald/core/rules"
)

func withRulesEngine(e *rules.Engine) func(*Config) {
	return func(c *Config) { c.Rules = e }
}

const validRuleBody = `{"id":"p1","match":"level == \"error\"","mode":"active","route":[{"match":"","channels":["rec"]}]}`

func TestHandleRulesWithoutEngine(t *testing.T) {
	env := newTestEnv(t) // no Rules configured

	for _, tc := range []struct {
		name   string
		method string
		path   string
	}{
		{"list", http.MethodGet, "/api/v1/rules"},
		{"create", http.MethodPost, "/api/v1/rules"},
		{"get", http.MethodGet, "/api/v1/rules/p1"},
		{"update", http.MethodPut, "/api/v1/rules/p1"},
		{"delete", http.MethodDelete, "/api/v1/rules/p1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, resp := env.do(t, tc.method, tc.path, "{}", nil)
			if code != http.StatusServiceUnavailable {
				t.Fatalf("expected status 503, got %d", code)
			}
			if resp["message"] != "rules engine is not configured" {
				t.Errorf("unexpected message: %v", resp["message"])
			}
		})
	}
}

func TestHandleRulesList(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		env := newTestEnv(t, withRulesEngine(rules.NewEngine(rules.NewMemoryStore())))
		code, resp := env.do(t, http.MethodGet, "/api/v1/rules", "", nil)
		if code != http.StatusOK {
			t.Fatalf("expected status 200, got %d", code)
		}
		data := dataOf(t, resp)
		if num(t, data["count"]) != 0 {
			t.Errorf("expected count 0, got %v", data["count"])
		}
	})

	t.Run("list after create", func(t *testing.T) {
		env := newTestEnv(t, withRulesEngine(rules.NewEngine(rules.NewMemoryStore())))
		if code, _ := env.do(t, http.MethodPost, "/api/v1/rules", validRuleBody, nil); code != http.StatusOK {
			t.Fatalf("create failed with status %d", code)
		}

		_, resp := env.do(t, http.MethodGet, "/api/v1/rules", "", nil)
		data := dataOf(t, resp)
		if num(t, data["count"]) != 1 {
			t.Fatalf("expected count 1, got %v", data["count"])
		}
		list, _ := data["rules"].([]any)
		entry, _ := list[0].(map[string]any)
		if entry["id"] != "p1" {
			t.Errorf("expected id p1, got %v", entry["id"])
		}
		// Empty mode is normalized to shadow at save time.
		if entry["mode"] != string(rules.ModeActive) {
			t.Errorf("expected mode active to round-trip, got %v", entry["mode"])
		}
	})

	t.Run("method not allowed", func(t *testing.T) {
		env := newTestEnv(t, withRulesEngine(rules.NewEngine(rules.NewMemoryStore())))
		code, _ := env.do(t, http.MethodPatch, "/api/v1/rules", "", nil)
		if code != http.StatusMethodNotAllowed {
			t.Fatalf("expected status 405, got %d", code)
		}
		code, _ = env.do(t, http.MethodPatch, "/api/v1/rules/p1", "", nil)
		if code != http.StatusMethodNotAllowed {
			t.Fatalf("expected status 405 by id, got %d", code)
		}
	})
}

func TestHandleCreateRule(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		env := newTestEnv(t, withRulesEngine(rules.NewEngine(rules.NewMemoryStore())))
		code, resp := env.do(t, http.MethodPost, "/api/v1/rules", validRuleBody, nil)
		if code != http.StatusOK {
			t.Fatalf("expected status 200, got %d", code)
		}
		data := dataOf(t, resp)
		if data["id"] != "p1" {
			t.Errorf("expected id p1, got %v", data["id"])
		}
	})

	t.Run("invalid body", func(t *testing.T) {
		env := newTestEnv(t, withRulesEngine(rules.NewEngine(rules.NewMemoryStore())))
		code, _ := env.do(t, http.MethodPost, "/api/v1/rules", "not-json", nil)
		if code != http.StatusBadRequest {
			t.Fatalf("expected status 400, got %d", code)
		}
	})

	t.Run("duplicate id conflicts", func(t *testing.T) {
		env := newTestEnv(t, withRulesEngine(rules.NewEngine(rules.NewMemoryStore())))
		if code, _ := env.do(t, http.MethodPost, "/api/v1/rules", validRuleBody, nil); code != http.StatusOK {
			t.Fatalf("first create failed with status %d", code)
		}
		code, resp := env.do(t, http.MethodPost, "/api/v1/rules", validRuleBody, nil)
		if code != http.StatusConflict {
			t.Fatalf("expected status 409, got %d", code)
		}
		if resp["message"] != "rule already exists: p1" {
			t.Errorf("unexpected message: %v", resp["message"])
		}
	})

	t.Run("invalid expression rejected", func(t *testing.T) {
		env := newTestEnv(t, withRulesEngine(rules.NewEngine(rules.NewMemoryStore())))
		code, resp := env.do(t, http.MethodPost, "/api/v1/rules",
			`{"id":"bad","match":"host == \"x\""}`, nil)
		if code != http.StatusBadRequest {
			t.Fatalf("expected status 400, got %d", code)
		}
		if resp["message"] == "" {
			t.Error("expected compile error in message")
		}
	})

	t.Run("invalid id rejected", func(t *testing.T) {
		env := newTestEnv(t, withRulesEngine(rules.NewEngine(rules.NewMemoryStore())))
		code, _ := env.do(t, http.MethodPost, "/api/v1/rules",
			`{"id":"bad id!","match":"true","route":[{"channels":["rec"]}]}`, nil)
		if code != http.StatusBadRequest {
			t.Fatalf("expected status 400, got %d", code)
		}
	})
}

func TestHandleRuleByID(t *testing.T) {
	newEnv := func(t *testing.T) *testEnv {
		return newTestEnv(t, withRulesEngine(rules.NewEngine(rules.NewMemoryStore())))
	}

	t.Run("get existing", func(t *testing.T) {
		env := newEnv(t)
		if code, _ := env.do(t, http.MethodPost, "/api/v1/rules", validRuleBody, nil); code != http.StatusOK {
			t.Fatalf("create failed with status %d", code)
		}
		code, resp := env.do(t, http.MethodGet, "/api/v1/rules/p1", "", nil)
		if code != http.StatusOK {
			t.Fatalf("expected status 200, got %d", code)
		}
		data := dataOf(t, resp)
		if data["id"] != "p1" {
			t.Errorf("expected id p1, got %v", data["id"])
		}
	})

	t.Run("get missing returns 404", func(t *testing.T) {
		env := newEnv(t)
		code, resp := env.do(t, http.MethodGet, "/api/v1/rules/nope", "", nil)
		if code != http.StatusNotFound {
			t.Fatalf("expected status 404, got %d", code)
		}
		if resp["message"] != "rule not found: nope" {
			t.Errorf("unexpected message: %v", resp["message"])
		}
	})

	t.Run("update via put", func(t *testing.T) {
		env := newEnv(t)
		if code, _ := env.do(t, http.MethodPost, "/api/v1/rules", validRuleBody, nil); code != http.StatusOK {
			t.Fatalf("create failed with status %d", code)
		}
		// Body carries a different id; the URL must win.
		code, resp := env.do(t, http.MethodPut, "/api/v1/rules/p1",
			`{"id":"other","match":"type == \"deploy\"","mode":"active","route":[{"match":"","channels":["rec"]}]}`, nil)
		if code != http.StatusOK {
			t.Fatalf("expected status 200, got %d", code)
		}
		if dataOf(t, resp)["id"] != "p1" {
			t.Errorf("expected url id p1 to win, got %v", dataOf(t, resp)["id"])
		}

		_, resp = env.do(t, http.MethodGet, "/api/v1/rules/p1", "", nil)
		if dataOf(t, resp)["match"] != `type == "deploy"` {
			t.Errorf("expected updated match, got %v", dataOf(t, resp)["match"])
		}
		if _, resp = env.do(t, http.MethodGet, "/api/v1/rules/other", "", nil); resp["code"] == float64(0) {
			t.Error("expected body id not to create a second rule")
		}
	})

	t.Run("update invalid body returns 400", func(t *testing.T) {
		env := newEnv(t)
		code, _ := env.do(t, http.MethodPut, "/api/v1/rules/p1", "not-json", nil)
		if code != http.StatusBadRequest {
			t.Fatalf("expected status 400, got %d", code)
		}
	})

	t.Run("update invalid expression returns 400", func(t *testing.T) {
		env := newEnv(t)
		if code, _ := env.do(t, http.MethodPost, "/api/v1/rules", validRuleBody, nil); code != http.StatusOK {
			t.Fatalf("create failed with status %d", code)
		}
		code, _ := env.do(t, http.MethodPut, "/api/v1/rules/p1",
			`{"match":"nonsense_func()"}`, nil)
		if code != http.StatusBadRequest {
			t.Fatalf("expected status 400, got %d", code)
		}
	})

	t.Run("delete existing", func(t *testing.T) {
		env := newEnv(t)
		if code, _ := env.do(t, http.MethodPost, "/api/v1/rules", validRuleBody, nil); code != http.StatusOK {
			t.Fatalf("create failed with status %d", code)
		}
		if code, _ := env.do(t, http.MethodDelete, "/api/v1/rules/p1", "", nil); code != http.StatusOK {
			t.Fatalf("delete failed with status %d", code)
		}
		if code, _ := env.do(t, http.MethodGet, "/api/v1/rules/p1", "", nil); code != http.StatusNotFound {
			t.Errorf("expected 404 after delete, got %d", code)
		}
	})

	t.Run("delete missing returns 404", func(t *testing.T) {
		env := newEnv(t)
		code, _ := env.do(t, http.MethodDelete, "/api/v1/rules/nope", "", nil)
		if code != http.StatusNotFound {
			t.Fatalf("expected status 404, got %d", code)
		}
	})
}

func TestRuleCRUDPersistsThroughFileStore(t *testing.T) {
	path := t.TempDir() + "/rules.json"
	store, err := rules.NewFileStore(path)
	if err != nil {
		t.Fatalf("failed to create file store: %v", err)
	}
	env := newTestEnv(t, withRulesEngine(rules.NewEngine(store)))

	if code, _ := env.do(t, http.MethodPost, "/api/v1/rules", validRuleBody, nil); code != http.StatusOK {
		t.Fatalf("create failed with status %d", code)
	}
	if code, _ := env.do(t, http.MethodDelete, "/api/v1/rules/p1", "", nil); code != http.StatusOK {
		t.Fatalf("delete failed with status %d", code)
	}

	// The file must reflect the final state (empty rules array).
	reloaded, err := rules.NewFileStore(path)
	if err != nil {
		t.Fatalf("failed to reopen store: %v", err)
	}
	got, err := reloaded.List(t.Context())
	if err != nil {
		t.Fatalf("failed to list: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty persisted list, got %d rules", len(got))
	}
}

// failingRulesStore errors on every operation, exercising the handlers'
// store-failure paths.
type failingRulesStore struct{}

func (failingRulesStore) List(context.Context) ([]rules.Rule, error) {
	return nil, errors.New("store down")
}
func (failingRulesStore) Get(context.Context, string) (rules.Rule, error) {
	return rules.Rule{}, errors.New("store down")
}
func (failingRulesStore) Put(context.Context, rules.Rule) error { return errors.New("store down") }
func (failingRulesStore) Delete(context.Context, string) error {
	return errors.New("store down")
}

func TestHandleRulesStoreFailures(t *testing.T) {
	env := newTestEnv(t, withRulesEngine(rules.NewEngine(failingRulesStore{})))

	// Listing reads the live table (the evaluation truth), not the store:
	// a dead store must not take the read path with it — the table that
	// routes traffic is exactly what the API should report.
	if code, body := env.do(t, http.MethodGet, "/api/v1/rules", "", nil); code != http.StatusOK {
		t.Fatalf("list must serve from the live table, got %d (%s)", code, body)
	}
	// Get failing with something other than ErrNotFound must surface as
	// 500 — both on the direct read and on the create-time existence check.
	if code, _ := env.do(t, http.MethodGet, "/api/v1/rules/p1", "", nil); code != http.StatusInternalServerError {
		t.Fatalf("get on a failing store must be 500, got %d", code)
	}
	if code, _ := env.do(t, http.MethodPost, "/api/v1/rules", validRuleBody, nil); code != http.StatusInternalServerError {
		t.Fatalf("create's existence check on a failing store must be 500, got %d", code)
	}
	// Same for delete.
	if code, _ := env.do(t, http.MethodDelete, "/api/v1/rules/p1", "", nil); code != http.StatusInternalServerError {
		t.Fatalf("delete on a failing store must be 500, got %d", code)
	}
}

// The shadow statistic is what an operator reads before flipping a rule to
// active, so it must be the shadow-hit count alone: the events the engine
// withholds for other reasons are counted under their own kinds.
func TestHandleRulesShadowHits(t *testing.T) {
	newEnv := func(t *testing.T) *testEnv {
		return newTestEnv(t, withRulesEngine(rules.NewEngine(rules.NewMemoryStore())))
	}
	observe := func(env *testEnv) {
		t.Helper()
		n := &core.Notification{ID: "n1", Type: "deploy", Level: "error"}
		env.runtime.RecordShadow("p1", []string{"rec"}, n)
		env.runtime.RecordShadow("p1", []string{"rec"}, n)
		env.runtime.RecordShadow("p1", []string{"rec"}, n)
		// Withheld for other reasons — must not show up as shadow hits.
		env.runtime.RecordForPending("p1", n)
		env.runtime.RecordSuppressed("p1", n)
	}

	t.Run("list reports hits per rule", func(t *testing.T) {
		env := newEnv(t)
		if code, _ := env.do(t, http.MethodPost, "/api/v1/rules", validRuleBody, nil); code != http.StatusOK {
			t.Fatalf("create failed with status %d", code)
		}
		observe(env)

		_, resp := env.do(t, http.MethodGet, "/api/v1/rules", "", nil)
		list, _ := dataOf(t, resp)["rules"].([]any)
		entry, _ := list[0].(map[string]any)
		if got := num(t, entry["shadow_hits"]); got != 3 {
			t.Errorf("expected 3 shadow hits, got %d", got)
		}
	})

	t.Run("detail reports hits and keeps the rule document", func(t *testing.T) {
		env := newEnv(t)
		if code, _ := env.do(t, http.MethodPost, "/api/v1/rules", validRuleBody, nil); code != http.StatusOK {
			t.Fatalf("create failed with status %d", code)
		}
		observe(env)

		code, resp := env.do(t, http.MethodGet, "/api/v1/rules/p1", "", nil)
		if code != http.StatusOK {
			t.Fatalf("expected status 200, got %d", code)
		}
		data := dataOf(t, resp)
		if got := num(t, data["shadow_hits"]); got != 3 {
			t.Errorf("expected 3 shadow hits, got %d", got)
		}
		// The rule document itself is untouched by the extra field.
		if data["id"] != "p1" || data["mode"] != string(rules.ModeActive) {
			t.Errorf("unexpected rule document: %v", data)
		}
	})

	t.Run("detail reports windows and samples", func(t *testing.T) {
		env := newEnv(t)
		if code, _ := env.do(t, http.MethodPost, "/api/v1/rules", validRuleBody, nil); code != http.StatusOK {
			t.Fatalf("create failed with status %d", code)
		}
		observe(env)

		_, resp := env.do(t, http.MethodGet, "/api/v1/rules/p1", "", nil)
		entry := dataOf(t, resp)
		stats, _ := entry["shadow_stats"].(map[string]any)
		if stats == nil {
			t.Fatalf("detail must carry shadow_stats, got %v", entry["shadow_stats"])
		}
		if got := num(t, stats["total"]); got != 3 {
			t.Errorf("total = %d, want 3", got)
		}
		if got := num(t, stats["last_24h"]); got != 3 {
			t.Errorf("last_24h = %d, want 3", got)
		}
		if got := num(t, stats["last_7d"]); got != 3 {
			t.Errorf("last_7d = %d, want 3", got)
		}
		samples, _ := stats["samples"].([]any)
		if len(samples) != 3 {
			t.Fatalf("samples = %d, want 3", len(samples))
		}
		first, _ := samples[0].(map[string]any)
		if first["type"] != "deploy" {
			t.Errorf("newest sample type = %v, want deploy", first["type"])
		}

		// The list view stays lean: the counter column only, no stats.
		_, listResp := env.do(t, http.MethodGet, "/api/v1/rules", "", nil)
		list, _ := dataOf(t, listResp)["rules"].([]any)
		row, _ := list[0].(map[string]any)
		if _, ok := row["shadow_stats"]; ok {
			t.Error("list rows must not carry shadow_stats")
		}
	})

	t.Run("a rule that never matched reports zero", func(t *testing.T) {
		env := newEnv(t)
		if code, _ := env.do(t, http.MethodPost, "/api/v1/rules", validRuleBody, nil); code != http.StatusOK {
			t.Fatalf("create failed with status %d", code)
		}
		_, resp := env.do(t, http.MethodGet, "/api/v1/rules/p1", "", nil)
		if got := num(t, dataOf(t, resp)["shadow_hits"]); got != 0 {
			t.Errorf("expected 0 shadow hits, got %d", got)
		}
	})

	t.Run("without a runtime manager the counter is zero", func(t *testing.T) {
		// The rules engine is optional wiring and can be attached to a
		// handler built without a runtime manager: the rule table is
		// still served, the live counter degrades to zero.
		engine := rules.NewEngine(rules.NewMemoryStore())
		seed := rules.Rule{ID: "p1", Match: "true", Mode: rules.ModeShadow,
			Route: []rules.RouteStep{{Channels: []string{"rec"}}}}
		if err := engine.Put(t.Context(), &seed); err != nil {
			t.Fatalf("failed to seed rule: %v", err)
		}
		h := NewHandler(nil, nil, nil)
		h.SetRuleEngine(engine)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/rules", nil)
		rec := httptest.NewRecorder()
		h.HandleRules(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d", rec.Code)
		}
		var body struct {
			Data struct {
				Rules []struct {
					ID         string `json:"id"`
					ShadowHits uint64 `json:"shadow_hits"`
				} `json:"rules"`
			} `json:"data"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}
		if len(body.Data.Rules) != 1 || body.Data.Rules[0].ShadowHits != 0 {
			t.Fatalf("expected one rule with zero shadow hits, got %+v", body.Data.Rules)
		}

		// The detail read degrades the same way: shadow_stats is omitted
		// rather than served empty.
		req = httptest.NewRequest(http.MethodGet, "/api/v1/rules/p1", nil)
		req.SetPathValue("id", "p1")
		rec = httptest.NewRecorder()
		h.HandleRuleByID(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d", rec.Code)
		}
		var detail map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil {
			t.Fatalf("failed to decode detail: %v", err)
		}
		if _, ok := dataOf(t, detail)["shadow_stats"]; ok {
			t.Error("without a runtime manager shadow_stats must be omitted")
		}
	})
}

// The mux never routes an empty {id}, so the empty-id guard is exercised
// by invoking the handler directly with an empty path value.
func TestHandleRuleByIDEmptyID(t *testing.T) {
	env := newTestEnv(t, withRulesEngine(rules.NewEngine(rules.NewMemoryStore())))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/rules/x", nil)
	req.SetPathValue("id", "")
	rec := httptest.NewRecorder()
	env.server.handler.HandleRuleByID(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty rule id must be 400, got %d", rec.Code)
	}
}
