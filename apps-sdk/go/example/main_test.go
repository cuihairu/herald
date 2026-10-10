package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// demoStub answers the demo's one pass against a minimal herald. The
// step named by fail answers a broken envelope instead, and every face
// it serves records one hit so tests can tell a skipped step from a
// served one.
type demoStub struct {
	server *httptest.Server
	hits   map[string]int
}

func newDemoStub(t *testing.T, fail string, scopes []string) *demoStub {
	t.Helper()
	s := &demoStub{hits: map[string]int{}}
	ok := func(step string, data any) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			s.hits[step]++
			w.Header().Set("Content-Type", "application/json")
			if step == fail {
				_ = json.NewEncoder(w).Encode(map[string]any{"code": 500, "message": "step broken"})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "message": "ok", "data": data})
		}
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/apps/demo-app", ok("show", map[string]any{
		"name": "demo-app", "scopes": scopes,
	}))
	mux.HandleFunc("GET /api/v1/apps/demo-app/categories", ok("categories", map[string]any{
		"categories": []map[string]any{{"name": "alerts", "default_urgency": "urgent"}},
	}))
	mux.HandleFunc("POST /api/v1/apps/demo-app/events", ok("trigger", map[string]any{
		"notification_id": "n-1", "category": "alerts", "urgency": "normal", "task_ids": []string{"t-1"},
		"refused": []map[string]any{{"audience": "bob", "channel": "sms", "reason": "no_channels"}},
	}))
	mux.HandleFunc("GET /api/v1/apps/demo-app/deliveries", ok("deliveries", map[string]any{
		"total": 1, "offset": 0, "limit": 5,
		"logs": []map[string]any{{
			"id": "l-1", "provider": "email", "status": "success",
			"audience_id": "user.1", "category": "alerts", "created_at": "2026-10-10T12:00:00Z",
		}},
	}))
	mux.HandleFunc("GET /api/v1/apps/demo-app/audit", ok("audit", map[string]any{"events": []any{}}))
	s.server = httptest.NewServer(mux)
	t.Cleanup(s.server.Close)
	return s
}

func TestRunRefusesWithoutToken(t *testing.T) {
	err := run(context.Background(), demoConfig{BaseURL: "http://127.0.0.1:1", App: "demo-app"})
	if err == nil || !strings.Contains(err.Error(), "HERALD_TOKEN is required") {
		t.Fatalf("want the token refusal, got %v", err)
	}
}

func TestRunWalksTheFlow(t *testing.T) {
	s := newDemoStub(t, "", []string{"config", "trigger", "query"})
	err := run(context.Background(), demoConfig{BaseURL: s.server.URL, App: "demo-app", Token: "tok"})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, step := range []string{"show", "categories", "trigger", "deliveries", "audit"} {
		if s.hits[step] != 1 {
			t.Errorf("step %s: %d hits, want 1", step, s.hits[step])
		}
	}
}

func TestRunSkipsTriggerWithoutScope(t *testing.T) {
	s := newDemoStub(t, "", []string{"query"})
	err := run(context.Background(), demoConfig{BaseURL: s.server.URL, App: "demo-app", Token: "tok"})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if s.hits["trigger"] != 0 {
		t.Errorf("trigger ran %d times without the scope, want skipped", s.hits["trigger"])
	}
	if s.hits["deliveries"] != 1 || s.hits["audit"] != 1 {
		t.Errorf("reads: %v, want deliveries and audit each once", s.hits)
	}
}

func TestRunStepFailures(t *testing.T) {
	cases := []struct{ fail, want string }{
		{"show", "self-check:"},
		{"categories", "categories:"},
		{"trigger", "trigger:"},
		{"deliveries", "deliveries:"},
		{"audit", "audit:"},
	}
	for _, tc := range cases {
		s := newDemoStub(t, tc.fail, []string{"config", "trigger", "query"})
		err := run(context.Background(), demoConfig{BaseURL: s.server.URL, App: "demo-app", Token: "tok"})
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("fail %s: want a %q step error, got %v", tc.fail, tc.want, err)
		}
	}
}

func TestDemoConfigReadsEnvironment(t *testing.T) {
	t.Setenv("HERALD_URL", "http://herald.example:8080")
	t.Setenv("HERALD_APP", "croupier")
	t.Setenv("HERALD_TOKEN", "t1")
	cfg := loadDemoConfig()
	if cfg.BaseURL != "http://herald.example:8080" || cfg.App != "croupier" || cfg.Token != "t1" {
		t.Fatalf("cfg = %+v, want the env values", cfg)
	}
}

func TestDemoConfigDefaults(t *testing.T) {
	t.Setenv("HERALD_URL", "")
	t.Setenv("HERALD_APP", "")
	t.Setenv("HERALD_TOKEN", "")
	cfg := loadDemoConfig()
	if cfg.BaseURL != "http://127.0.0.1:8080" || cfg.App != "demo-app" || cfg.Token != "" {
		t.Fatalf("cfg = %+v, want the local defaults", cfg)
	}
}
