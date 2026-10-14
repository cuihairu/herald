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

func seedDeliveries(t *testing.T, env *testEnv) {
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

func TestHandleDeliveries(t *testing.T) {
	t.Run("defaults", func(t *testing.T) {
		env := newTestEnv(t)
		seedDeliveries(t, env)
		code, resp := env.do(t, http.MethodGet, "/api/v1/deliveries", "", nil)
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
		rows, _ := data["deliveries"].([]any)
		if len(rows) != 3 {
			t.Errorf("expected 3 deliveries, got %d", len(rows))
		}
	})

	t.Run("lifecycle vocabulary not event outcomes", func(t *testing.T) {
		env := newTestEnv(t)
		seedDeliveries(t, env)
		_, resp := env.do(t, http.MethodGet, "/api/v1/deliveries?status=delivered", "", nil)
		data := dataOf(t, resp)
		if num(t, data["total"]) != 2 {
			t.Errorf("expected 2 delivered, got %v", data["total"])
		}
		_, resp = env.do(t, http.MethodGet, "/api/v1/deliveries?status=failed", "", nil)
		data = dataOf(t, resp)
		if num(t, data["total"]) != 1 {
			t.Errorf("expected 1 failed, got %v", data["total"])
		}
	})

	t.Run("clamps limit", func(t *testing.T) {
		env := newTestEnv(t)
		seedDeliveries(t, env)
		_, resp := env.do(t, http.MethodGet, "/api/v1/deliveries?limit=1000", "", nil)
		data := dataOf(t, resp)
		if num(t, data["limit"]) != 500 {
			t.Errorf("expected limit 500, got %v", data["limit"])
		}
		_, resp = env.do(t, http.MethodGet, "/api/v1/deliveries?limit=-5", "", nil)
		data = dataOf(t, resp)
		if num(t, data["limit"]) != 50 {
			t.Errorf("expected limit 50, got %v", data["limit"])
		}
	})

	t.Run("offset beyond total", func(t *testing.T) {
		env := newTestEnv(t)
		seedDeliveries(t, env)
		_, resp := env.do(t, http.MethodGet, "/api/v1/deliveries?offset=10", "", nil)
		data := dataOf(t, resp)
		if num(t, data["total"]) != 3 {
			t.Errorf("expected total 3, got %v", data["total"])
		}
		rows, _ := data["deliveries"].([]any)
		if len(rows) != 0 {
			t.Errorf("expected 0 deliveries, got %d", len(rows))
		}
	})

	t.Run("negative offset clamps to first page", func(t *testing.T) {
		env := newTestEnv(t)
		seedDeliveries(t, env)
		_, resp := env.do(t, http.MethodGet, "/api/v1/deliveries?offset=-3", "", nil)
		data := dataOf(t, resp)
		rows, _ := data["deliveries"].([]any)
		if len(rows) != 3 {
			t.Errorf("expected negative offset to clamp, got %d rows", len(rows))
		}
	})

	t.Run("filter by provider", func(t *testing.T) {
		env := newTestEnv(t)
		seedDeliveries(t, env)
		_, resp := env.do(t, http.MethodGet, "/api/v1/deliveries?provider=p1", "", nil)
		data := dataOf(t, resp)
		if num(t, data["total"]) != 2 {
			t.Errorf("expected total 2, got %v", data["total"])
		}
	})

	t.Run("filter by level", func(t *testing.T) {
		env := newTestEnv(t)
		seedDeliveries(t, env)
		_, resp := env.do(t, http.MethodGet, "/api/v1/deliveries?level=info", "", nil)
		data := dataOf(t, resp)
		if num(t, data["total"]) != 1 {
			t.Errorf("expected total 1, got %v", data["total"])
		}
	})

	t.Run("since and until matching", func(t *testing.T) {
		env := newTestEnv(t)
		seedDeliveries(t, env)
		since := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
		until := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
		_, resp := env.do(t, http.MethodGet, "/api/v1/deliveries?since="+since+"&until="+until, "", nil)
		data := dataOf(t, resp)
		if num(t, data["total"]) != 3 {
			t.Errorf("expected total 3, got %v", data["total"])
		}
	})

	t.Run("since in future excludes all", func(t *testing.T) {
		env := newTestEnv(t)
		seedDeliveries(t, env)
		since := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
		_, resp := env.do(t, http.MethodGet, "/api/v1/deliveries?since="+since, "", nil)
		data := dataOf(t, resp)
		if num(t, data["total"]) != 0 {
			t.Errorf("expected total 0, got %v", data["total"])
		}
	})

	t.Run("until in past excludes all", func(t *testing.T) {
		env := newTestEnv(t)
		seedDeliveries(t, env)
		until := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
		_, resp := env.do(t, http.MethodGet, "/api/v1/deliveries?until="+until, "", nil)
		data := dataOf(t, resp)
		if num(t, data["total"]) != 0 {
			t.Errorf("expected total 0, got %v", data["total"])
		}
	})

	t.Run("invalid since and until ignored", func(t *testing.T) {
		env := newTestEnv(t)
		seedDeliveries(t, env)
		_, resp := env.do(t, http.MethodGet, "/api/v1/deliveries?since=not-a-time&until=also-bad", "", nil)
		data := dataOf(t, resp)
		if num(t, data["total"]) != 3 {
			t.Errorf("expected total 3, got %v", data["total"])
		}
	})
}

func TestHandleDeliveriesStats(t *testing.T) {
	env := newTestEnv(t)
	seedDeliveries(t, env)

	code, resp := env.do(t, http.MethodGet, "/api/v1/deliveries/stats", "", nil)
	if code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", code)
	}
	data := dataOf(t, resp)
	if num(t, data["total"]) != 3 {
		t.Errorf("expected total 3, got %v", data["total"])
	}
	byStatus, _ := data["by_status"].(map[string]any)
	if num(t, byStatus["delivered"]) != 2 {
		t.Errorf("expected 2 delivered, got %v", byStatus["delivered"])
	}
	if num(t, byStatus["failed"]) != 1 {
		t.Errorf("expected 1 failed, got %v", byStatus["failed"])
	}
	byProvider, _ := data["by_provider"].(map[string]any)
	if num(t, byProvider["p1"]) != 2 {
		t.Errorf("expected 2 for p1, got %v", byProvider["p1"])
	}
}

