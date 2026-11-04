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

// TestProviderReusesPinnedClientTransport proves repeated deliveries with
// the same validated address answer reuse one pinned client. Mutating the
// answer replaces it, so stale addresses are never dialed.
func TestProviderReusesPinnedClientTransport(t *testing.T) {
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
	first := p.pinnedClientFor("http://app.example.com:"+srvURL.Port()+"/hook", []net.IP{srvIP})
	second := p.pinnedClientFor("http://app.example.com:"+srvURL.Port()+"/hook", []net.IP{srvIP})
	if first != second {
		t.Fatal("same URL and IP set must reuse the pinned client")
	}
	changed := p.pinnedClientFor("http://app.example.com:"+srvURL.Port()+"/hook", []net.IP{net.ParseIP("192.0.2.10")})
	if changed == first {
		t.Fatal("changed IP set must replace the pinned client")
	}
}

// TestProviderPinnedClientKeyIsOrderInsensitive: the same address set in
// a different answer order must reuse one pinned transport, not rebuild a
// fresh one (which would churn transports on every DNS answer rotation).
func TestProviderPinnedClientKeyIsOrderInsensitive(t *testing.T) {
	p := NewProvider()
	ipA := net.ParseIP("192.0.2.1")
	ipB := net.ParseIP("192.0.2.2")

	first := p.pinnedClientFor("http://app.example.com/hook", []net.IP{ipA, ipB})
	second := p.pinnedClientFor("http://app.example.com/hook", []net.IP{ipB, ipA})
	if first != second {
		t.Fatal("reordered IP set must reuse the pinned client")
	}
}

// TestProviderDropsPinnedClientOnCallbackClear: clearing a callback URL
// must drop its cached pinned transport so the connection pool is not
// retained indefinitely after the URL is removed.
func TestProviderDropsPinnedClientOnCallbackClear(t *testing.T) {
	reg, err := apps.NewRegistry([]apps.SeedApp{{
		Name:   "demo-app",
		Tokens: []apps.SeedToken{{Secret: "tok", Scopes: []string{"config"}}},
	}})
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	reg.SetCallbackPolicy(apps.CallbackPolicy{AllowPrivate: true})
	reg.LookupIP = func(host string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("127.0.0.1")}, nil
	}
	if err := reg.SetCallback("demo-app", "http://app.example.com/hook", strings.Repeat("s", 16)); err != nil {
		t.Fatalf("set callback: %v", err)
	}

	p := NewProvider()
	p.SetAppRegistry(reg)
	url := "http://app.example.com/hook"
	_ = p.pinnedClientFor(url, []net.IP{net.ParseIP("127.0.0.1")})
	if len(p.pinned) != 1 {
		t.Fatalf("pinned entries = %d, want 1", len(p.pinned))
	}
	if err := reg.ClearCallback("demo-app"); err != nil {
		t.Fatalf("clear callback: %v", err)
	}
	if len(p.pinned) != 0 {
		t.Fatalf("pinned entries after clear = %d, want 0", len(p.pinned))
	}
}

// TestProviderDropsPinnedClientOnCallbackReplace: re-pointing a callback
// URL must drop the old URL's cached transport; the new URL is built
// lazily on the next delivery.
func TestProviderDropsPinnedClientOnCallbackReplace(t *testing.T) {
	reg, err := apps.NewRegistry([]apps.SeedApp{{
		Name:   "demo-app",
		Tokens: []apps.SeedToken{{Secret: "tok", Scopes: []string{"config"}}},
	}})
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	reg.SetCallbackPolicy(apps.CallbackPolicy{AllowPrivate: true})
	reg.LookupIP = func(host string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("127.0.0.1")}, nil
	}
	oldURL := "http://app.example.com/old"
	if err := reg.SetCallback("demo-app", oldURL, strings.Repeat("s", 16)); err != nil {
		t.Fatalf("set callback: %v", err)
	}

	p := NewProvider()
	p.SetAppRegistry(reg)
	_ = p.pinnedClientFor(oldURL, []net.IP{net.ParseIP("127.0.0.1")})
	if len(p.pinned) != 1 {
		t.Fatalf("pinned entries = %d, want 1", len(p.pinned))
	}
	if err := reg.SetCallback("demo-app", "http://app.example.com/new", strings.Repeat("s", 16)); err != nil {
		t.Fatalf("replace callback: %v", err)
	}
	if len(p.pinned) != 0 {
		t.Fatalf("pinned entries after replace = %d, want 0", len(p.pinned))
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

// TestProviderSharedCallbackURLKeepsPinnedClient: two apps sharing one
// callback URL deliver through one pinned transport. Clearing one app's
// callback must not drop the transport (the other app still uses it); only
// clearing the last reference drops it.
func TestProviderSharedCallbackURLKeepsPinnedClient(t *testing.T) {
	reg, err := apps.NewRegistry([]apps.SeedApp{
		{Name: "app-a", Tokens: []apps.SeedToken{{Secret: "tok-a", Scopes: []string{"config"}}}},
		{Name: "app-b", Tokens: []apps.SeedToken{{Secret: "tok-b", Scopes: []string{"config"}}}},
	})
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	reg.SetCallbackPolicy(apps.CallbackPolicy{AllowPrivate: true})
	reg.LookupIP = func(host string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("127.0.0.1")}, nil
	}
	shared := "http://app.example.com/hook"
	if err := reg.SetCallback("app-a", shared, strings.Repeat("s", 16)); err != nil {
		t.Fatalf("set callback a: %v", err)
	}
	if err := reg.SetCallback("app-b", shared, strings.Repeat("s", 16)); err != nil {
		t.Fatalf("set callback b: %v", err)
	}

	p := NewProvider()
	p.SetAppRegistry(reg)
	_ = p.pinnedClientFor(shared, []net.IP{net.ParseIP("127.0.0.1")})
	if len(p.pinned) != 1 {
		t.Fatalf("pinned entries = %d, want 1", len(p.pinned))
	}
	// Clearing one app must not drop the shared transport.
	if err := reg.ClearCallback("app-a"); err != nil {
		t.Fatalf("clear callback a: %v", err)
	}
	if len(p.pinned) != 1 {
		t.Fatalf("pinned entries after clearing one sharer = %d, want 1", len(p.pinned))
	}
	// Clearing the last app drops it.
	if err := reg.ClearCallback("app-b"); err != nil {
		t.Fatalf("clear callback b: %v", err)
	}
	if len(p.pinned) != 0 {
		t.Fatalf("pinned entries after clearing last sharer = %d, want 0", len(p.pinned))
	}
}

