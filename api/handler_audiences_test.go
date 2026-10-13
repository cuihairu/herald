package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cuihairu/herald/core/audience"
)

// failingAudienceStore fails every operation with a non-ErrNotFound error,
// standing in for a store outage. It exists so the HTTP layer can be
// checked for reporting 500 instead of mistaking a backend failure for
// "not found".
type failingAudienceStore struct{ err error }

func (s failingAudienceStore) List(context.Context) ([]audience.Entry, error) {
	return nil, s.err
}
func (s failingAudienceStore) Put(context.Context, audience.Entry) error { return s.err }
func (s failingAudienceStore) Delete(context.Context, string) error      { return s.err }

// newAudienceManager builds a manager over one feishu recipient, the
// backing table every valid write path references.
func newAudienceManager(t *testing.T, store audience.Store) *audience.Manager {
	t.Helper()
	m, err := audience.NewManager(store, nil, map[string]audience.Recipient{
		"alice": {Endpoints: []audience.Endpoint{{Type: "feishu", Target: "@alice"}}},
		"bob":   {Endpoints: []audience.Endpoint{{Type: "email", Target: "bob@x.io"}}},
	})
	if err != nil {
		t.Fatalf("audience.NewManager: %v", err)
	}
	return m
}

func withAudiencesManager(m *audience.Manager) func(*Config) {
	return func(c *Config) { c.Users = m }
}

const validAudienceBody = `{"id":"ops","recipients":["alice","bob"]}`

// Manager.Delete forwards the store's error verbatim, so a store outage
// reaches the handler as an ordinary error. It must become a 500, never a
// 404: telling the caller the audience is gone would invite it to drop the
// "user:" reference and silently stop resolving it.
func TestHandleAudienceStoreFailureIs500(t *testing.T) {
	env := newTestEnv(t, withAudiencesManager(newAudienceManager(t,
		failingAudienceStore{err: errors.New("audience store offline")})))

	code, resp := env.do(t, http.MethodDelete, "/api/v1/audiences/ops", "", nil)
	if code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d (%v)", code, resp)
	}
	if resp["message"] != "audience store offline" {
		t.Errorf("the store error must reach the client, got %v", resp["message"])
	}
}

// The live table is not updated when the store write fails, so a second
// delete still hits the store and still reports 500 — the failure must not
// be cached into a misleading 404.
func TestHandleAudienceStoreFailureStays500OnRetry(t *testing.T) {
	env := newTestEnv(t, withAudiencesManager(newAudienceManager(t,
		failingAudienceStore{err: errors.New("audience store offline")})))

	for i := range 2 {
		code, _ := env.do(t, http.MethodDelete, "/api/v1/audiences/ops", "", nil)
		if code != http.StatusInternalServerError {
			t.Fatalf("attempt %d: expected 500, got %d", i+1, code)
		}
	}
}

// Manager.Get cannot fail with anything but ErrNotFound, so the 500 arm
// behind it is only reachable through the audiencesGet seam. Injecting a
// non-ErrNotFound error pins the contract: a widened Get must surface as
// 500 with the real message, never as the adjacent 404.
func TestHandleGetAudienceInternalErrorIs500(t *testing.T) {
	env := newTestEnv(t, withAudiencesManager(newAudienceManager(t, nil)))

	sentinel := errors.New("audience index corrupted")
	orig := audiencesGet
	audiencesGet = func(context.Context, *audience.Manager, string) (audience.Audience, error) {
		return audience.Audience{}, sentinel
	}
	defer func() { audiencesGet = orig }()

	code, resp := env.do(t, http.MethodGet, "/api/v1/audiences/ops", "", nil)
	if code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d (%v)", code, resp)
	}
	if resp["message"] != "audience index corrupted" {
		t.Errorf("the lookup error must reach the client, got %v", resp["message"])
	}
}

