package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cuihairu/herald/core"
	"github.com/cuihairu/herald/core/worker"
)

func TestHandleWorkers(t *testing.T) {
	t.Run("nil registry returns empty list", func(t *testing.T) {
		h := NewHandler(nil, nil, nil)
		w := httptest.NewRecorder()
		h.HandleWorkers(w, httptest.NewRequest(http.MethodGet, "/api/v1/workers", nil))

		if w.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d", w.Code)
		}
		resp := dataOf(t, decodeJSON(t, w.Body.String()))
		workers, _ := resp["workers"].([]any)
		if len(workers) != 0 {
			t.Errorf("expected 0 workers, got %d", len(workers))
		}
	})

	t.Run("lists registered workers", func(t *testing.T) {
		env := newTestEnv(t)
		if err := env.registry.Register(&worker.Info{ID: "w1", Mode: worker.Local, Capabilities: []string{"email"}}); err != nil {
			t.Fatal(err)
		}
		if err := env.registry.Register(&worker.Info{ID: "w2", Mode: worker.Remote, Capabilities: []string{"sms"}}); err != nil {
			t.Fatal(err)
		}

		code, resp := env.do(t, http.MethodGet, "/api/v1/workers", "", nil)
		if code != http.StatusOK {
			t.Fatalf("expected status 200, got %d", code)
		}
		data := dataOf(t, resp)
		if num(t, data["count"]) != 2 {
			t.Fatalf("expected count 2, got %v", data["count"])
		}
		workers, _ := data["workers"].([]any)
		if len(workers) != 2 {
			t.Fatalf("expected 2 workers, got %d", len(workers))
		}

		found := map[string]bool{}
		for _, item := range workers {
			entry, _ := item.(map[string]any)
			found[entry["worker_id"].(string)] = true
			if entry["status"] != "online" {
				t.Errorf("expected status online, got %v", entry["status"])
			}
		}
		if !found["w1"] || !found["w2"] {
			t.Errorf("expected w1 and w2 in response, got %v", found)
		}
	})
}

func TestHandleQueue(t *testing.T) {
	env := newTestEnv(t)
	if err := env.queue.Push(context.Background(), &core.DeliveryTask{ID: "t1", Provider: "p1"}); err != nil {
		t.Fatal(err)
	}
	if err := env.queue.Push(context.Background(), &core.DeliveryTask{ID: "t2", Provider: "p1"}); err != nil {
		t.Fatal(err)
	}

	code, resp := env.do(t, http.MethodGet, "/api/v1/queue", "", nil)
	if code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", code)
	}
	data := dataOf(t, resp)
	if num(t, data["size"]) != 2 {
		t.Errorf("expected size 2, got %v", data["size"])
	}
}
