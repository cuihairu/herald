package api

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/cuihairu/herald/core/ack"
	"github.com/cuihairu/herald/core/incident"
	"github.com/cuihairu/herald/core/rules"
)

func withIncidentStore(s *incident.Store) func(*Config) {
	return func(c *Config) { c.Incidents = s }
}

func TestHandleIncidentsWithoutStore(t *testing.T) {
	env := newTestEnv(t) // no incident store configured

	code, resp := env.do(t, http.MethodGet, "/api/v1/incidents", "", nil)
	if code != http.StatusServiceUnavailable {
		t.Fatalf("expected status 503, got %d", code)
	}
	if resp["message"] != "incident store is not configured" {
		t.Errorf("unexpected message: %v", resp["message"])
	}
}

func TestHandleIncidentsListAndFilter(t *testing.T) {
	ledger := incident.New(0)
	ledger.Open(incident.Opening{RuleID: "r1", GroupKey: "g1", AlertID: "a1", Title: "one"})
	ledger.Open(incident.Opening{RuleID: "r2", GroupKey: "g2", AlertID: "a2", Title: "two"})
	ledger.Ack("a2", "alice")
	env := newTestEnv(t, withIncidentStore(ledger))

	// Unfiltered list, newest first.
	code, resp := env.do(t, http.MethodGet, "/api/v1/incidents", "", nil)
	if code != http.StatusOK {
		t.Fatalf("list failed: %d", code)
	}
	data := dataOf(t, resp)
	if n := num(t, data["count"]); n != 2 {
		t.Fatalf("expected 2 incidents, got %d", n)
	}
	list := data["incidents"].([]any)
	first := list[0].(map[string]any)
	if first["alert_id"] != "a2" {
		t.Fatalf("list must be newest-first, got %+v", first)
	}

	// Status filter.
	code, resp = env.do(t, http.MethodGet, "/api/v1/incidents?status=acked", "", nil)
	if code != http.StatusOK {
		t.Fatalf("filtered list failed: %d", code)
	}
	if n := num(t, dataOf(t, resp)["count"]); n != 1 {
		t.Fatalf("expected 1 acked incident, got %d", n)
	}

	// Alert filter.
	code, resp = env.do(t, http.MethodGet, "/api/v1/incidents?alert_id=a1", "", nil)
	if code != http.StatusOK {
		t.Fatalf("alert-filtered list failed: %d", code)
	}
	if n := num(t, dataOf(t, resp)["count"]); n != 1 {
		t.Fatalf("expected 1 incident for a1, got %d", n)
	}

	// Limit truncates the newest-first list.
	code, resp = env.do(t, http.MethodGet, "/api/v1/incidents?limit=1", "", nil)
	if code != http.StatusOK {
		t.Fatalf("limited list failed: %d", code)
	}
	if n := num(t, dataOf(t, resp)["count"]); n != 1 {
		t.Fatalf("expected the limit to truncate the list, got %d", n)
	}

	// Invalid filters are rejected.
	code, _ = env.do(t, http.MethodGet, "/api/v1/incidents?status=bogus", "", nil)
	if code != http.StatusBadRequest {
		t.Fatalf("invalid status must be 400, got %d", code)
	}
	code, _ = env.do(t, http.MethodGet, "/api/v1/incidents?limit=nope", "", nil)
	if code != http.StatusBadRequest {
		t.Fatalf("invalid limit must be 400, got %d", code)
	}
}

func TestHandleIncidentByID(t *testing.T) {
	ledger := incident.New(0)
	inc := ledger.Open(incident.Opening{RuleID: "r1", GroupKey: "g1", AlertID: "a1", Title: "one"})
	env := newTestEnv(t, withIncidentStore(ledger))

	code, resp := env.do(t, http.MethodGet, "/api/v1/incidents/"+inc.ID, "", nil)
	if code != http.StatusOK {
		t.Fatalf("get failed: %d", code)
	}
	if got := dataOf(t, resp)["alert_id"]; got != "a1" {
		t.Fatalf("unexpected incident: %+v", resp)
	}

	code, _ = env.do(t, http.MethodGet, "/api/v1/incidents/no-such-id", "", nil)
	if code != http.StatusNotFound {
		t.Fatalf("unknown id must be 404, got %d", code)
	}
}

