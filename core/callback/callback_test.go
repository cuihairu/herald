package callback

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cuihairu/herald/core"
	"github.com/cuihairu/herald/core/apps"
	"github.com/cuihairu/herald/core/audience"
	"github.com/cuihairu/herald/core/queue"
	"github.com/cuihairu/herald/core/runtime"
)

const testSecret = "callback-test-secret-32"

type boomError struct{}

func (*boomError) Error() string { return "boom" }

var errBoom error = &boomError{}

func emitterEnv(t *testing.T) (*Emitter, *apps.Registry, core.Queue) {
	t.Helper()
	reg, err := apps.NewRegistry([]apps.SeedApp{{
		Name:   "ferry",
		Tokens: []apps.SeedToken{{Secret: "tok", Scopes: []string{"config"}}},
	}})
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	if err := reg.SetCallback("ferry", "https://callback.example.com/hook", testSecret); err != nil {
		t.Fatalf("set callback: %v", err)
	}
	q, err := queue.NewMemoryQueue(&queue.QueueConfig{})
	if err != nil {
		t.Fatalf("queue: %v", err)
	}
	t.Cleanup(func() { _ = q.Close() })
	return NewEmitter(reg, q), reg, q
}

func popAll(t *testing.T, q core.Queue) []*core.DeliveryTask {
	t.Helper()
	tasks := []*core.DeliveryTask{}
	for q.Size() > 0 {
		task, err := q.Pop(context.Background())
		if err != nil || task == nil {
			break
		}
		tasks = append(tasks, task)
	}
	return tasks
}

// TestOnSettleEmitsSignedDeliveryResult: every settled app-sourced task
// becomes a signed delivery_result event on the queue, addressed to the
// app's registered URL — success and failure alike.
func TestOnSettleEmitsSignedDeliveryResult(t *testing.T) {
	e, _, q := emitterEnv(t)

	e.OnSettle(&core.DeliveryTask{
		ID: "t1", Provider: "email", Source: "app:ferry",
		AudienceID: "alice", Category: "alerts",
	}, nil)
	e.OnSettle(&core.DeliveryTask{
		ID: "t2", Provider: "sms", Source: "app:ferry",
		AudienceID: "bob", Category: "alerts", LastError: "boom",
	}, errBoom)

	tasks := popAll(t, q)
	if len(tasks) != 2 {
		t.Fatalf("queue: want 2 callback tasks, got %d", len(tasks))
	}
	seen := map[string]DeliveryResult{}
	for _, task := range tasks {
		if task.Provider != ProviderName {
			t.Fatalf("task provider: want %s, got %q", ProviderName, task.Provider)
		}
		// No source stamp: the §13.4 delivery query must not count the
		// callback plumbing as one of the app's dispatch rows, and an
		// empty source keeps the settle sink from re-emitting it.
		if task.Source != "" || !strings.HasPrefix(task.ID, "cb-") {
			t.Fatalf("task dims: source=%q id=%q", task.Source, task.ID)
		}
		if task.Targets[0] != "https://callback.example.com/hook" {
			t.Fatalf("task target: want the registered url, got %v", task.Targets)
		}
		// The signature must verify over the exact body with the app's
		// secret — this is the contract the receiving app implements.
		body := task.Payload.Raw["body"].(string)
		sig := task.Payload.Raw["signature"].(string)
		mac := hmac.New(sha256.New, []byte(testSecret))
		mac.Write([]byte(body))
		if sig != signaturePrefix+hex.EncodeToString(mac.Sum(nil)) {
			t.Fatalf("signature mismatch: %q", sig)
		}
		var ev Event
		if err := json.Unmarshal([]byte(body), &ev); err != nil {
			t.Fatalf("event body: %v", err)
		}
		if ev.App != "ferry" || ev.Kind != KindDeliveryResult || ev.EventID == "" || ev.At.IsZero() {
			t.Fatalf("event envelope: %+v", ev)
		}
		if ev.Delivery == nil {
			t.Fatalf("delivery payload missing: %+v", ev)
		}
		seen[ev.Delivery.Status] = *ev.Delivery
	}
	if got := seen["success"]; got.TaskID != "t1" || got.AudienceID != "alice" || got.Channel != "email" || got.Error != "" {
		t.Fatalf("success event: %+v", got)
	}
	if got := seen["failed"]; got.TaskID != "t2" || got.Error != "boom" {
		t.Fatalf("failed event: %+v", got)
	}
}