// Same seam on the create path: a lookup failure must be a 500, not the
// 409 the success-found branch returns and not a silent "does not exist,
// go ahead" — and the audience must not be written.
func TestHandleCreateAudienceLookupErrorIs500(t *testing.T) {
	env := newTestEnv(t, withAudiencesManager(newAudienceManager(t, nil)))

	sentinel := errors.New("audience index corrupted")
	orig := audiencesGet
	audiencesGet = func(context.Context, *audience.Manager, string) (audience.Audience, error) {
		return audience.Audience{}, sentinel
	}

	code, resp := env.do(t, http.MethodPost, "/api/v1/audiences", validAudienceBody, nil)
	audiencesGet = orig
	if code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d (%v)", code, resp)
	}
	if resp["message"] != "audience index corrupted" {
		t.Errorf("the lookup error must reach the client, got %v", resp["message"])
	}

	// The create aborted before Put: a normal lookup afterwards still finds
	// nothing (404), proving the store was not written behind the failure.
	code, _ = env.do(t, http.MethodGet, "/api/v1/audiences/ops", "", nil)
	if code != http.StatusNotFound {
		t.Fatalf("expected 404 after the aborted create, got %d", code)
	}
}

func TestHandleAudiencesWithoutManager(t *testing.T) {
	env := newTestEnv(t) // no Users configured

	for _, tc := range []struct {
		name   string
		method string
		path   string
	}{
		{"list", http.MethodGet, "/api/v1/audiences"},
		{"create", http.MethodPost, "/api/v1/audiences"},
		{"get", http.MethodGet, "/api/v1/audiences/ops"},
		{"update", http.MethodPut, "/api/v1/audiences/ops"},
		{"delete", http.MethodDelete, "/api/v1/audiences/ops"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, resp := env.do(t, tc.method, tc.path, "{}", nil)
			if code != http.StatusServiceUnavailable {
				t.Fatalf("expected status 503, got %d", code)
			}
			if resp["message"] != "audiences manager is not configured" {
				t.Errorf("unexpected message: %v", resp["message"])
			}
		})
	}
}

