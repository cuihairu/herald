package api

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/cuihairu/herald/core"
	"github.com/cuihairu/herald/core/worker"
)

func seedOverviewLogs(t *testing.T, env *testEnv) {
	t.Helper()

	if err := env.runtime.RegisterProvider("p1", &stubProvider{name: "p1", pType: "webhook"}); err != nil {
		t.Fatal(err)
	}
	if err := env.runtime.RegisterProvider("p2", &stubProvider{name: "p2", pType: "webhook", deliverErr: errors.New("delivery failed")}); err != nil {
		t.Fatal(err)
	}
	if err := env.runtime.RegisterProvider("p3", &stubProvider{name: "p3", pType: "webhook"}, false); err != nil {
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

func overviewData(t *testing.T, env *testEnv) map[string]any {
	t.Helper()
	code, resp := env.do(t, http.MethodGet, "/api/v1/overview", "", nil)
	if code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", code)
	}
	return dataOf(t, resp)
}

func subObject(t *testing.T, parent map[string]any, key string) map[string]any {
	t.Helper()
	v, ok := parent[key].(map[string]any)
	if !ok {
		t.Fatalf("expected object under %q, got %T (%v)", key, parent[key], parent[key])
	}
	return v
}

func TestOverviewReturnsZeroesOnAnIdleServer(t *testing.T) {
	env := newTestEnv(t)
	data := overviewData(t, env)

	providers := subObject(t, data, "providers")
	if num(t, providers["enabled"]) != 0 || num(t, providers["total"]) != 0 {
		t.Errorf("unexpected providers on idle server: %v", providers)
	}
	if num(t, subObject(t, data, "queue")["depth"]) != 0 {
		t.Errorf("unexpected queue depth on idle server: %v", data["queue"])
	}
	delivery := subObject(t, data, "delivery")
	if num(t, delivery["total"]) != 0 || num(t, delivery["success"]) != 0 || num(t, delivery["failed"]) != 0 {
		t.Errorf("unexpected delivery stats on idle server: %v", delivery)
	}
	workers := subObject(t, data, "workers")
	if num(t, workers["online"]) != 0 || num(t, workers["total"]) != 0 {
		t.Errorf("unexpected workers on idle server: %v", workers)
	}
}

func TestOverviewAggregatesAllStatusSignals(t *testing.T) {
	env := newTestEnv(t)
	seedOverviewLogs(t, env)

	if err := env.registry.Register(&worker.Info{ID: "w1", Mode: worker.Local}); err != nil {
		t.Fatal(err)
	}

	task := &core.DeliveryTask{ID: "q-1", Provider: "p1", Payload: core.DeliveryPayload{Kind: core.PayloadContent}}
	if err := env.queue.Push(context.Background(), task); err != nil {
		t.Fatal(err)
	}

	data := overviewData(t, env)

	providers := subObject(t, data, "providers")
	if got := num(t, providers["enabled"]); got != 2 {
		t.Errorf("expected 2 enabled providers, got %d", got)
	}
	if got := num(t, providers["total"]); got != 3 {
		t.Errorf("expected 3 providers total, got %d", got)
	}

	if got := num(t, subObject(t, data, "queue")["depth"]); got != 1 {
		t.Errorf("expected queue depth 1, got %d", got)
	}

	delivery := subObject(t, data, "delivery")
	if got := num(t, delivery["total"]); got != 3 {
		t.Errorf("expected 3 deliveries in window, got %d", got)
	}
	if got := num(t, delivery["success"]); got != 2 {
		t.Errorf("expected 2 successes in window, got %d", got)
	}
	if got := num(t, delivery["failed"]); got != 1 {
		t.Errorf("expected 1 failure in window, got %d", got)
	}
	rate, ok := delivery["success_rate"].(float64)
	if !ok || rate < 66.0 || rate > 67.0 {
		t.Errorf("expected success rate ~66.7, got %v", delivery["success_rate"])
	}

	workers := subObject(t, data, "workers")
	if got := num(t, workers["online"]); got != 1 {
		t.Errorf("expected 1 online worker, got %d", got)
	}
	if got := num(t, workers["total"]); got != 1 {
		t.Errorf("expected 1 worker total, got %d", got)
	}
}

func TestOverviewDeliveryWindowExcludesOlderThan24h(t *testing.T) {
	env := newTestEnv(t)
	seedOverviewLogs(t, env)

	restore := overviewNow
	overviewNow = func() time.Time { return time.Now().Add(25 * time.Hour) }
	defer func() { overviewNow = restore }()

	delivery := subObject(t, overviewData(t, env), "delivery")
	if got := num(t, delivery["total"]); got != 0 {
		t.Errorf("expected the 24h window to exclude all seeded logs, got total %d", got)
	}
}

func TestOverviewRejectsNonGET(t *testing.T) {
	env := newTestEnv(t)
	code, _ := env.do(t, http.MethodPost, "/api/v1/overview", "", nil)
	if code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", code)
	}
}

func TestOverviewToleratesNilWorkerRegistryAndQueue(t *testing.T) {
	env := newTestEnv(t, func(cfg *Config) {
		cfg.WorkerRegistry = nil
		cfg.Queue = nil
	})
	data := overviewData(t, env)
	workers := subObject(t, data, "workers")
	if num(t, workers["online"]) != 0 || num(t, workers["total"]) != 0 {
		t.Errorf("expected zeroed workers without a registry, got %v", workers)
	}
	if num(t, subObject(t, data, "queue")["depth"]) != 0 {
		t.Errorf("expected depth 0 without a queue, got %v", data["queue"])
	}
}
