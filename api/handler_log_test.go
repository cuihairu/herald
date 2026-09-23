package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/cuihairu/herald/core"
)

func seedLogs(t *testing.T, env *testEnv) {
	t.Helper()

	if err := env.runtime.RegisterProvider("p1", &stubProvider{name: "p1", pType: "webhook"}); err != nil {
		t.Fatal(err)
	}
	if err := env.runtime.RegisterProvider("p2", &stubProvider{name: "p2", pType: "webhook", deliverErr: errors.New("delivery failed")}); err != nil {
		t.Fatal(err)
	}

	tasks := []*core.DeliveryTask{
		{ID: "task-1", Provider: "p1", Level: "error", CreatedAt: time.Now(), Payload: core.DeliveryPayload{Kind: core.PayloadContent}},
		{ID: "task-2", Provider: "p2", Level: "warning", CreatedAt: time.Now(), Payload: core.DeliveryPayload{Kind: core.PayloadContent}},
		{ID: "task-3", Provider: "p1", Level: "info", CreatedAt: time.Now(), Payload: core.DeliveryPayload{Kind: core.PayloadContent}},
	}
	for _, task := range tasks {
		_ = env.runtime.Deliver(context.Background(), task)
	}
}

func TestHandleLogs(t *testing.T) {
	t.Run("defaults", func(t *testing.T) {
		env := newTestEnv(t)
		seedLogs(t, env)
		code, resp := env.do(t, http.MethodGet, "/api/v1/logs", "", nil)
		if code != http.StatusOK {
			t.Fatalf("expected status 200, got %d", code)
		}
		data := dataOf(t, resp)
		if num(t, data["total"]) != 3 {
			t.Errorf("expected total 3, got %v", data["total"])
		}
		if num(t, data["limit"]) != 50 {
			t.Errorf("expected default limit 50, got %v", data["limit"])
		}
		if num(t, data["offset"]) != 0 {
			t.Errorf("expected offset 0, got %v", data["offset"])
		}
		logs, _ := data["logs"].([]any)
		if len(logs) != 3 {
			t.Errorf("expected 3 logs, got %d", len(logs))
		}
	})

	t.Run("clamps limit", func(t *testing.T) {
		env := newTestEnv(t)
		seedLogs(t, env)
		_, resp := env.do(t, http.MethodGet, "/api/v1/logs?limit=1000", "", nil)
		data := dataOf(t, resp)
		if num(t, data["limit"]) != 500 {
			t.Errorf("expected limit 500, got %v", data["limit"])
		}
		_, resp = env.do(t, http.MethodGet, "/api/v1/logs?limit=-5", "", nil)
		data = dataOf(t, resp)
		if num(t, data["limit"]) != 50 {
			t.Errorf("expected limit 50, got %v", data["limit"])
		}
	})

	t.Run("offset beyond total", func(t *testing.T) {
		env := newTestEnv(t)
		seedLogs(t, env)
		_, resp := env.do(t, http.MethodGet, "/api/v1/logs?offset=10", "", nil)
		data := dataOf(t, resp)
		if num(t, data["total"]) != 3 {
			t.Errorf("expected total 3, got %v", data["total"])
		}
		logs, _ := data["logs"].([]any)
		if len(logs) != 0 {
			t.Errorf("expected 0 logs, got %d", len(logs))
		}
	})

	t.Run("filter by status", func(t *testing.T) {
		env := newTestEnv(t)
		seedLogs(t, env)
		_, resp := env.do(t, http.MethodGet, "/api/v1/logs?status=failed", "", nil)
		data := dataOf(t, resp)
		if num(t, data["total"]) != 1 {
			t.Fatalf("expected total 1, got %v", data["total"])
		}
		logs, _ := data["logs"].([]any)
		entry, _ := logs[0].(map[string]any)
		if entry["id"] != "task-2" {
			t.Errorf("expected task-2, got %v", entry["id"])
		}
	})

	t.Run("filter by provider", func(t *testing.T) {
		env := newTestEnv(t)
		seedLogs(t, env)
		_, resp := env.do(t, http.MethodGet, "/api/v1/logs?provider=p1", "", nil)
		data := dataOf(t, resp)
		if num(t, data["total"]) != 2 {
			t.Errorf("expected total 2, got %v", data["total"])
		}
	})

	t.Run("filter by level", func(t *testing.T) {
		env := newTestEnv(t)
		seedLogs(t, env)
		_, resp := env.do(t, http.MethodGet, "/api/v1/logs?level=info", "", nil)
		data := dataOf(t, resp)
		if num(t, data["total"]) != 1 {
			t.Errorf("expected total 1, got %v", data["total"])
		}
	})

	t.Run("since and until matching", func(t *testing.T) {
		env := newTestEnv(t)
		seedLogs(t, env)
		since := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
		until := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
		_, resp := env.do(t, http.MethodGet, "/api/v1/logs?since="+since+"&until="+until, "", nil)
		data := dataOf(t, resp)
		if num(t, data["total"]) != 3 {
			t.Errorf("expected total 3, got %v", data["total"])
		}
	})

	t.Run("since in future excludes all", func(t *testing.T) {
		env := newTestEnv(t)
		seedLogs(t, env)
		since := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
		_, resp := env.do(t, http.MethodGet, "/api/v1/logs?since="+since, "", nil)
		data := dataOf(t, resp)
		if num(t, data["total"]) != 0 {
			t.Errorf("expected total 0, got %v", data["total"])
		}
	})

	t.Run("invalid since ignored", func(t *testing.T) {
		env := newTestEnv(t)
		seedLogs(t, env)
		_, resp := env.do(t, http.MethodGet, "/api/v1/logs?since=not-a-time&until=also-bad", "", nil)
		data := dataOf(t, resp)
		if num(t, data["total"]) != 3 {
			t.Errorf("expected total 3, got %v", data["total"])
		}
	})
}

