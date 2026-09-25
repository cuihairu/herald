package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cuihairu/herald/core/groups"
)

func withGroupsManager(m *groups.Manager) func(*Config) {
	return func(c *Config) { c.Groups = m }
}

const validGroupBody = `{"id":"ops","members":[{"channel":"feishu"},{"channel":"sms","recipients":["138"]}],"description":"the oncall roster"}`

func TestHandleGroupsWithoutManager(t *testing.T) {
	env := newTestEnv(t) // no Groups configured

	for _, tc := range []struct {
		name   string
		method string
		path   string
	}{
		{"list", http.MethodGet, "/api/v1/groups"},
		{"create", http.MethodPost, "/api/v1/groups"},
		{"get", http.MethodGet, "/api/v1/groups/ops"},
		{"update", http.MethodPut, "/api/v1/groups/ops"},
		{"delete", http.MethodDelete, "/api/v1/groups/ops"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, resp := env.do(t, tc.method, tc.path, "{}", nil)
			if code != http.StatusServiceUnavailable {
				t.Fatalf("expected status 503, got %d", code)
			}
			if resp["message"] != "groups manager is not configured" {
				t.Errorf("unexpected message: %v", resp["message"])
			}
		})
	}
}

func TestHandleGroupsCRUD(t *testing.T) {
	t.Run("empty list", func(t *testing.T) {
		env := newTestEnv(t, withGroupsManager(groups.NewManager(nil)))
		code, resp := env.do(t, http.MethodGet, "/api/v1/groups", "", nil)
		if code != http.StatusOK {
			t.Fatalf("expected 200, got %d", code)
		}
		data := dataOf(t, resp)
		if num(t, data["count"]) != 0 {
			t.Errorf("expected count 0, got %v", data["count"])
		}
	})

	t.Run("create then list and get", func(t *testing.T) {
		env := newTestEnv(t, withGroupsManager(groups.NewManager(nil)))
		code, resp := env.do(t, http.MethodPost, "/api/v1/groups", validGroupBody, nil)
		if code != http.StatusOK {
			t.Fatalf("create: expected 200, got %d (%v)", code, resp)
		}
		created := dataOf(t, resp)
		if created["id"] != "ops" {
			t.Fatalf("created id = %v", created["id"])
		}

		code, resp = env.do(t, http.MethodGet, "/api/v1/groups", "", nil)
		if code != http.StatusOK || num(t, dataOf(t, resp)["count"]) != 1 {
			t.Fatalf("list after create: %d %v", code, resp)
		}

		code, resp = env.do(t, http.MethodGet, "/api/v1/groups/ops", "", nil)
		if code != http.StatusOK {
			t.Fatalf("get: expected 200, got %d", code)
		}
		got := dataOf(t, resp)
		members, ok := got["members"].([]any)
		if !ok || len(members) != 2 {
			t.Fatalf("expected 2 members, got %v", got["members"])
		}
	})

	t.Run("create duplicate conflicts", func(t *testing.T) {
		env := newTestEnv(t, withGroupsManager(groups.NewManager(nil)))
		env.do(t, http.MethodPost, "/api/v1/groups", validGroupBody, nil)
		code, resp := env.do(t, http.MethodPost, "/api/v1/groups", validGroupBody, nil)
		if code != http.StatusConflict {
			t.Fatalf("expected 409, got %d (%v)", code, resp)
		}
	})

	t.Run("create invalid group is a 400", func(t *testing.T) {
		env := newTestEnv(t, withGroupsManager(groups.NewManager(nil)))
		code, _ := env.do(t, http.MethodPost, "/api/v1/groups", `{"id":"bad","members":[]}`, nil)
		if code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", code)
		}
	})

	t.Run("create with malformed json is a 400", func(t *testing.T) {
		env := newTestEnv(t, withGroupsManager(groups.NewManager(nil)))
		code, _ := env.do(t, http.MethodPost, "/api/v1/groups", `{not json`, nil)
		if code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", code)
		}
	})

	t.Run("update replaces the roster", func(t *testing.T) {
		env := newTestEnv(t, withGroupsManager(groups.NewManager(nil)))
		env.do(t, http.MethodPost, "/api/v1/groups", validGroupBody, nil)
		code, resp := env.do(t, http.MethodPut, "/api/v1/groups/ops",
			`{"id":"ignored","members":[{"channel":"wecom"}]}`, nil)
		if code != http.StatusOK {
			t.Fatalf("update: expected 200, got %d (%v)", code, resp)
		}
		updated := dataOf(t, resp)
		if updated["id"] != "ops" {
			t.Fatalf("URL id must win, got %v", updated["id"])
		}
		// The replacement is live: the resolver serves the new roster.
		members, ok := env.server.handler.groupsManager.Resolver().ExpandGroup("ops")
		if !ok || len(members) != 1 || members[0].Channel != "wecom" {
			t.Fatalf("expected the updated roster live, got %v", members)
		}
	})

	t.Run("update with malformed json is a 400", func(t *testing.T) {
		env := newTestEnv(t, withGroupsManager(groups.NewManager(nil)))
		env.do(t, http.MethodPost, "/api/v1/groups", validGroupBody, nil)
		code, _ := env.do(t, http.MethodPut, "/api/v1/groups/ops", `{not json`, nil)
		if code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", code)
		}
	})

	t.Run("update invalid is a 400", func(t *testing.T) {
		env := newTestEnv(t, withGroupsManager(groups.NewManager(nil)))
		env.do(t, http.MethodPost, "/api/v1/groups", validGroupBody, nil)
		code, _ := env.do(t, http.MethodPut, "/api/v1/groups/ops", `{"members":[]}`, nil)
		if code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", code)
		}
	})

	t.Run("delete removes", func(t *testing.T) {
		env := newTestEnv(t, withGroupsManager(groups.NewManager(nil)))
		env.do(t, http.MethodPost, "/api/v1/groups", validGroupBody, nil)
		code, _ := env.do(t, http.MethodDelete, "/api/v1/groups/ops", "", nil)
		if code != http.StatusOK {
			t.Fatalf("delete: expected 200, got %d", code)
		}
		code, _ = env.do(t, http.MethodGet, "/api/v1/groups/ops", "", nil)
		if code != http.StatusNotFound {
			t.Fatalf("get after delete: expected 404, got %d", code)
		}
	})

	t.Run("unknown group is a 404", func(t *testing.T) {
		env := newTestEnv(t, withGroupsManager(groups.NewManager(nil)))
		code, _ := env.do(t, http.MethodGet, "/api/v1/groups/ghost", "", nil)
		if code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d", code)
		}
		code, _ = env.do(t, http.MethodDelete, "/api/v1/groups/ghost", "", nil)
		if code != http.StatusNotFound {
			t.Fatalf("delete: expected 404, got %d", code)
		}
	})

	t.Run("method not allowed", func(t *testing.T) {
		env := newTestEnv(t, withGroupsManager(groups.NewManager(nil)))
		code, _ := env.do(t, http.MethodPatch, "/api/v1/groups", "{}", nil)
		if code != http.StatusMethodNotAllowed {
			t.Fatalf("expected 405, got %d", code)
		}
		code, _ = env.do(t, http.MethodPatch, "/api/v1/groups/ops", "{}", nil)
		if code != http.StatusMethodNotAllowed {
			t.Fatalf("expected 405 on by-id, got %d", code)
		}
	})
}

// The mux never routes an empty {id}, so the empty-id guard is exercised
// by invoking the handler directly with an empty path value.
func TestHandleGroupByIDEmptyID(t *testing.T) {
	env := newTestEnv(t, withGroupsManager(groups.NewManager(nil)))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/groups/x", nil)
	req.SetPathValue("id", "")
	rec := httptest.NewRecorder()
	env.server.handler.HandleGroupByID(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty id, got %d", rec.Code)
	}
}
