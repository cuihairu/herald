package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/cuihairu/herald/core/roster"
	"github.com/cuihairu/herald/core/rules"
)

// failingRosterStore fails every operation with a non-ErrNotFound error,
// standing in for a store outage. It exists so the HTTP layer can be
// checked for reporting 500 instead of mistaking a backend failure for
// "not found".
type failingRosterStore struct{ err error }

func (s failingRosterStore) List(context.Context) ([]roster.Roster, error) {
	return nil, s.err
}
func (s failingRosterStore) Get(context.Context, string) (roster.Roster, error) {
	return roster.Roster{}, s.err
}
func (s failingRosterStore) Put(context.Context, roster.Roster) error { return s.err }
func (s failingRosterStore) Delete(context.Context, string) error    { return s.err }

// Manager.Delete forwards the store's error verbatim, so a store outage
// reaches the handler as an ordinary error. It must become a 500, never a
// 404: telling the caller the roster is gone would invite it to drop the
// reference and silently unsilence the schedule.
func TestHandleRosterStoreFailureIs500(t *testing.T) {
	env := newTestEnv(t, withRostersManager(roster.NewManager(
		failingRosterStore{err: errors.New("roster store offline")})))

	code, resp := env.do(t, http.MethodDelete, "/api/v1/rosters/ops", "", nil)
	if code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d (%v)", code, resp)
	}
	if resp["message"] != "roster store offline" {
		t.Errorf("the store error must reach the client, got %v", resp["message"])
	}
}

// The live table is not updated when the store write fails, so a second
// delete still hits the store and still reports 500 — the failure must not
// be cached into a misleading 404.
func TestHandleRosterStoreFailureStays500OnRetry(t *testing.T) {
	env := newTestEnv(t, withRostersManager(roster.NewManager(
		failingRosterStore{err: errors.New("roster store offline")})))

	for i := range 2 {
		code, _ := env.do(t, http.MethodDelete, "/api/v1/rosters/ops", "", nil)
		if code != http.StatusInternalServerError {
			t.Fatalf("attempt %d: expected 500, got %d", i+1, code)
		}
	}
}

// Manager.Get cannot fail with anything but ErrNotFound, so the 500 arm
// behind it is only reachable through the rostersGet seam. Injecting a
// non-ErrNotFound error pins the contract: a widened Get must surface as
// 500 with the real message, never as the adjacent 404.
func TestHandleGetRosterInternalErrorIs500(t *testing.T) {
	env := newTestEnv(t, withRostersManager(roster.NewManager(nil)))

	sentinel := errors.New("roster index corrupted")
	orig := rostersGet
	rostersGet = func(context.Context, *roster.Manager, string) (roster.Roster, error) {
		return roster.Roster{}, sentinel
	}
	defer func() { rostersGet = orig }()

	code, resp := env.do(t, http.MethodGet, "/api/v1/rosters/ops", "", nil)
	if code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d (%v)", code, resp)
	}
	if resp["message"] != "roster index corrupted" {
		t.Errorf("the lookup error must reach the client, got %v", resp["message"])
	}
}

// Same seam on the create path: a lookup failure must be a 500, not the
// 409 the success-found branch returns and not a silent "does not exist,
// go ahead" — and the roster must not be written.
func TestHandleCreateRosterLookupErrorIs500(t *testing.T) {
	env := newTestEnv(t, withRostersManager(roster.NewManager(nil)))

	sentinel := errors.New("roster index corrupted")
	orig := rostersGet
	rostersGet = func(context.Context, *roster.Manager, string) (roster.Roster, error) {
		return roster.Roster{}, sentinel
	}

	code, resp := env.do(t, http.MethodPost, "/api/v1/rosters", validRosterBody, nil)
	rostersGet = orig
	if code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d (%v)", code, resp)
	}
	if resp["message"] != "roster index corrupted" {
		t.Errorf("the lookup error must reach the client, got %v", resp["message"])
	}

	// The create aborted before Put: a normal lookup afterwards still finds
	// nothing (404), proving the store was not written behind the failure.
	code, _ = env.do(t, http.MethodGet, "/api/v1/rosters/ops", "", nil)
	if code != http.StatusNotFound {
		t.Fatalf("expected 404 after the aborted create, got %d", code)
	}
}

func withRostersManager(m *roster.Manager) func(*Config) {
	return func(c *Config) { c.Rosters = m }
}

// A pushed schedule covering the next hour: the same shape an external
// scheduler would PUT.
func coveringRosterBody(t *testing.T) string {
	t.Helper()
	now := time.Now().UTC()
	at := func(d time.Duration) string { return now.Add(d).Format(time.RFC3339) }
	return `{"id":"ops","description":"primary rotation","periods":[{"start":"` + at(-time.Minute) + `","end":"` + at(time.Hour) + `"}]}`
}

const validRosterBody = `{"id":"ops","description":"primary rotation","periods":[{"start":"2026-10-05T09:00:00Z","end":"2026-10-05T17:00:00Z"}]}`

