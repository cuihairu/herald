package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cuihairu/herald/core"
	"github.com/cuihairu/herald/core/auth"
	"github.com/cuihairu/herald/core/dedup"
	"github.com/cuihairu/herald/core/queue"
	"github.com/cuihairu/herald/core/route"
	"github.com/cuihairu/herald/core/runtime"
	"github.com/cuihairu/herald/core/template"
	"github.com/cuihairu/herald/core/worker"
)

type stubProvider struct {
	name       string
	pType      string
	deliverErr error
}

func (p *stubProvider) Deliver(ctx context.Context, task *core.DeliveryTask) error {
	return p.deliverErr
}

func (p *stubProvider) Name() string { return p.name }

func (p *stubProvider) Type() string { return p.pType }

func (p *stubProvider) Status() *core.ProviderStatus {
	return &core.ProviderStatus{Name: p.name, Type: p.pType, Status: "available"}
}

type configProvider struct {
	stubProvider
	config map[string]interface{}
}

func (p *configProvider) GetConfig() map[string]interface{} { return p.config }

type stubFactory struct {
	name     string
	err      error
	provider core.Provider
}

func (f *stubFactory) Name() string { return f.name }

func (f *stubFactory) Type() string { return f.name }

func (f *stubFactory) Create(config map[string]interface{}) (core.Provider, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.provider != nil {
		return f.provider, nil
	}
	return &configProvider{
		stubProvider: stubProvider{name: f.name, pType: f.name},
		config:       config,
	}, nil
}

type testEnv struct {
	server    *Server
	ts        *httptest.Server
	runtime   *runtime.Manager
	queue     core.Queue
	templates *template.Manager
	registry  *worker.Registry
	router    *route.Router
	auth      *auth.Auth
	dedup     *dedup.Dedup
}

func newTestEnv(t *testing.T, opts ...func(*Config)) *testEnv {
	t.Helper()

	q, err := queue.NewMemoryQueue(&queue.QueueConfig{})
	if err != nil {
		t.Fatalf("failed to create queue: %v", err)
	}

	cfg := &Config{
		Addr:            "127.0.0.1:0",
		Router:          route.NewRouter(&route.Config{}),
		Queue:           q,
		Runtime:         runtime.NewManager(100),
		Dedup:           dedup.NewDedup(&dedup.Config{}),
		Auth:            auth.New(&auth.Config{}),
		TemplateManager: template.NewManager(),
		WorkerRegistry:  worker.NewRegistry(),
	}
	for _, opt := range opts {
		opt(cfg)
	}

	s := NewServer(cfg)
	ts := httptest.NewServer(s.server.Handler)
	t.Cleanup(ts.Close)

	return &testEnv{
		server:    s,
		ts:        ts,
		runtime:   cfg.Runtime,
		queue:     cfg.Queue,
		templates: cfg.TemplateManager,
		registry:  cfg.WorkerRegistry,
		router:    cfg.Router,
		auth:      cfg.Auth,
		dedup:     cfg.Dedup,
	}
}

func (e *testEnv) do(t *testing.T, method, path, body string, headers map[string]string) (int, map[string]any) {
	t.Helper()

	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, e.ts.URL+path, reader)
	if err != nil {
		t.Fatalf("failed to build request: %v", err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response: %v", err)
	}

	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	return resp.StatusCode, out
}

func dataOf(t *testing.T, resp map[string]any) map[string]any {
	t.Helper()
	d, ok := resp["data"].(map[string]any)
	if !ok {
		t.Fatalf("expected object data, got %T (%v)", resp["data"], resp["data"])
	}
	return d
}

func num(t *testing.T, v any) int {
	t.Helper()
	f, ok := v.(float64)
	if !ok {
		t.Fatalf("expected number, got %T (%v)", v, v)
	}
	return int(f)
}

func decodeJSON(t *testing.T, raw string) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("failed to decode response %q: %v", raw, err)
	}
	return out
}