func TestHandleIncidentByIDGuards(t *testing.T) {
	// No store configured: the endpoint reports unavailable.
	env := newTestEnv(t)
	if code, _ := env.do(t, http.MethodGet, "/api/v1/incidents/some-id", "", nil); code != http.StatusServiceUnavailable {
		t.Fatalf("no store must be 503, got %d", code)
	}
	// Store configured, but the method is not GET.
	env = newTestEnv(t, withIncidentStore(incident.New(0)))
	if code, _ := env.do(t, http.MethodPost, "/api/v1/incidents/some-id", "{}", nil); code != http.StatusMethodNotAllowed {
		t.Fatalf("POST must be 405, got %d", code)
	}
}

func TestHandleIncidentsMethodNotAllowed(t *testing.T) {
	ledger := incident.New(0)
	env := newTestEnv(t, withIncidentStore(ledger))

	code, _ := env.do(t, http.MethodPost, "/api/v1/incidents", "{}", nil)
	if code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", code)
	}
}

func TestHandleAlertAckMarksIncident(t *testing.T) {
	ledger := incident.New(0)
	ledger.Open(incident.Opening{RuleID: "r1", GroupKey: "g1", AlertID: "inc-1", Title: "one"})
	env := newTestEnv(t, withAckStore(ack.NewMemoryStore()), withIncidentStore(ledger))

	if code, _ := env.do(t, http.MethodPost, "/api/v1/alerts/inc-1/ack", `{"acked_by":"alice"}`, nil); code != http.StatusOK {
		t.Fatalf("ack failed with status %d", code)
	}
	inc := ledger.List(&incident.Filter{AlertID: "inc-1"})[0]
	if inc.Status() != incident.StatusAcked || inc.AckedBy != "alice" {
		t.Fatalf("ack must land on the ledger, got %+v", inc)
	}
}

// TestServerWiresResolvedHook pins the daemon assembly: a server built
// with both a rule engine and an incident store connects the engine's
// recovery reports to the ledger — a fired group whose match stops
// holding closes the episode.
func TestServerWiresResolvedHook(t *testing.T) {
	ledger := incident.New(0)
	// The engine's group key is a content hash of the group-by values, so
	// derive the episode identity the same way the engine does.
	groupKey, _ := rules.RuleGroupKey([]string{"env"}, rules.NewEnv("alert", "error", "t", "b", map[string]any{"env": "prod"}))
	ledger.Open(incident.Opening{RuleID: "r-for", GroupKey: groupKey, AlertID: "a1", Title: "disk full", Channels: []string{"ghost"}})

	forDur := "1ms"
	rule := rules.Rule{
		ID:      "r-for",
		Match:   `type == "alert" && level == "error"`,
		Mode:    rules.ModeActive,
		Route:   []rules.RouteStep{{Channels: []string{"ghost"}}},
		For:     &forDur,
		GroupBy: []string{"env"},
	}
	engine := rules.NewEngine(rules.NewMemoryStore())
	if err := engine.Put(context.Background(), &rule); err != nil {
		t.Fatalf("Put rule: %v", err)
	}

	newTestEnv(t, func(c *Config) { c.Rules = engine; c.Incidents = ledger })

	ctx := context.Background()
	bad := rules.NewEnv("alert", "error", "t", "b", map[string]any{"env": "prod"})
	if _, err := engine.Evaluate(ctx, bad); err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	time.Sleep(5 * time.Millisecond)
	if _, err := engine.Evaluate(ctx, bad); err != nil {
		t.Fatalf("Evaluate fired: %v", err)
	}
	// prod recovers: the engine reports it, the wired hook closes the episode.
	ok := rules.NewEnv("alert", "info", "t", "b", map[string]any{"env": "prod"})
	if _, err := engine.Evaluate(ctx, ok); err != nil {
		t.Fatalf("Evaluate miss: %v", err)
	}

	inc := ledger.List(&incident.Filter{AlertID: "a1"})[0]
	if inc.Status() != incident.StatusResolved {
		t.Fatalf("the wired hook must resolve the episode, got %+v", inc)
	}
}