// TestOnSettleIgnoresNonAppTraffic: anonymous /notify traffic carries no
// source stamp, nil tasks are nothing to report, and a sink without a
// queue has nowhere to put anything.
func TestOnSettleIgnoresNonAppTraffic(t *testing.T) {
	e, _, q := emitterEnv(t)

	e.OnSettle(&core.DeliveryTask{ID: "t1", Provider: "email"}, nil)
	e.OnSettle(nil, nil)
	NewEmitter(nil, nil).OnSettle(&core.DeliveryTask{Source: "app:ferry"}, nil)
	if len(popAll(t, q)) != 0 {
		t.Fatalf("non-app settle must not enqueue")
	}
}

// TestUnsubscribedBackflow: a relation ending whose entry source belongs
// to an app namespace flows back to that app; other entries' and closed
// faces enqueue nothing.
func TestUnsubscribedBackflow(t *testing.T) {
	e, reg, q := emitterEnv(t)

	e.Unsubscribed(audience.Relation{
		AudienceID: "alice", Category: "alerts", Channel: "email",
		Type: audience.RelationSubscription, Source: "app:ferry",
	}, "preference_center")
	e.Unsubscribed(audience.Relation{
		AudienceID: "alice", Category: "alerts", Channel: "email",
		Type: audience.RelationSubscription, Source: "bot",
	}, "bot")

	tasks := popAll(t, q)
	if len(tasks) != 1 {
		t.Fatalf("queue: want only the app-sourced unsubscribe, got %d", len(tasks))
	}
	var ev Event
	if err := json.Unmarshal([]byte(tasks[0].Payload.Raw["body"].(string)), &ev); err != nil {
		t.Fatalf("event body: %v", err)
	}
	if ev.Kind != KindUnsubscribe || ev.Unsub == nil ||
		ev.Unsub.AudienceID != "alice" || ev.Unsub.Category != "alerts" ||
		ev.Unsub.Channel != "email" || ev.Unsub.RelationType != "subscription" ||
		ev.Unsub.Actor != "preference_center" {
		t.Fatalf("unsubscribe event: %+v", ev)
	}

	// An app without callback config is a closed face: nothing enqueues.
	if err := reg.ClearCallback("ferry"); err != nil {
		t.Fatalf("clear: %v", err)
	}
	e.Unsubscribed(audience.Relation{
		AudienceID: "alice", Category: "alerts", Channel: "email",
		Type: audience.RelationSubscription, Source: "app:ferry",
	}, "preference_center")
	if len(popAll(t, q)) != 0 {
		t.Fatalf("closed face must not enqueue")
	}

	// A dead queue swallows the push error — the hooks discard it by
	// contract, the direct callers read it. The callback config is
	// re-armed first so the push really reaches the queue.
	if err := reg.SetCallback("ferry", "https://callback.example.com/hook", testSecret); err != nil {
		t.Fatalf("re-arm: %v", err)
	}
	if err := q.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	e.Unsubscribed(audience.Relation{
		AudienceID: "alice", Category: "alerts", Channel: "email",
		Type: audience.RelationSubscription, Source: "app:ferry",
	}, "preference_center")
	NewEmitter(reg, nil).Unsubscribed(audience.Relation{
		AudienceID: "alice", Category: "alerts", Channel: "email",
		Type: audience.RelationSubscription, Source: "app:ferry",
	}, "preference_center")
}