func TestHandleRostersWithoutManager(t *testing.T) {
	env := newTestEnv(t) // no Rosters configured

	for _, tc := range []struct {
		name   string
		method string
		path   string
	}{
		{"list", http.MethodGet, "/api/v1/rosters"},
		{"create", http.MethodPost, "/api/v1/rosters"},
		{"get", http.MethodGet, "/api/v1/rosters/ops"},
		{"update", http.MethodPut, "/api/v1/rosters/ops"},
		{"delete", http.MethodDelete, "/api/v1/rosters/ops"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, resp := env.do(t, tc.method, tc.path, "{}", nil)
			if code != http.StatusServiceUnavailable {
				t.Fatalf("expected status 503, got %d", code)
			}
			if resp["message"] != "rosters manager is not configured" {
				t.Errorf("unexpected message: %v", resp["message"])
			}
		})
	}
}

func TestHandleRostersCRUD(t *testing.T) {
	t.Run("empty list", func(t *testing.T) {
		env := newTestEnv(t, withRostersManager(roster.NewManager(nil)))
		code, resp := env.do(t, http.MethodGet, "/api/v1/rosters", "", nil)
		if code != http.StatusOK {
			t.Fatalf("expected 200, got %d", code)
		}
		data := dataOf(t, resp)
		if num(t, data["count"]) != 0 {
			t.Errorf("expected count 0, got %v", data["count"])
		}
	})

	t.Run("create then list and get", func(t *testing.T) {
		env := newTestEnv(t, withRostersManager(roster.NewManager(nil)))
		code, resp := env.do(t, http.MethodPost, "/api/v1/rosters", validRosterBody, nil)
		if code != http.StatusOK {
			t.Fatalf("create: expected 200, got %d (%v)", code, resp)
		}
		created := dataOf(t, resp)
		if created["id"] != "ops" {
			t.Fatalf("created id = %v", created["id"])
		}

		code, resp = env.do(t, http.MethodGet, "/api/v1/rosters", "", nil)
		if code != http.StatusOK || num(t, dataOf(t, resp)["count"]) != 1 {
			t.Fatalf("list after create: %d", code)
		}

		code, resp = env.do(t, http.MethodGet, "/api/v1/rosters/ops", "", nil)
		if code != http.StatusOK {
			t.Fatalf("get: expected 200, got %d", code)
		}
		got := dataOf(t, resp)
		periods, ok := got["periods"].([]any)
		if !ok || len(periods) != 1 {
			t.Fatalf("expected 1 period, got %v", got["periods"])
		}
	})

	t.Run("create duplicate conflicts", func(t *testing.T) {
		env := newTestEnv(t, withRostersManager(roster.NewManager(nil)))
		env.do(t, http.MethodPost, "/api/v1/rosters", validRosterBody, nil)
		code, resp := env.do(t, http.MethodPost, "/api/v1/rosters", validRosterBody, nil)
		if code != http.StatusConflict {
			t.Fatalf("expected 409, got %d (%v)", code, resp)
		}
	})

	t.Run("create invalid roster is a 400", func(t *testing.T) {
		env := newTestEnv(t, withRostersManager(roster.NewManager(nil)))
		code, _ := env.do(t, http.MethodPost, "/api/v1/rosters", `{"id":"bad","periods":[]}`, nil)
		if code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", code)
		}
	})

	t.Run("create with malformed json is a 400", func(t *testing.T) {
		env := newTestEnv(t, withRostersManager(roster.NewManager(nil)))
		code, _ := env.do(t, http.MethodPost, "/api/v1/rosters", `{not json`, nil)
		if code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", code)
		}
	})

	t.Run("update replaces the schedule", func(t *testing.T) {
		env := newTestEnv(t, withRostersManager(roster.NewManager(nil)))
		env.do(t, http.MethodPost, "/api/v1/rosters", validRosterBody, nil)
		code, resp := env.do(t, http.MethodPut, "/api/v1/rosters/ops", coveringRosterBody(t), nil)
		if code != http.StatusOK {
			t.Fatalf("update: expected 200, got %d", code)
		}
		updated := dataOf(t, resp)
		if updated["id"] != "ops" {
			t.Fatalf("URL id must win, got %v", updated["id"])
		}
		// The replacement is live: the silence gate reads the new schedule.
		if !env.server.handler.rostersManager.Covers("ops", time.Now()) {
			t.Fatal("expected the updated schedule to cover now")
		}
	})

	t.Run("update with malformed json is a 400", func(t *testing.T) {
		env := newTestEnv(t, withRostersManager(roster.NewManager(nil)))
		env.do(t, http.MethodPost, "/api/v1/rosters", validRosterBody, nil)
		code, _ := env.do(t, http.MethodPut, "/api/v1/rosters/ops", `{not json`, nil)
		if code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", code)
		}
	})

	t.Run("update invalid is a 400", func(t *testing.T) {
		env := newTestEnv(t, withRostersManager(roster.NewManager(nil)))
		env.do(t, http.MethodPost, "/api/v1/rosters", validRosterBody, nil)
		code, _ := env.do(t, http.MethodPut, "/api/v1/rosters/ops", `{"periods":[]}`, nil)
		if code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", code)
		}
	})

	t.Run("delete removes", func(t *testing.T) {
		env := newTestEnv(t, withRostersManager(roster.NewManager(nil)))
		env.do(t, http.MethodPost, "/api/v1/rosters", validRosterBody, nil)
		code, _ := env.do(t, http.MethodDelete, "/api/v1/rosters/ops", "", nil)
		if code != http.StatusOK {
			t.Fatalf("delete: expected 200, got %d", code)
		}
		code, _ = env.do(t, http.MethodGet, "/api/v1/rosters/ops", "", nil)
		if code != http.StatusNotFound {
			t.Fatalf("get after delete: expected 404, got %d", code)
		}
	})

	t.Run("unknown roster is a 404", func(t *testing.T) {
		env := newTestEnv(t, withRostersManager(roster.NewManager(nil)))
		code, _ := env.do(t, http.MethodGet, "/api/v1/rosters/ghost", "", nil)
		if code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d", code)
		}
		code, _ = env.do(t, http.MethodDelete, "/api/v1/rosters/ghost", "", nil)
		if code != http.StatusNotFound {
			t.Fatalf("delete: expected 404, got %d", code)
		}
	})

	t.Run("method not allowed", func(t *testing.T) {
		env := newTestEnv(t, withRostersManager(roster.NewManager(nil)))
		code, _ := env.do(t, http.MethodPatch, "/api/v1/rosters", "{}", nil)
		if code != http.StatusMethodNotAllowed {
			t.Fatalf("expected 405, got %d", code)
		}
		code, _ = env.do(t, http.MethodPatch, "/api/v1/rosters/ops", "{}", nil)
		if code != http.StatusMethodNotAllowed {
			t.Fatalf("expected 405 on by-id, got %d", code)
		}
	})
}