func TestHandleDeliveryByID(t *testing.T) {
	t.Run("found", func(t *testing.T) {
		env := newTestEnv(t)
		seedDeliveries(t, env)
		code, resp := env.do(t, http.MethodGet, "/api/v1/deliveries/task-1", "", nil)
		if code != http.StatusOK {
			t.Fatalf("expected status 200, got %d", code)
		}
		data := dataOf(t, resp)
		if data["id"] != "task-1" {
			t.Errorf("expected id task-1, got %v", data["id"])
		}
		if data["status"] != "delivered" {
			t.Errorf("expected lifecycle status delivered, got %v", data["status"])
		}
	})

	t.Run("found failed delivery", func(t *testing.T) {
		env := newTestEnv(t)
		seedDeliveries(t, env)
		_, resp := env.do(t, http.MethodGet, "/api/v1/deliveries/task-2", "", nil)
		data := dataOf(t, resp)
		if data["status"] != "failed" {
			t.Errorf("expected lifecycle status failed, got %v", data["status"])
		}
		if data["last_error"] != "delivery failed" {
			t.Errorf("expected last_error, got %v", data["last_error"])
		}
	})

	t.Run("not found", func(t *testing.T) {
		env := newTestEnv(t)
		code, resp := env.do(t, http.MethodGet, "/api/v1/deliveries/missing", "", nil)
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
		env.server.handler.HandleDeliveryByID(w, httptest.NewRequest(http.MethodGet, "/api/v1/deliveries/", nil))
		if w.Code != http.StatusBadRequest {
			t.Errorf("expected status 400, got %d", w.Code)
		}
	})
}