// TestProviderSetCallbackReplaceSharedURLKeepsPinnedClient: when one app
// replaces a shared callback URL, the old URL is still referenced by the
// other app, so the pinned transport for the old URL must not be dropped
// (the SetCallback replace path, not just ClearCallback, must honour
// presence).
func TestProviderSetCallbackReplaceSharedURLKeepsPinnedClient(t *testing.T) {
	reg, err := apps.NewRegistry([]apps.SeedApp{
		{Name: "app-a", Tokens: []apps.SeedToken{{Secret: "tok-a", Scopes: []string{"config"}}}},
		{Name: "app-b", Tokens: []apps.SeedToken{{Secret: "tok-b", Scopes: []string{"config"}}}},
	})
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	reg.SetCallbackPolicy(apps.CallbackPolicy{AllowPrivate: true})
	reg.LookupIP = func(host string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("127.0.0.1")}, nil
	}
	shared := "http://app.example.com/hook"
	if err := reg.SetCallback("app-a", shared, strings.Repeat("s", 16)); err != nil {
		t.Fatalf("set callback a: %v", err)
	}
	if err := reg.SetCallback("app-b", shared, strings.Repeat("s", 16)); err != nil {
		t.Fatalf("set callback b: %v", err)
	}

	p := NewProvider()
	p.SetAppRegistry(reg)
	_ = p.pinnedClientFor(shared, []net.IP{net.ParseIP("127.0.0.1")})
	if len(p.pinned) != 1 {
		t.Fatalf("pinned entries = %d, want 1", len(p.pinned))
	}
	// App-a moves off the shared URL; app-b still references it, so the
	// transport must survive the replace.
	if err := reg.SetCallback("app-a", "http://app.example.com/hook2", strings.Repeat("s", 16)); err != nil {
		t.Fatalf("replace callback a: %v", err)
	}
	if len(p.pinned) != 1 {
		t.Fatalf("pinned entries after replacing one sharer = %d, want 1", len(p.pinned))
	}
	// App-b moves off too; the last reference is gone, so the transport drops.
	if err := reg.SetCallback("app-b", "http://app.example.com/hook2", strings.Repeat("s", 16)); err != nil {
		t.Fatalf("replace callback b: %v", err)
	}
	if len(p.pinned) != 0 {
		t.Fatalf("pinned entries after replacing last sharer = %d, want 0", len(p.pinned))
	}
}

// TestProviderSetAppRegistryConcurrentWithPinnedAccess exercises reattaching
// the registry while deliveries read the registry and use the pinned cache;
// it is a regression guard for the data race where SetAppRegistry wrote
// p.apps/p.pinned without holding p.mu.
func TestProviderSetAppRegistryConcurrentWithPinnedAccess(t *testing.T) {
	reg, err := apps.NewRegistry([]apps.SeedApp{{
		Name:   "demo-app",
		Tokens: []apps.SeedToken{{Secret: "tok", Scopes: []string{"config"}}},
	}})
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	reg.SetCallbackPolicy(apps.CallbackPolicy{AllowPrivate: true})
	reg.LookupIP = func(host string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("127.0.0.1")}, nil
	}
	if err := reg.SetCallback("demo-app", "http://app.example.com/hook", strings.Repeat("s", 16)); err != nil {
		t.Fatalf("set callback: %v", err)
	}

	p := NewProvider()
	p.SetAppRegistry(reg)
	_ = p.pinnedClientFor("http://app.example.com/hook", []net.IP{net.ParseIP("127.0.0.1")})

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 100; i++ {
			p.SetAppRegistry(reg)
		}
	}()
	for i := 0; i < 100; i++ {
		p.mu.Lock()
		_ = p.apps
		p.mu.Unlock()
		_ = p.pinnedClientFor("http://app.example.com/hook", []net.IP{net.ParseIP("127.0.0.1")})
	}
	<-done
}