// The mux never routes an empty {id}, so the empty-id guard is exercised
// by invoking the handler directly with an empty path value.
func TestHandleRosterByIDEmptyID(t *testing.T) {
	env := newTestEnv(t, withRostersManager(roster.NewManager(nil)))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/rosters/x", nil)
	req.SetPathValue("id", "")
	rec := httptest.NewRecorder()
	env.server.handler.HandleRosterByID(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty id, got %d", rec.Code)
	}
}

// TestServerWiresRosterSourceIntoEngine pins the NewServer wiring: a rule
// engine and roster manager passed together make the silence gate consult
// the pushed schedule, while no rosters configured fails the gate open.
func TestServerWiresRosterSourceIntoEngine(t *testing.T) {
	newEngine := func(t *testing.T) *rules.Engine {
		t.Helper()
		match := `level != "critical"`
		engine := rules.NewEngine(rules.NewMemoryStore())
		r := rules.Rule{
			ID:    "r-quiet",
			Match: `level != ""`,
			Mode:  rules.ModeActive,
			Route: []rules.RouteStep{{Channels: []string{"feishu-oncall"}}},
			Silence: &rules.SilenceSpec{
				Roster: "ops",
				Match:  &match,
			},
		}
		if err := engine.Put(context.Background(), &r); err != nil {
			t.Fatalf("Put rule: %v", err)
		}
		return engine
	}
	ctx := context.Background()
	env := rules.NewEnv("alert", "error", "t", "b", nil)

	t.Run("rosters attached: pushed schedule silences", func(t *testing.T) {
		engine := newEngine(t)
		rosters := roster.NewManager(nil)
		now := time.Now().UTC()
		r := roster.Roster{ID: "ops", Periods: []roster.Period{
			{Start: now.Add(-time.Minute), End: now.Add(time.Hour)},
		}}
		if err := rosters.Put(ctx, &r); err != nil {
			t.Fatalf("Put roster: %v", err)
		}
		// NewServer wires the manager into the engine's silence gate.
		newTestEnv(t, withRulesEngine(engine), withRostersManager(rosters))

		d, err := engine.Evaluate(ctx, env)
		if err != nil {
			t.Fatalf("Evaluate: %v", err)
		}
		if d == nil || !d.Silenced {
			t.Fatalf("expected silenced while the roster covers, got %+v", d)
		}
	})

	t.Run("no rosters configured: gate fails open", func(t *testing.T) {
		engine := newEngine(t)
		// NewServer attaches a nil source; the roster silence stays open.
		newTestEnv(t, withRulesEngine(engine))

		d, err := engine.Evaluate(ctx, env)
		if err != nil {
			t.Fatalf("Evaluate: %v", err)
		}
		if d == nil || d.Silenced || len(d.Channels) != 1 {
			t.Fatalf("missing roster data must never silence, got %+v", d)
		}
	})
}
