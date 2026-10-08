package sdk

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cuihairu/herald/api"
	"github.com/cuihairu/herald/core"
	"github.com/cuihairu/herald/core/apps"
	"github.com/cuihairu/herald/core/audience"
	"github.com/cuihairu/herald/core/audit"
	"github.com/cuihairu/herald/core/auth"
	"github.com/cuihairu/herald/core/dedup"
	"github.com/cuihairu/herald/core/queue"
	"github.com/cuihairu/herald/core/route"
	"github.com/cuihairu/herald/core/runtime"
	"github.com/cuihairu/herald/core/template"
)

// stubProvider delivers nothing and fails nothing — the flow proof does
// not care about the wire, only that the pipeline accepts the task.
type stubProvider struct{}

func (stubProvider) Deliver(ctx context.Context, task *core.DeliveryTask) error { return nil }
func (stubProvider) Name() string                                               { return "email" }
func (stubProvider) Type() string                                               { return "email" }
func (stubProvider) Status() *core.ProviderStatus {
	return &core.ProviderStatus{Name: "email", Type: "email", Status: "available"}
}

// newServer assembles the real api server the way heraldd does, seeded
// with one demo-app app holding a full-scope token.
func newServer(t *testing.T) (*httptest.Server, core.Queue, *runtime.Manager) {
	t.Helper()
	reg, err := apps.NewRegistry([]apps.SeedApp{{
		Name:   "demo-app",
		Tokens: []apps.SeedToken{{Secret: "demo-app-full", Scopes: []string{"config", "trigger", "query"}}},
	}})
	if err != nil {
		t.Fatalf("seed apps: %v", err)
	}
	relations := audience.NewRegistry()
	surfaces := audience.NewSurfaceRegistry()
	if _, err := surfaces.Activate("alice", "email", "alice@example.com", "test"); err != nil {
		t.Fatalf("activate: %v", err)
	}
	if err := relations.Subscribe(audience.Relation{
		AudienceID: "alice", Category: "alerts", Channel: "email",
		Type: audience.RelationSubscription, Source: "test",
	}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	policy, err := audience.NewDeliveryPolicy(map[string]string{"alerts": "urgent"}, nil, nil, false)
	if err != nil {
		t.Fatalf("policy: %v", err)
	}
	q, err := queue.NewMemoryQueue(&queue.QueueConfig{})
	if err != nil {
		t.Fatalf("queue: %v", err)
	}
	t.Cleanup(func() { _ = q.Close() })
	mgr := runtime.NewManager(100)
	if err := mgr.RegisterProvider("email", stubProvider{}, true); err != nil {
		t.Fatalf("provider: %v", err)
	}
	srv := api.NewServer(&api.Config{
		Addr:            "127.0.0.1:0",
		Router:          route.NewRouter(&route.Config{}),
		Queue:           q,
		Runtime:         mgr,
		Dedup:           dedup.NewDedup(&dedup.Config{}),
		Auth:            auth.New(&auth.Config{}),
		TemplateManager: template.NewManager(),
		Apps:            reg,
		Delivery:        policy,
		Filter:          audience.NewFilter(relations, surfaces),
		FeedRelations:   relations,
		FeedSurfaces:    surfaces,
		Audit:           audit.New(0),
	})
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts, q, mgr
}

// TestClientFullFlow walks §13.5's 全流程 against the real server:
// 配品类 → 策略/模板/回调 → 触发 → 查状态.
func TestClientFullFlow(t *testing.T) {
	ts, q, mgr := newServer(t)
	c := New(ts.URL, "demo-app", "demo-app-full")
	ctx := context.Background()

	// 配品类.
	if err := c.RegisterCategory(ctx, "alerts", "urgent"); err != nil {
		t.Fatalf("register category: %v", err)
	}
	cats, err := c.Categories(ctx)
	if err != nil || len(cats) != 1 || cats[0].Name != "alerts" || cats[0].DefaultUrgency != "urgent" {
		t.Fatalf("categories: %v / %+v", err, cats)
	}
	if err := c.RegisterCategory(ctx, "alerts", "hourly"); err == nil {
		t.Fatalf("bad urgency: want refusal")
	}
	var httpErr *Error
	if asErr(c.RegisterCategory(ctx, "alerts", "hourly"), &httpErr); httpErr.Status != 422 {
		t.Fatalf("bad urgency: want a 422 *Error, got %#v", httpErr)
	}

	// 策略与回调.
	if err := c.PutPolicy(ctx, "intensity", map[string]string{"sms": "L4"}); err != nil {
		t.Fatalf("put policy: %v", err)
	}
	if err := c.SetCallback(ctx, "https://app.example.com/hook", "sdk-callback-secret-32"); err != nil {
		t.Fatalf("set callback: %v", err)
	}
	cb, err := c.Callback(ctx)
	if err != nil || cb.URL != "https://app.example.com/hook" || !cb.HasSecret {
		t.Fatalf("callback: %v / %+v", err, cb)
	}

	// 模板.
	if err := c.RegisterTemplate(ctx, map[string]any{
		"id": "node_down", "name": "节点下线", "title": "节点 {{.node}} 下线", "level": "error",
	}); err != nil {
		t.Fatalf("register template: %v", err)
	}
	tmpls, err := c.Templates(ctx)
	if err != nil || len(tmpls) != 1 || tmpls[0].ID != "node_down" || tmpls[0].Title != "节点 {{.node}} 下线" {
		t.Fatalf("templates: %v / %+v", err, tmpls)
	}

	// 触发: the namespace template renders, the urgent plan accepts, and
	// one task rides out to alice's email.
	out, err := c.Dispatch(ctx, DispatchRequest{
		Category:  "alerts",
		Template:  "node_down",
		Params:    map[string]any{"node": "node-17"},
		Audiences: []string{"alice"},
		DedupKey:  "k1",
	})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if out.Suppressed || out.Urgency != "urgent" || len(out.TaskIDs) != 1 {
		t.Fatalf("dispatch outcome: %+v", out)
	}
	if rep, err := c.Dispatch(ctx, DispatchRequest{
		Category: "alerts", Template: "node_down",
		Params: map[string]any{"node": "node-17"}, Audiences: []string{"alice"}, DedupKey: "k1",
	}); err != nil || !rep.Suppressed {
		t.Fatalf("repeat dispatch: %v / %+v", err, rep)
	}

	// 查状态: the settled row answers who/what/which source.
	for q.Size() > 0 {
		task, err := q.Pop(context.Background())
		if err != nil || task == nil {
			break
		}
		if err := mgr.Deliver(context.Background(), task); err != nil {
			t.Fatalf("deliver: %v", err)
		}
		_ = q.Ack(context.Background(), task.ID)
	}
	page, err := c.Deliveries(ctx, DeliveriesQuery{})
	if err != nil || page.Total != 1 {
		t.Fatalf("deliveries: %v / %+v", err, page)
	}
	row := page.Logs[0]
	if row.AudienceID != "alice" || row.Category != "alerts" || row.Source != "app:demo-app" || row.Status != "success" {
		t.Fatalf("delivery row: %+v", row)
	}
	narrow, err := c.Deliveries(ctx, DeliveriesQuery{Audience: "bob"})
	if err != nil || narrow.Total != 0 {
		t.Fatalf("narrowed deliveries: %v / %+v", err, narrow)
	}

	// Audit: the repeat fold is on the trail.
	events, err := c.Audit(ctx, time.Time{})
	if err != nil || len(events) != 1 {
		t.Fatalf("audit: %v / %+v", err, events)
	}
	if events[0].Kind != "delivery.deduped" || events[0].Source != "app:demo-app" {
		t.Fatalf("audit event: %+v", events[0])
	}
	future, err := c.Audit(ctx, time.Now().Add(time.Hour))
	if err != nil || len(future) != 0 {
		t.Fatalf("audit since future: %v / %d", err, len(future))
	}

	// Relations.
	rels, err := c.Relations(ctx, "alice")
	if err != nil || len(rels) != 1 || rels[0].Type != "subscription" || rels[0].Channel != "email" {
		t.Fatalf("relations: %v / %+v", err, rels)
	}
	if !rels[0].Policy.AllowUnsubscribe || rels[0].Policy.MustDeliver {
		t.Fatalf("relation policy: %+v", rels[0].Policy)
	}

	// Template removal.
	if err := c.DeleteTemplate(ctx, "node_down"); err != nil {
		t.Fatalf("delete template: %v", err)
	}
	tmpls, _ = c.Templates(ctx)
	if len(tmpls) != 0 {
		t.Fatalf("templates after delete: %+v", tmpls)
	}
}

func asErr(err error, target **Error) bool {
	if e, ok := err.(*Error); ok {
		*target = e
		return true
	}
	*target = nil
	return false
}

// TestClientAuthError: a wrong token answers the uniform 401 *Error.
func TestClientAuthError(t *testing.T) {
	ts, _, _ := newServer(t)
	c := New(ts.URL, "demo-app", "wrong-token")
	err := c.RegisterCategory(context.Background(), "alerts", "urgent")
	var httpErr *Error
	if !asErr(err, &httpErr) || httpErr.Status != http.StatusUnauthorized {
		t.Fatalf("want 401 *Error, got %#v", err)
	}
	if httpErr.Error() != "herald: 401: invalid app credentials" {
		t.Fatalf("Error() renders status and message: %q", httpErr.Error())
	}
}

// TestClientTransportError: an unreachable server surfaces as a wrapped
// transport error, not an *Error — on every face, including the
// admin-gated relations read that never answers 401 to an app token.
func TestClientTransportError(t *testing.T) {
	c := New("http://127.0.0.1:1", "demo-app", "tok")
	if _, err := c.Categories(context.Background()); err == nil || !IsTransport(err) {
		t.Fatalf("want transport error, got %#v", err)
	}
	if _, err := c.Relations(context.Background(), "alice"); err == nil || !IsTransport(err) {
		t.Fatalf("relations: want transport error, got %#v", err)
	}
}

// TestClientEveryFaceRefuses: one unauthorized round trip per face —
// every method's error path runs, and every answer decodes as *Error.
func TestClientEveryFaceRefuses(t *testing.T) {
	ts, _, _ := newServer(t)
	c := New(ts.URL, "demo-app", "wrong-token")
	ctx := context.Background()
	checks := map[string]func() error{
		"RegisterCategory": func() error { return c.RegisterCategory(ctx, "alerts", "urgent") },
		"Categories":       func() error { _, err := c.Categories(ctx); return err },
		"PutPolicy":        func() error { return c.PutPolicy(ctx, "intensity", map[string]string{"sms": "L4"}) },
		"SetCallback":      func() error { return c.SetCallback(ctx, "https://x.example.com/h", "callback-secret-32bytes") },
		"Callback":         func() error { _, err := c.Callback(ctx); return err },
		"Dispatch": func() error {
			_, err := c.Dispatch(ctx, DispatchRequest{Category: "alerts", Audiences: []string{"alice"}})
			return err
		},
		"Deliveries":       func() error { _, err := c.Deliveries(ctx, DeliveriesQuery{}); return err },
		"Audit":            func() error { _, err := c.Audit(ctx, time.Time{}); return err },
		"RegisterTemplate": func() error { return c.RegisterTemplate(ctx, map[string]any{"id": "x", "name": "x", "title": "x"}) },
		"Templates":        func() error { _, err := c.Templates(ctx); return err },
		"DeleteTemplate":   func() error { return c.DeleteTemplate(ctx, "x") },
	}
	for name, call := range checks {
		err := call()
		var httpErr *Error
		if !asErr(err, &httpErr) || httpErr.Status != http.StatusUnauthorized {
			t.Fatalf("%s: want 401 *Error, got %#v", name, err)
		}
	}
}

// TestClientMalformedAnswers: the client surfaces broken responses as
// decode errors, and a 2xx envelope with a non-zero code is an *Error.
func TestClientMalformedAnswers(t *testing.T) {
	junk := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("<html>not json</html>"))
	}))
	defer junk.Close()
	var httpErr *Error
	_, err := New(junk.URL, "a", "t").Categories(context.Background())
	if err == nil || asErr(err, &httpErr) || !strings.Contains(err.Error(), "decode response") {
		t.Fatalf("junk body: want decode error, got %#v", err)
	}

	codeOnly := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":7,"message":"no"}`))
	}))
	defer codeOnly.Close()
	_, err = New(codeOnly.URL, "a", "t").Categories(context.Background())
	if !asErr(err, &httpErr) || httpErr.Status != http.StatusOK || httpErr.Message != "no" {
		t.Fatalf("non-zero envelope: want 200 *Error, got %#v", err)
	}

	badData := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":"not-an-array"}`))
	}))
	defer badData.Close()
	_, err = New(badData.URL, "a", "t").Categories(context.Background())
	if err == nil || asErr(err, &httpErr) || !strings.Contains(err.Error(), "decode data") {
		t.Fatalf("bad data: want decode error, got %#v", err)
	}
}

// TestClientRequestBuildErrors: an unencodable payload and an
// unbuildable URL refuse before any bytes hit the wire.
func TestClientRequestBuildErrors(t *testing.T) {
	var httpErr *Error
	c := New("http://127.0.0.1:1", "a", "t")
	err := c.PutPolicy(context.Background(), "intensity", map[string]any{"x": make(chan int)})
	if err == nil || asErr(err, &httpErr) || !strings.Contains(err.Error(), "encode request") {
		t.Fatalf("encode: want local error, got %#v", err)
	}
	bad := New("ht tp://bad url", "a", "t")
	_, err = bad.Categories(context.Background())
	if err == nil || asErr(err, &httpErr) || !strings.Contains(err.Error(), "build request") {
		t.Fatalf("build: want local error, got %#v", err)
	}
}
