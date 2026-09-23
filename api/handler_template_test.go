package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleTemplates(t *testing.T) {
	t.Run("empty list", func(t *testing.T) {
		env := newTestEnv(t)
		code, resp := env.do(t, http.MethodGet, "/api/v1/templates", "", nil)
		if code != http.StatusOK {
			t.Fatalf("expected status 200, got %d", code)
		}
		data := dataOf(t, resp)
		if num(t, data["count"]) != 0 {
			t.Errorf("expected count 0, got %v", data["count"])
		}
		templates, _ := data["templates"].([]any)
		if len(templates) != 0 {
			t.Errorf("expected 0 templates, got %d", len(templates))
		}
	})

	t.Run("list after create", func(t *testing.T) {
		env := newTestEnv(t)
		body := `{"id":"t1","name":"T1","title":"{{.a}}","fields":[{"label":"a","value":"{{.a}}"}]}`
		_, _ = env.do(t, http.MethodPost, "/api/v1/templates/create", body, nil)

		_, resp := env.do(t, http.MethodGet, "/api/v1/templates", "", nil)
		data := dataOf(t, resp)
		if num(t, data["count"]) != 1 {
			t.Fatalf("expected count 1, got %v", data["count"])
		}
		templates, _ := data["templates"].([]any)
		entry, _ := templates[0].(map[string]any)
		if entry["id"] != "t1" {
			t.Errorf("expected id t1, got %v", entry["id"])
		}
	})
}

func TestHandleCreateTemplate(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		env := newTestEnv(t)
		body := `{"id":"t1","name":"T1","title":"{{.a}}","fields":[{"label":"a","value":"{{.a}}"}]}`
		code, resp := env.do(t, http.MethodPost, "/api/v1/templates/create", body, nil)
		if code != http.StatusOK {
			t.Fatalf("expected status 200, got %d", code)
		}
		if num(t, resp["code"]) != 0 {
			t.Errorf("expected code 0, got %v", resp["code"])
		}
		data := dataOf(t, resp)
		if data["id"] != "t1" {
			t.Errorf("expected id t1, got %v", data["id"])
		}
	})

	t.Run("invalid body", func(t *testing.T) {
		env := newTestEnv(t)
		code, _ := env.do(t, http.MethodPost, "/api/v1/templates/create", "not-json", nil)
		if code != http.StatusBadRequest {
			t.Errorf("expected status 400, got %d", code)
		}
	})

	t.Run("invalid template", func(t *testing.T) {
		env := newTestEnv(t)
		code, resp := env.do(t, http.MethodPost, "/api/v1/templates/create", `{"id":"t2","title":"x"}`, nil)
		if code != http.StatusBadRequest {
			t.Fatalf("expected status 400, got %d", code)
		}
		if num(t, resp["code"]) != http.StatusBadRequest {
			t.Errorf("expected code 400, got %v", resp["code"])
		}
	})
}