func TestHandleAudiencesCRUD(t *testing.T) {
	t.Run("empty list", func(t *testing.T) {
		env := newTestEnv(t, withAudiencesManager(newAudienceManager(t, nil)))
		code, resp := env.do(t, http.MethodGet, "/api/v1/audiences", "", nil)
		if code != http.StatusOK {
			t.Fatalf("expected 200, got %d", code)
		}
		data := dataOf(t, resp)
		if num(t, data["count"]) != 0 {
			t.Errorf("expected count 0, got %v", data["count"])
		}
	})

	t.Run("create then list and get", func(t *testing.T) {
		env := newTestEnv(t, withAudiencesManager(newAudienceManager(t, nil)))
		code, resp := env.do(t, http.MethodPost, "/api/v1/audiences", validAudienceBody, nil)
		if code != http.StatusOK {
			t.Fatalf("create: expected 200, got %d (%v)", code, resp)
		}
		created := dataOf(t, resp)
		if created["id"] != "ops" {
			t.Fatalf("created id = %v", created["id"])
		}

		code, resp = env.do(t, http.MethodGet, "/api/v1/audiences", "", nil)
		if code != http.StatusOK || num(t, dataOf(t, resp)["count"]) != 1 {
			t.Fatalf("list after create: %d %v", code, resp)
		}

		code, resp = env.do(t, http.MethodGet, "/api/v1/audiences/ops", "", nil)
		if code != http.StatusOK {
			t.Fatalf("get: expected 200, got %d", code)
		}
		got := dataOf(t, resp)
		recipients, ok := got["recipients"].([]any)
		if !ok || len(recipients) != 2 {
			t.Fatalf("expected 2 recipients, got %v", got["recipients"])
		}
	})

	t.Run("create duplicate conflicts", func(t *testing.T) {
		env := newTestEnv(t, withAudiencesManager(newAudienceManager(t, nil)))
		env.do(t, http.MethodPost, "/api/v1/audiences", validAudienceBody, nil)
		code, resp := env.do(t, http.MethodPost, "/api/v1/audiences", validAudienceBody, nil)
		if code != http.StatusConflict {
			t.Fatalf("expected 409, got %d (%v)", code, resp)
		}
	})

	t.Run("create invalid audience is a 400", func(t *testing.T) {
		env := newTestEnv(t, withAudiencesManager(newAudienceManager(t, nil)))
		code, _ := env.do(t, http.MethodPost, "/api/v1/audiences", `{"id":"bad id!","recipients":["alice"]}`, nil)
		if code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", code)
		}
	})

	t.Run("create with malformed json is a 400", func(t *testing.T) {
		env := newTestEnv(t, withAudiencesManager(newAudienceManager(t, nil)))
		code, _ := env.do(t, http.MethodPost, "/api/v1/audiences", `{not json`, nil)
		if code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", code)
		}
	})

	t.Run("update replaces the roster", func(t *testing.T) {
		env := newTestEnv(t, withAudiencesManager(newAudienceManager(t, nil)))
		env.do(t, http.MethodPost, "/api/v1/audiences", validAudienceBody, nil)
		code, resp := env.do(t, http.MethodPut, "/api/v1/audiences/ops",
			`{"id":"ignored","recipients":["bob"]}`, nil)
		if code != http.StatusOK {
			t.Fatalf("update: expected 200, got %d (%v)", code, resp)
		}
		updated := dataOf(t, resp)
		if updated["id"] != "ops" {
			t.Fatalf("URL id must win, got %v", updated["id"])
		}
		// The replacement is live: the resolver serves the new roster.
		eps, ok := env.server.handler.audiencesManager.ExpandUser("ops")
		if !ok || len(eps) != 1 || eps[0].Type != "email" {
			t.Fatalf("expected the updated audience live, got %v", eps)
		}
	})

	t.Run("update with malformed json is a 400", func(t *testing.T) {
		env := newTestEnv(t, withAudiencesManager(newAudienceManager(t, nil)))
		env.do(t, http.MethodPost, "/api/v1/audiences", validAudienceBody, nil)
		code, _ := env.do(t, http.MethodPut, "/api/v1/audiences/ops", `{not json`, nil)
		if code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", code)
		}
	})

	t.Run("update invalid is a 400", func(t *testing.T) {
		env := newTestEnv(t, withAudiencesManager(newAudienceManager(t, nil)))
		env.do(t, http.MethodPost, "/api/v1/audiences", validAudienceBody, nil)
		code, _ := env.do(t, http.MethodPut, "/api/v1/audiences/ops", `{"recipients":["ghost"]}`, nil)
		if code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", code)
		}
	})

	t.Run("delete removes", func(t *testing.T) {
		env := newTestEnv(t, withAudiencesManager(newAudienceManager(t, nil)))
		env.do(t, http.MethodPost, "/api/v1/audiences", validAudienceBody, nil)
		code, _ := env.do(t, http.MethodDelete, "/api/v1/audiences/ops", "", nil)
		if code != http.StatusOK {
			t.Fatalf("delete: expected 200, got %d", code)
		}
		code, _ = env.do(t, http.MethodGet, "/api/v1/audiences/ops", "", nil)
		if code != http.StatusNotFound {
			t.Fatalf("get after delete: expected 404, got %d", code)
		}
	})

	t.Run("unknown audience is a 404", func(t *testing.T) {
		env := newTestEnv(t, withAudiencesManager(newAudienceManager(t, nil)))
		code, _ := env.do(t, http.MethodGet, "/api/v1/audiences/ghost", "", nil)
		if code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d", code)
		}
		code, _ = env.do(t, http.MethodDelete, "/api/v1/audiences/ghost", "", nil)
		if code != http.StatusNotFound {
			t.Fatalf("delete: expected 404, got %d", code)
		}
	})

	t.Run("method not allowed", func(t *testing.T) {
		env := newTestEnv(t, withAudiencesManager(newAudienceManager(t, nil)))
		code, _ := env.do(t, http.MethodPatch, "/api/v1/audiences", "{}", nil)
		if code != http.StatusMethodNotAllowed {
			t.Fatalf("expected 405, got %d", code)
		}
		code, _ = env.do(t, http.MethodPatch, "/api/v1/audiences/ops", "{}", nil)
		if code != http.StatusMethodNotAllowed {
			t.Fatalf("expected 405 on by-id, got %d", code)
		}
	})
}

// The mux never routes an empty {id}, so the empty-id guard is exercised
// by invoking the handler directly with an empty path value.
func TestHandleAudienceByIDEmptyID(t *testing.T) {
	env := newTestEnv(t, withAudiencesManager(newAudienceManager(t, nil)))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/audiences/x", nil)
	req.SetPathValue("id", "")
	rec := httptest.NewRecorder()
	env.server.handler.HandleAudienceByID(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty id, got %d", rec.Code)
	}
}
