package callback

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/cuihairu/herald/core"
	"github.com/cuihairu/herald/core/apps"
	"github.com/cuihairu/herald/core/audience"
	"github.com/cuihairu/herald/core/queue"
	"github.com/cuihairu/herald/core/retry"
	"github.com/cuihairu/herald/core/runtime"
)

const testSecret = "callback-test-secret-32"

type boomError struct{}

func (*boomError) Error() string { return "boom" }

var errBoom error = &boomError{}

func emitterEnv(t *testing.T) (*Emitter, *apps.Registry, core.Queue) {
	t.Helper()
	reg, err := apps.NewRegistry([]apps.SeedApp{{
		Name:   "demo-app",
		Tokens: []apps.SeedToken{{Secret: "tok", Scopes: []string{"config"}}},
	}})
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	if err := reg.SetCallback("demo-app", "https://93.184.216.34/hook", testSecret); err != nil {
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
		ID: "t1", Provider: "email", Source: "app:demo-app",
		AudienceID: "alice", Category: "alerts", EventID: "77",
	}, nil)
	e.OnSettle(&core.DeliveryTask{
		ID: "t2", Provider: "sms", Source: "app:demo-app",
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
		if task.Targets[0] != "https://93.184.216.34/hook" {
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
		if ev.App != "demo-app" || ev.Kind != KindDeliveryResult || ev.EventID == "" || ev.At.IsZero() {
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
	// The integrator's event identity echoes back — the receipt-
	// correlation field (demo-app 的 outbox id 走这条线回家).
	if got := seen["success"]; got.EventID != "77" {
		t.Fatalf("success event_id = %q, want \"77\"", got.EventID)
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
	NewEmitter(nil, nil).OnSettle(&core.DeliveryTask{Source: "app:demo-app"}, nil)
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
		Type: audience.RelationSubscription, Source: "app:demo-app",
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
	if err := reg.ClearCallback("demo-app"); err != nil {
		t.Fatalf("clear: %v", err)
	}
	e.Unsubscribed(audience.Relation{
		AudienceID: "alice", Category: "alerts", Channel: "email",
		Type: audience.RelationSubscription, Source: "app:demo-app",
	}, "preference_center")
	if len(popAll(t, q)) != 0 {
		t.Fatalf("closed face must not enqueue")
	}

	// A dead queue swallows the push error — the hooks discard it by
	// contract, the direct callers read it. The callback config is
	// re-armed first so the push really reaches the queue.
	if err := reg.SetCallback("demo-app", "https://93.184.216.34/hook", testSecret); err != nil {
		t.Fatalf("re-arm: %v", err)
	}
	if err := q.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	e.Unsubscribed(audience.Relation{
		AudienceID: "alice", Category: "alerts", Channel: "email",
		Type: audience.RelationSubscription, Source: "app:demo-app",
	}, "preference_center")
	NewEmitter(reg, nil).Unsubscribed(audience.Relation{
		AudienceID: "alice", Category: "alerts", Channel: "email",
		Type: audience.RelationSubscription, Source: "app:demo-app",
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

// TestProviderDeliverRechecksDestination: the provider re-runs the app
// registry's destination policy immediately before posting, so a DNS
// answer that moved a hostname onto the deny list after SetCallback is
// refused at delivery time.
func TestProviderDeliverRechecksDestination(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	reg, err := apps.NewRegistry([]apps.SeedApp{{
		Name:   "demo-app",
		Tokens: []apps.SeedToken{{Secret: "tok", Scopes: []string{"config"}}},
	}})
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	// Allow the test server's loopback so SetCallback and the valid
	// delivery pass the recheck; the recheck still refuses other private
	// ranges (10/8) at delivery time because only the explicit deployment
	// switch admits loopback.
	reg.SetCallbackPolicy(apps.CallbackPolicy{AllowPrivate: true})
	if err := reg.SetCallback("demo-app", srv.URL, testSecret); err != nil {
		t.Fatalf("set callback: %v", err)
	}

	p := NewProvider()
	p.SetAppRegistry(reg)
	task := &core.DeliveryTask{
		ID:      "cb-recheck",
		Targets: []string{srv.URL},
		Payload: core.DeliveryPayload{Kind: core.PayloadRaw, Raw: map[string]any{
			"event_id":  "evt-r",
			"body":      `{"event_id":"evt-r"}`,
			"signature": signaturePrefix + "abc",
		}},
	}
	if err := p.Deliver(context.Background(), task); err != nil {
		t.Fatalf("deliver with valid destination: %v", err)
	}

	// A task whose URL is a denied private range must be refused even
	// though it was never passed to SetCallback. Use a strict registry
	// (no AllowPrivate) so the recheck still refuses 10/8; delivery
	// revalidates, never trusts.
	strict, err := apps.NewRegistry([]apps.SeedApp{{
		Name:   "demo-app",
		Tokens: []apps.SeedToken{{Secret: "tok", Scopes: []string{"config"}}},
	}})
	if err != nil {
		t.Fatalf("strict registry: %v", err)
	}
	pStrict := NewProvider()
	pStrict.SetAppRegistry(strict)
	task.Targets = []string{"http://10.0.0.1/hook"}
	if err := pStrict.Deliver(context.Background(), task); err == nil || !strings.Contains(err.Error(), "private/loopback") {
		t.Fatalf("private destination: want refusal, got %v", err)
	}
}

// TestProviderDeliverPinsValidatedHostname: delivery resolves the hostname
// once through the registry seam, pins the validated IPs, and connects
// without a second resolution. A resolver that answers the test server's
// loopback proves the seam path end to end.
func TestProviderDeliverPinsValidatedHostname(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	srvURL, _ := url.Parse(srv.URL)
	srvIP := net.ParseIP(srvURL.Hostname())

	reg, err := apps.NewRegistry([]apps.SeedApp{{
		Name:   "demo-app",
		Tokens: []apps.SeedToken{{Secret: "tok", Scopes: []string{"config"}}},
	}})
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	reg.SetCallbackPolicy(apps.CallbackPolicy{AllowPrivate: true})
	reg.LookupIP = func(host string) ([]net.IP, error) {
		return []net.IP{srvIP}, nil
	}

	p := NewProvider()
	p.SetAppRegistry(reg)
	task := &core.DeliveryTask{
		ID:      "cb-pin",
		Targets: []string{"http://app.example.com:" + srvURL.Port() + "/hook?pin=1"},
		Payload: core.DeliveryPayload{Kind: core.PayloadRaw, Raw: map[string]any{
			"event_id":  "evt-pin",
			"body":      `{"event_id":"evt-pin"}`,
			"signature": signaturePrefix + "abc",
		}},
	}
	if err := p.Deliver(context.Background(), task); err != nil {
		t.Fatalf("deliver pinned hostname: %v", err)
	}
}

// TestProviderDeliverRefusesHostnameResolvedToDenied: a hostname that the
// resolver maps to a denied network is refused at delivery time — the seam
// hostname path, not just literal-IP targets.
func TestProviderDeliverRefusesHostnameResolvedToDenied(t *testing.T) {
	reg, err := apps.NewRegistry([]apps.SeedApp{{
		Name:   "demo-app",
		Tokens: []apps.SeedToken{{Secret: "tok", Scopes: []string{"config"}}},
	}})
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	reg.LookupIP = func(host string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("10.0.0.1")}, nil
	}

	p := NewProvider()
	p.SetAppRegistry(reg)
	task := &core.DeliveryTask{
		ID:      "cb-deny-host",
		Targets: []string{"http://app.example.com/hook"},
		Payload: core.DeliveryPayload{Kind: core.PayloadRaw, Raw: map[string]any{
			"event_id":  "evt-dh",
			"body":      `{"event_id":"evt-dh"}`,
			"signature": signaturePrefix + "abc",
		}},
	}
	err = p.Deliver(context.Background(), task)
	if err == nil || !strings.Contains(err.Error(), "private/loopback") {
		t.Fatalf("denied hostname: want private refusal, got %v", err)
	}
	// A deterministic refusal is not retryable — a later attempt cannot
	// make a private destination lawful.
	if _, ok := err.(*retry.RetryableError); ok {
		t.Fatalf("denied hostname must not be retryable, got %v", err)
	}
}

// TestProviderDeliverDNSFailureIsRetryable: an unresolvable hostname at
// delivery time is transient — a DNS blip must not permanently fail the
// callback. The task comes back through the retry budget instead.
func TestProviderDeliverDNSFailureIsRetryable(t *testing.T) {
	reg, err := apps.NewRegistry([]apps.SeedApp{{
		Name:   "demo-app",
		Tokens: []apps.SeedToken{{Secret: "tok", Scopes: []string{"config"}}},
	}})
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	reg.LookupIP = func(host string) ([]net.IP, error) {
		return nil, &net.DNSError{Err: "no such host", Name: host}
	}

	p := NewProvider()
	p.SetAppRegistry(reg)
	task := &core.DeliveryTask{
		ID:      "cb-dns-fail",
		Targets: []string{"http://app.example.com/hook"},
		Payload: core.DeliveryPayload{Kind: core.PayloadRaw, Raw: map[string]any{
			"event_id":  "evt-df",
			"body":      `{"event_id":"evt-df"}`,
			"signature": signaturePrefix + "abc",
		}},
	}
	err = p.Deliver(context.Background(), task)
	if err == nil {
		t.Fatal("dns failure: want error")
	}
	var retryErr *retry.RetryableError
	if !errors.As(err, &retryErr) {
		t.Fatalf("dns failure must be retryable, got %v", err)
	}
}

// TestProviderDeliverRebindAfterSet: a URL that passed SetCallback against
// a public answer is refused when delivery's resolver now maps it to a
// denied network — the Set-time check alone cannot see the flip.
func TestProviderDeliverRebindAfterSet(t *testing.T) {
	reg, err := apps.NewRegistry([]apps.SeedApp{{
		Name:   "demo-app",
		Tokens: []apps.SeedToken{{Secret: "tok", Scopes: []string{"config"}}},
	}})
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	// Set sees a public answer.
	reg.LookupIP = func(host string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("93.184.216.34")}, nil
	}
	if err := reg.SetCallback("demo-app", "http://app.example.com/hook", testSecret); err != nil {
		t.Fatalf("set callback: %v", err)
	}
	// Delivery sees the answer flip to a private range.
	reg.LookupIP = func(host string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("10.0.0.1")}, nil
	}

	p := NewProvider()
	p.SetAppRegistry(reg)
	task := &core.DeliveryTask{
		ID:      "cb-rebind",
		Targets: []string{"http://app.example.com/hook"},
		Payload: core.DeliveryPayload{Kind: core.PayloadRaw, Raw: map[string]any{
			"event_id":  "evt-rb",
			"body":      `{"event_id":"evt-rb"}`,
			"signature": signaturePrefix + "abc",
		}},
	}
	err = p.Deliver(context.Background(), task)
	if err == nil || !strings.Contains(err.Error(), "private/loopback") {
		t.Fatalf("rebind after set: want private refusal, got %v", err)
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
	reg.SetCallbackPolicy(apps.CallbackPolicy{AllowPrivate: true})
	if err := reg.SetCallback("demo-app", srv.URL, testSecret); err != nil {
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
			// After Close the ready channel is drained-closed: Pop
			// answers (nil, nil). A late tick of this loop must stop,
			// not deref the nil task.
			if err != nil || task == nil {
				return
			}
			_ = mgr.Deliver(ctx, task)
			_ = q.Ack(ctx, task.ID)
		}
	}()

	e.OnSettle(&core.DeliveryTask{
		ID: "t9", Provider: "email", Source: "app:demo-app",
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
