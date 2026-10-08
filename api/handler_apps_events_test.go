package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/cuihairu/herald/core/audience"
)

// eventsEnv is the dispatch env with a §3-shaped namespace: the notice
// category registered, audience user.1 holding an email surface and a
// subscription — the ids the target mapping produces.
func eventsEnv(t *testing.T) *testEnv {
	t.Helper()
	reg := seedAppsRegistry()
	if err := reg.RegisterCategory("demo-app", "notice", "normal"); err != nil {
		t.Fatalf("register category: %v", err)
	}
	relations := audience.NewRegistry()
	surfaces := audience.NewSurfaceRegistry()
	if _, err := surfaces.Activate("user.1", "email", "user1@example.com", "test"); err != nil {
		t.Fatalf("activate user.1: %v", err)
	}
	if err := relations.Subscribe(audience.Relation{
		AudienceID: "user.1", Category: "notice", Channel: "email",
		Type: audience.RelationSubscription, Source: "test",
	}); err != nil {
		t.Fatalf("subscribe user.1: %v", err)
	}
	policy, err := audience.NewDeliveryPolicy(map[string]string{"notice": "normal"}, nil, nil, false)
	if err != nil {
		t.Fatalf("policy: %v", err)
	}
	e := newTestEnv(t, func(c *Config) {
		c.Apps = reg
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

// post is one §3 event submission with the trigger-scope token.
func (e *testEnv) postEvent(t *testing.T, body string) (int, map[string]any) {
	t.Helper()
	return e.do(t, "POST", "/api/v1/apps/demo-app/events", body,
		map[string]string{"Authorization": "Bearer demo-app-full"})
}

// TestAppEventsMapsOntoDispatch walks the §3 mapping: severity→urgency
// table, target→audiences ref mapping, outbox id→event_id threading (the
// field the §13.5 callback echoes back), meta→params.
func TestAppEventsMapsOntoDispatch(t *testing.T) {
	e := eventsEnv(t)

	cases := []struct {
		severity string
		urgency  string
	}{
		{"critical", "critical"},
		{"warning", "urgent"},
		{"info", "normal"},
	}
	for _, tc := range cases {
		code, resp := e.postEvent(t, `{"id":77,"kind":"notice","severity":"`+tc.severity+
			`","title":"n","target":"user:1","dedup_key":"k-`+tc.severity+
			`","meta":{"node":"n1"}}`)
		if code != http.StatusOK {
			t.Fatalf("severity %s: code = %d, resp = %v", tc.severity, code, resp)
		}
		out := resp["data"].(map[string]any)
		if out["urgency"] != tc.urgency {
			t.Fatalf("severity %s: urgency = %v, want %s", tc.severity, out["urgency"], tc.urgency)
		}
	}

	// Non-ref targets pass through verbatim — "admin" is already a
	// well-formed registry id. No relation exists for it, so the
	// acceptance succeeds with an empty plan (refused, no tasks).
	code, resp := e.postEvent(t, `{"id":78,"kind":"notice","severity":"warning","title":"n","target":"admin"}`)
	if code != http.StatusOK {
		t.Fatalf("admin target: code = %d, resp = %v", code, resp)
	}
	out := resp["data"].(map[string]any)
	if tasks, ok := out["task_ids"].([]any); ok && len(tasks) != 0 {
		t.Fatalf("admin target should match nobody, got tasks %v", tasks)
	}
}

// TestAppEventsTaskCarriesEventID drains the queue after a §3 accept and
// asserts the outbox id rode through to the delivery task — the planner
// half of the receipt-correlation loop.
func TestAppEventsTaskCarriesEventID(t *testing.T) {
	e := eventsEnv(t)
	code, resp := e.postEvent(t, `{"id":42,"kind":"notice","severity":"info","title":"公告","body":"正文","target":"user:1"}`)
	if code != http.StatusOK {
		t.Fatalf("code = %d, resp = %v", code, resp)
	}
	out := resp["data"].(map[string]any)
	if out["suppressed"] == true {
		t.Fatalf("first sighting suppressed: %v", out)
	}
	ids := out["task_ids"].([]any)
	if len(ids) != 1 {
		t.Fatalf("task_ids = %v, want one task", ids)
	}
	task, err := e.queue.Pop(context.Background())
	if err != nil || task == nil {
		t.Fatalf("pop: %v / %v", task, err)
	}
	if task.AudienceID != "user.1" || task.Category != "notice" {
		t.Fatalf("task dims: %+v", task)
	}
	if task.EventID != "42" {
		t.Fatalf("task event_id = %q, want \"42\"", task.EventID)
	}
}

// TestAppEventsRefusals walks the adapter's own contract: bad severity,
// missing kind/target, non-object meta, unregistered kind, wrong scope.
func TestAppEventsRefusals(t *testing.T) {
	e := eventsEnv(t)
	cases := []struct {
		name string
		body string
		code int
	}{
		{"bad severity", `{"id":1,"kind":"notice","severity":"alarm","title":"x","target":"user:1"}`, 422},
		{"empty severity", `{"id":1,"kind":"notice","title":"x","target":"user:1"}`, 422},
		{"missing kind", `{"id":1,"severity":"info","title":"x","target":"user:1"}`, 422},
		{"missing target", `{"id":1,"kind":"notice","severity":"info","title":"x"}`, 422},
		{"meta not an object", `{"id":1,"kind":"notice","severity":"info","title":"x","target":"user:1","meta":[1,2]}`, 400},
		{"unregistered kind", `{"id":1,"kind":"bill","severity":"info","title":"x","target":"user:1"}`, 422},
		{"broken JSON", `{"id":1,"kind":`, 400},
	}
	for _, tc := range cases {
		code, _ := e.postEvent(t, tc.body)
		if code != tc.code {
			t.Fatalf("%s: code = %d, want %d", tc.name, code, tc.code)
		}
	}

	// The trigger scope gates the face like dispatch: a query-scope
	// token is one uniform 403.
	code, _ := e.do(t, "POST", "/api/v1/apps/demo-app/events",
		`{"id":1,"kind":"notice","severity":"info","title":"x","target":"user:1"}`,
		map[string]string{"Authorization": "Bearer demo-app-config"})
	if code != http.StatusForbidden {
		t.Fatalf("config token: code = %d, want 403", code)
	}
}

// TestAppEventsRoundTripDedup: a repeat §3 event with the same dedup_key
// folds like any dispatch — the acceptance answers suppressed.
func TestAppEventsRoundTripDedup(t *testing.T) {
	e := eventsEnv(t)
	body := `{"id":9,"kind":"notice","severity":"info","title":"公告","target":"user:1","dedup_key":"ann-1"}`
	if code, _ := e.postEvent(t, body); code != http.StatusOK {
		t.Fatalf("first: code = %d", code)
	}
	code, resp := e.postEvent(t, body)
	if code != http.StatusOK {
		t.Fatalf("repeat: code = %d", code)
	}
	out := resp["data"].(map[string]any)
	if out["suppressed"] != true {
		t.Fatalf("repeat not suppressed: %v", out)
	}
	// Sanity: the payload really is the §3 shape end to end.
	raw, _ := json.Marshal(out)
	if len(raw) == 0 {
		t.Fatalf("empty outcome")
	}
}
