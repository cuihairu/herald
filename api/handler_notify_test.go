package api

import (
	"net/http"
	"testing"
	"time"

	"github.com/cuihairu/herald/core/dedup"
	"github.com/cuihairu/herald/core/template"
)

func TestHandleNotify(t *testing.T) {
	t.Run("invalid body", func(t *testing.T) {
		env := newTestEnv(t)
		code, resp := env.do(t, http.MethodPost, "/api/v1/notify", "not-json", nil)
		if code != http.StatusBadRequest {
			t.Fatalf("expected status 400, got %d", code)
		}
		if num(t, resp["code"]) != http.StatusBadRequest {
			t.Errorf("expected code 400, got %v", resp["code"])
		}
	})

	t.Run("success with direct content", func(t *testing.T) {
		env := newTestEnv(t)
		if err := env.runtime.RegisterProvider("p1", &stubProvider{name: "p1", pType: "webhook"}); err != nil {
			t.Fatal(err)
		}
		body := `{"type":"alert","level":"high","channels":["p1"],"title":"Title","body":"Body"}`
		code, resp := env.do(t, http.MethodPost, "/api/v1/notify", body, nil)
		if code != http.StatusOK {
			t.Fatalf("expected status 200, got %d", code)
		}
		if num(t, resp["code"]) != 0 {
			t.Fatalf("expected code 0, got %v", resp["code"])
		}
		data := dataOf(t, resp)
		if data["notification_id"] == "" || data["notification_id"] == nil {
			t.Error("expected notification id")
		}
		accepted, _ := data["accepted"].([]any)
		if len(accepted) != 1 || accepted[0] != "p1" {
			t.Errorf("expected accepted [p1], got %v", data["accepted"])
		}
		taskIDs, _ := data["task_ids"].([]any)
		if len(taskIDs) != 1 {
			t.Errorf("expected 1 task id, got %v", data["task_ids"])
		}
		if _, hasFailed := data["failed"]; hasFailed {
			t.Errorf("expected no failed field, got %v", data["failed"])
		}
		if env.queue.Size() != 1 {
			t.Errorf("expected queue size 1, got %d", env.queue.Size())
		}
	})

	t.Run("partial failure", func(t *testing.T) {
		env := newTestEnv(t)
		if err := env.runtime.RegisterProvider("p1", &stubProvider{name: "p1", pType: "webhook"}); err != nil {
			t.Fatal(err)
		}
		body := `{"type":"alert","channels":["p1","missing"],"title":"T","body":"B"}`
		_, resp := env.do(t, http.MethodPost, "/api/v1/notify", body, nil)
		if num(t, resp["code"]) != 0 {
			t.Fatalf("expected code 0, got %v", resp["code"])
		}
		data := dataOf(t, resp)
		accepted, _ := data["accepted"].([]any)
		if len(accepted) != 1 {
			t.Errorf("expected 1 accepted, got %v", data["accepted"])
		}
		failed, _ := data["failed"].([]any)
		if len(failed) != 1 {
			t.Errorf("expected 1 failed, got %v", data["failed"])
		}
	})

	t.Run("all channels failed", func(t *testing.T) {
		env := newTestEnv(t)
		body := `{"type":"alert","channels":["missing"],"title":"T","body":"B"}`
		_, resp := env.do(t, http.MethodPost, "/api/v1/notify", body, nil)
		if num(t, resp["code"]) != 422 {
			t.Fatalf("expected code 422, got %v", resp["code"])
		}
		data := dataOf(t, resp)
		if data["notification_id"] == nil || data["notification_id"] == "" {
			t.Error("expected notification id in failure response")
		}
		failed, _ := data["failed"].([]any)
		if len(failed) != 1 {
			t.Errorf("expected 1 failed, got %v", data["failed"])
		}
	})

	t.Run("no route returns error without result", func(t *testing.T) {
		env := newTestEnv(t)
		body := `{"type":"unroutable","title":"T","body":"B"}`
		_, resp := env.do(t, http.MethodPost, "/api/v1/notify", body, nil)
		if num(t, resp["code"]) != 422 {
			t.Fatalf("expected code 422, got %v", resp["code"])
		}
		data := dataOf(t, resp)
		if _, has := data["notification_id"]; has {
			t.Error("expected no notification id when result is nil")
		}
		accepted, _ := data["accepted"].([]any)
		if len(accepted) != 0 {
			t.Errorf("expected empty accepted, got %v", data["accepted"])
		}
	})

	t.Run("template based notify", func(t *testing.T) {
		env := newTestEnv(t)
		if err := env.runtime.RegisterProvider("p1", &stubProvider{name: "p1", pType: "webhook"}); err != nil {
			t.Fatal(err)
		}
		err := env.templates.Register(&template.Template{
			ID:     "t1",
			Name:   "T1",
			Title:  "{{.a}}",
			Fields: []template.Field{{Label: "a", Value: "{{.a}}"}},
		})
		if err != nil {
			t.Fatal(err)
		}
		body := `{"channels":["p1"],"template":"t1","params":{"a":"x"}}`
		_, resp := env.do(t, http.MethodPost, "/api/v1/notify", body, nil)
		if num(t, resp["code"]) != 0 {
			t.Fatalf("expected code 0, got %v", resp["code"])
		}
		data := dataOf(t, resp)
		accepted, _ := data["accepted"].([]any)
		if len(accepted) != 1 {
			t.Errorf("expected 1 accepted, got %v", data["accepted"])
		}
	})

	t.Run("unknown template", func(t *testing.T) {
		env := newTestEnv(t)
		body := `{"channels":["p1"],"template":"missing"}`
		_, resp := env.do(t, http.MethodPost, "/api/v1/notify", body, nil)
		if num(t, resp["code"]) != 422 {
			t.Fatalf("expected code 422, got %v", resp["code"])
		}
		data := dataOf(t, resp)
		if _, has := data["notification_id"]; has {
			t.Error("expected no notification id when template render fails")
		}
	})

	t.Run("dedup suppresses duplicate", func(t *testing.T) {
		env := newTestEnv(t, func(c *Config) {
			c.Dedup = dedup.NewDedup(&dedup.Config{Window: time.Minute})
		})
		if err := env.runtime.RegisterProvider("p1", &stubProvider{name: "p1", pType: "webhook"}); err != nil {
			t.Fatal(err)
		}
		body := `{"type":"alert","channels":["p1"],"title":"T","body":"B"}`

		_, first := env.do(t, http.MethodPost, "/api/v1/notify", body, nil)
		if num(t, first["code"]) != 0 {
			t.Fatalf("expected code 0, got %v", first["code"])
		}
		firstData := dataOf(t, first)
		taskIDs, _ := firstData["task_ids"].([]any)
		if len(taskIDs) != 1 {
			t.Fatalf("expected 1 task id on first request, got %v", firstData["task_ids"])
		}

		_, second := env.do(t, http.MethodPost, "/api/v1/notify", body, nil)
		if num(t, second["code"]) != 0 {
			t.Fatalf("expected code 0, got %v", second["code"])
		}
		secondData := dataOf(t, second)
		taskIDs, _ = secondData["task_ids"].([]any)
		if len(taskIDs) != 0 {
			t.Errorf("expected 0 task ids on deduplicated request, got %v", secondData["task_ids"])
		}
		if env.queue.Size() != 1 {
			t.Errorf("expected queue size 1, got %d", env.queue.Size())
		}
	})
}

func TestHandleStatus(t *testing.T) {
	t.Run("empty runtime", func(t *testing.T) {
		env := newTestEnv(t)
		code, resp := env.do(t, http.MethodGet, "/api/v1/status", "", nil)
		if code != http.StatusOK {
			t.Fatalf("expected status 200, got %d", code)
		}
		data := dataOf(t, resp)
		if data["status"] != "running" {
			t.Errorf("expected status running, got %v", data["status"])
		}
		providers, _ := data["providers"].([]any)
		if len(providers) != 0 {
			t.Errorf("expected 0 providers, got %d", len(providers))
		}
	})

	t.Run("with providers", func(t *testing.T) {
		env := newTestEnv(t)
		if err := env.runtime.RegisterProvider("p1", &stubProvider{name: "p1", pType: "webhook"}); err != nil {
			t.Fatal(err)
		}
		_, resp := env.do(t, http.MethodGet, "/api/v1/status", "", nil)
		data := dataOf(t, resp)
		providers, _ := data["providers"].([]any)
		if len(providers) != 1 {
			t.Fatalf("expected 1 provider, got %d", len(providers))
		}
	})
}