func TestHandleLogsStats(t *testing.T) {
	env := newTestEnv(t)
	seedLogs(t, env)

	code, resp := env.do(t, http.MethodGet, "/api/v1/logs/stats", "", nil)
	if code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", code)
	}
	data := dataOf(t, resp)
	if num(t, data["total"]) != 3 {
		t.Errorf("expected total 3, got %v", data["total"])
	}
	byStatus, _ := data["by_status"].(map[string]any)
	if num(t, byStatus["success"]) != 2 {
		t.Errorf("expected 2 success, got %v", byStatus["success"])
	}
	if num(t, byStatus["failed"]) != 1 {
		t.Errorf("expected 1 failed, got %v", byStatus["failed"])
	}
	byProvider, _ := data["by_provider"].(map[string]any)
	if num(t, byProvider["p1"]) != 2 {
		t.Errorf("expected 2 for p1, got %v", byProvider["p1"])
	}
}

func TestHandleLogByID(t *testing.T) {
	t.Run("found", func(t *testing.T) {
		env := newTestEnv(t)
		seedLogs(t, env)
		code, resp := env.do(t, http.MethodGet, "/api/v1/logs/task-1", "", nil)
		if code != http.StatusOK {
			t.Fatalf("expected status 200, got %d", code)
		}
		data := dataOf(t, resp)
		if data["id"] != "task-1" {
			t.Errorf("expected id task-1, got %v", data["id"])
		}
		if data["status"] != "success" {
			t.Errorf("expected status success, got %v", data["status"])
		}
	})

	t.Run("found failed log", func(t *testing.T) {
		env := newTestEnv(t)
		seedLogs(t, env)
		_, resp := env.do(t, http.MethodGet, "/api/v1/logs/task-2", "", nil)
		data := dataOf(t, resp)
		if data["status"] != "failed" {
			t.Errorf("expected status failed, got %v", data["status"])
		}
	})

	t.Run("not found", func(t *testing.T) {
		env := newTestEnv(t)
		code, resp := env.do(t, http.MethodGet, "/api/v1/logs/missing", "", nil)
		if code != http.StatusNotFound {
			t.Fatalf("expected status 404, got %d", code)
		}
		if num(t, resp["code"]) != http.StatusNotFound {
			t.Errorf("expected code 404, got %v", resp["code"])
		}
	})

	t.Run("empty id", func(t *testing.T) {
		env := newTestEnv(t)
		w := httptest.NewRecorder()
		env.server.handler.HandleLogByID(w, httptest.NewRequest(http.MethodGet, "/api/v1/logs/", nil))
		if w.Code != http.StatusBadRequest {
			t.Errorf("expected status 400, got %d", w.Code)
		}
	})
}