func TestHandleTemplateByID(t *testing.T) {
	createBody := `{"id":"t1","name":"T1","title":"old","fields":[{"label":"a","value":"{{.a}}"}],"bindings":{"webhook":{"format":"plain"}}}`

	t.Run("get found", func(t *testing.T) {
		env := newTestEnv(t)
		_, _ = env.do(t, http.MethodPost, "/api/v1/templates/create", createBody, nil)

		code, resp := env.do(t, http.MethodGet, "/api/v1/templates/t1", "", nil)
		if code != http.StatusOK {
			t.Fatalf("expected status 200, got %d", code)
		}
		data := dataOf(t, resp)
		if data["id"] != "t1" {
			t.Errorf("expected id t1, got %v", data["id"])
		}
		if data["title"] != "old" {
			t.Errorf("expected title old, got %v", data["title"])
		}
		bindings, _ := data["bindings"].(map[string]any)
		if _, has := bindings["webhook"]; !has {
			t.Errorf("expected webhook binding, got %v", data["bindings"])
		}
	})

	t.Run("get not found", func(t *testing.T) {
		env := newTestEnv(t)
		code, _ := env.do(t, http.MethodGet, "/api/v1/templates/missing", "", nil)
		if code != http.StatusNotFound {
			t.Errorf("expected status 404, got %d", code)
		}
	})

	t.Run("update preserves bindings", func(t *testing.T) {
		env := newTestEnv(t)
		_, _ = env.do(t, http.MethodPost, "/api/v1/templates/create", createBody, nil)

		body := `{"name":"T1","title":"new","fields":[{"label":"a","value":"{{.a}}"}]}`
		code, resp := env.do(t, http.MethodPut, "/api/v1/templates/t1", body, nil)
		if code != http.StatusOK {
			t.Fatalf("expected status 200, got %d", code)
		}
		if resp["message"] != "template updated" {
			t.Errorf("expected updated message, got %v", resp["message"])
		}

		_, got := env.do(t, http.MethodGet, "/api/v1/templates/t1", "", nil)
		data := dataOf(t, got)
		if data["title"] != "new" {
			t.Errorf("expected title new, got %v", data["title"])
		}
		bindings, _ := data["bindings"].(map[string]any)
		if _, has := bindings["webhook"]; !has {
			t.Errorf("expected bindings preserved, got %v", data["bindings"])
		}
	})

	t.Run("update overrides bindings", func(t *testing.T) {
		env := newTestEnv(t)
		_, _ = env.do(t, http.MethodPost, "/api/v1/templates/create", createBody, nil)

		body := `{"name":"T1","title":"new","fields":[{"label":"a","value":"{{.a}}"}],"bindings":{"email":{"format":"html"}}}`
		code, _ := env.do(t, http.MethodPut, "/api/v1/templates/t1", body, nil)
		if code != http.StatusOK {
			t.Fatalf("expected status 200, got %d", code)
		}

		_, got := env.do(t, http.MethodGet, "/api/v1/templates/t1", "", nil)
		data := dataOf(t, got)
		bindings, _ := data["bindings"].(map[string]any)
		if _, has := bindings["email"]; !has {
			t.Errorf("expected email binding, got %v", data["bindings"])
		}
		if _, has := bindings["webhook"]; has {
			t.Errorf("expected webhook binding replaced, got %v", data["bindings"])
		}
	})

	t.Run("update invalid body", func(t *testing.T) {
		env := newTestEnv(t)
		_, _ = env.do(t, http.MethodPost, "/api/v1/templates/create", createBody, nil)

		code, _ := env.do(t, http.MethodPut, "/api/v1/templates/t1", "not-json", nil)
		if code != http.StatusBadRequest {
			t.Errorf("expected status 400, got %d", code)
		}
	})

	t.Run("update invalid template", func(t *testing.T) {
		env := newTestEnv(t)
		_, _ = env.do(t, http.MethodPost, "/api/v1/templates/create", createBody, nil)

		body := `{"name":"x","title":"y","fields":[{"label":"","value":"v"}]}`
		code, _ := env.do(t, http.MethodPut, "/api/v1/templates/t1", body, nil)
		if code != http.StatusBadRequest {
			t.Errorf("expected status 400, got %d", code)
		}
	})

	t.Run("delete", func(t *testing.T) {
		env := newTestEnv(t)
		_, _ = env.do(t, http.MethodPost, "/api/v1/templates/create", createBody, nil)

		code, resp := env.do(t, http.MethodDelete, "/api/v1/templates/t1", "", nil)
		if code != http.StatusOK {
			t.Fatalf("expected status 200, got %d", code)
		}
		if resp["message"] != "template deleted" {
			t.Errorf("expected deleted message, got %v", resp["message"])
		}

		code, _ = env.do(t, http.MethodGet, "/api/v1/templates/t1", "", nil)
		if code != http.StatusNotFound {
			t.Errorf("expected status 404 after delete, got %d", code)
		}

		code, _ = env.do(t, http.MethodDelete, "/api/v1/templates/t1", "", nil)
		if code != http.StatusNotFound {
			t.Errorf("expected status 404 for repeated delete, got %d", code)
		}
	})

	t.Run("empty id", func(t *testing.T) {
		env := newTestEnv(t)
		h := env.server.handler
		w := httptest.NewRecorder()
		h.HandleTemplateByID(w, httptest.NewRequest(http.MethodGet, "/api/v1/templates/", nil))
		if w.Code != http.StatusBadRequest {
			t.Errorf("expected status 400, got %d", w.Code)
		}
	})
}