// TestProviderSetAppRegistryNoUnhookWindow: swapping in a fresh registry
// must register its change hook before publishing it as live, so a
// Set/Clear on the new registry is always observed (the old code opened a
// window where p.apps pointed at reg but no subscriber was attached, and a
// dropped URL's pinned transport leaked until the next reattach).
func TestProviderSetAppRegistryNoUnhookWindow(t *testing.T) {
	reg := func() *apps.Registry {
		r, err := apps.NewRegistry([]apps.SeedApp{{
			Name:   "demo-app",
			Tokens: []apps.SeedToken{{Secret: "tok", Scopes: []string{"config"}}},
		}})
		if err != nil {
			t.Fatalf("registry: %v", err)
		}
		r.SetCallbackPolicy(apps.CallbackPolicy{AllowPrivate: true})
		r.LookupIP = func(host string) ([]net.IP, error) {
			return []net.IP{net.ParseIP("127.0.0.1")}, nil
		}
		if err := r.SetCallback("demo-app", "http://app.example.com/hook", strings.Repeat("s", 16)); err != nil {
			t.Fatalf("set callback: %v", err)
		}
		return r
	}()

	p := NewProvider()
	p.SetAppRegistry(reg)
	// Attach a second registry, then build a fresh pinned transport against
	// it. If the new registry's hook were registered only after p.apps was
	// published, a clear on the new registry could notify nobody and the
	// transport would leak until the next reattach.
	reg2 := func() *apps.Registry {
		r, err := apps.NewRegistry([]apps.SeedApp{{
			Name:   "demo-app",
			Tokens: []apps.SeedToken{{Secret: "tok", Scopes: []string{"config"}}},
		}})
		if err != nil {
			t.Fatalf("registry2: %v", err)
		}
		r.SetCallbackPolicy(apps.CallbackPolicy{AllowPrivate: true})
		r.LookupIP = func(host string) ([]net.IP, error) {
			return []net.IP{net.ParseIP("127.0.0.1")}, nil
		}
		if err := r.SetCallback("demo-app", "http://app.example.com/hook", strings.Repeat("s", 16)); err != nil {
			t.Fatalf("set callback2: %v", err)
		}
		return r
	}()
	p.SetAppRegistry(reg2)
	_ = p.pinnedClientFor("http://app.example.com/hook", []net.IP{net.ParseIP("127.0.0.1")})
	if len(p.pinned) != 1 {
		t.Fatalf("pinned entries after swap = %d, want 1", len(p.pinned))
	}
	if err := reg2.ClearCallback("demo-app"); err != nil {
		t.Fatalf("clear callback2: %v", err)
	}
	if len(p.pinned) != 0 {
		t.Fatalf("pinned entries after swap+clear = %d, want 0 (hook must be live before publish)", len(p.pinned))
	}
}

// TestProviderSetAppRegistryNilDetachesHook: attaching a nil registry takes
// the else arm — no hook is registered and the stale handle is cleared, so a
// later detach never calls into a registry the provider never had.
func TestProviderSetAppRegistryNilDetachesHook(t *testing.T) {
	p := NewProvider()
	p.SetAppRegistry(nil)
	if p.apps != nil {
		t.Fatal("apps registry must stay nil")
	}
	if p.unregisterHook != nil {
		t.Fatal("unregister hook must be nil when no registry is attached")
	}
}

// TestJoinIPsMixedAddressFamilies pins the key derivation for the address
// sets the DNS answer can actually contain: IPv4 and IPv6 literals (To4 vs
// To16 encodings), duplicate addresses (the sort's equal-elements arm), and
// an invalid entry (To16 nil). The key must not depend on answer order.
func TestJoinIPsMixedAddressFamilies(t *testing.T) {
	v4 := net.ParseIP("127.0.0.1")
	v6 := net.ParseIP("2001:db8::1")
	invalid := net.IP{1, 2, 3, 4, 5} // not a 4- or 16-byte IP: To16() is nil

	forward := joinIPs([]net.IP{v4, v6, v4, invalid})
	reversed := joinIPs([]net.IP{invalid, v4, v6, v4})
	if forward == "" {
		t.Fatal("joinIPs produced an empty key for a mixed address set")
	}
	if forward != reversed {
		t.Fatal("joinIPs key must be order-insensitive for the same address set")
	}
	if forward == joinIPs([]net.IP{v4}) {
		t.Fatal("mixed set must not collide with a single-address set")
	}
}