// TestProviderDeliver posts through a real HTTP endpoint: headers carry
// the event id and the signature, the body is the signed bytes verbatim,
// a non-2xx answer and a malformed task both read as delivery failures.
func TestProviderDeliver(t *testing.T) {
	var body []byte
	var sig, eventID string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ = io.ReadAll(r.Body)
		sig = r.Header.Get(SignatureHeader)
		eventID = r.Header.Get(EventIDHeader)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	p := NewProvider()
	if p.Name() != ProviderName || p.Type() != ProviderName || p.Status() == nil {
		t.Fatalf("provider identity: %q/%q", p.Name(), p.Type())
	}
	task := &core.DeliveryTask{
		ID:      "cb-x",
		Targets: []string{srv.URL},
		Payload: core.DeliveryPayload{Kind: core.PayloadRaw, Raw: map[string]any{
			"event_id":  "evt-1",
			"body":      `{"event_id":"evt-1"}`,
			"signature": signaturePrefix + "abc",
		}},
	}
	if err := p.Deliver(context.Background(), task); err != nil {
		t.Fatalf("deliver: %v", err)
	}
	if string(body) != `{"event_id":"evt-1"}` || sig != signaturePrefix+"abc" || eventID != "evt-1" {
		t.Fatalf("delivered request: body=%q sig=%q id=%q", body, sig, eventID)
	}

	// A non-2xx answer is a delivery failure — the retry budget lives in
	// the pipeline, not in this provider.
	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer failing.Close()
	task.Targets[0] = failing.URL
	if err := p.Deliver(context.Background(), task); err == nil || !strings.Contains(err.Error(), "500") {
		t.Fatalf("non-2xx: want failure, got %v", err)
	}

	// Malformed tasks refuse.
	task.Targets = nil
	if err := p.Deliver(context.Background(), task); err == nil {
		t.Fatalf("no url: want failure")
	}
	task.Targets = []string{srv.URL}
	task.Payload.Raw = map[string]any{"event_id": "evt-2"}
	if err := p.Deliver(context.Background(), task); err == nil || !strings.Contains(err.Error(), "signed event body") {
		t.Fatalf("no body: want failure, got %v", err)
	}

	// An unreachable endpoint is a transport failure for the retryer.
	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	deadURL := dead.URL
	dead.Close()
	task.Payload.Raw = map[string]any{"event_id": "evt-3", "body": "{}", "signature": signaturePrefix + "abc"}
	task.Targets = []string{deadURL}
	if err := p.Deliver(context.Background(), task); err == nil {
		t.Fatalf("unreachable endpoint: want failure")
	}
}

// TestPipelineDeliversSignedEvent is the §13.5 end-to-end proof: the
// emitter's task rides the real queue→manager→provider path and lands
// at the app's HTTP endpoint with a verifiable signature.
func TestPipelineDeliversSignedEvent(t *testing.T) {
	type received struct {
		body    []byte
		sig     string
		eventID string
	}
	got := make(chan received, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		got <- received{
			body:    body,
			sig:     r.Header.Get(SignatureHeader),
			eventID: r.Header.Get(EventIDHeader),
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	e, reg, q := emitterEnv(t)
	if err := reg.SetCallback("ferry", srv.URL, testSecret); err != nil {
		t.Fatalf("re-point callback at the test server: %v", err)
	}

	mgr := runtime.NewManager(100)
	if err := mgr.RegisterProvider(ProviderName, NewProvider(), true); err != nil {
		t.Fatalf("register dispatcher: %v", err)
	}
	// The worker loop's shape: Pop → Deliver → Ack.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		for {
			task, err := q.Pop(ctx)
			if err != nil {
				return
			}
			_ = mgr.Deliver(ctx, task)
			_ = q.Ack(ctx, task.ID)
		}
	}()

	e.OnSettle(&core.DeliveryTask{
		ID: "t9", Provider: "email", Source: "app:ferry",
		AudienceID: "alice", Category: "alerts",
	}, nil)

	select {
	case r := <-got:
		mac := hmac.New(sha256.New, []byte(testSecret))
		mac.Write(r.body)
		if r.sig != signaturePrefix+hex.EncodeToString(mac.Sum(nil)) {
			t.Fatalf("delivered signature mismatch: %q", r.sig)
		}
		var ev Event
		if err := json.Unmarshal(r.body, &ev); err != nil {
			t.Fatalf("delivered body: %v", err)
		}
		if ev.EventID != r.eventID || ev.Delivery == nil || ev.Delivery.TaskID != "t9" {
			t.Fatalf("delivered event: %+v (header id %q)", ev, r.eventID)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("callback never landed at the app endpoint")
	}
}
