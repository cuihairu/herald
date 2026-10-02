package jpush

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/cuihairu/herald/core"
)

// readBody drains the full request body.
func readBody(r *http.Request) ([]byte, error) {
	var out []byte
	buf := make([]byte, 1024)
	for {
		n, err := r.Body.Read(buf)
		out = append(out, buf[:n]...)
		if err != nil {
			return out, nil
		}
	}
}

// baseConfig is the minimum configuration every constructor test extends.
func baseConfig() map[string]interface{} {
	return map[string]interface{}{
		"app_key":       "test_app_key",
		"master_secret": "test_master_secret",
	}
}

// newTestProvider builds a provider with the minimum valid configuration.
func newTestProvider(t *testing.T) *Provider {
	t.Helper()
	p, err := NewProvider(baseConfig())
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	return p.(*Provider)
}

// expectedAuth is the HTTP Basic header value for baseConfig.
func expectedAuth() string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte("test_app_key:test_master_secret"))
}

func TestNewProviderValidation(t *testing.T) {
	t.Run("missing app_key", func(t *testing.T) {
		_, err := NewProvider(map[string]interface{}{"master_secret": "s"})
		if err == nil || err.Error() != "jpush: app_key is required" {
			t.Errorf("NewProvider = %v, want app_key required", err)
		}
	})

	t.Run("missing master_secret", func(t *testing.T) {
		_, err := NewProvider(map[string]interface{}{"app_key": "k"})
		if err == nil || err.Error() != "jpush: master_secret is required" {
			t.Errorf("NewProvider = %v, want master_secret required", err)
		}
	})

	t.Run("negative time_to_live from int", func(t *testing.T) {
		cfg := baseConfig()
		cfg["time_to_live"] = -1
		_, err := NewProvider(cfg)
		if err == nil || err.Error() != "jpush: time_to_live must be >= 0" {
			t.Errorf("NewProvider = %v, want ttl validation", err)
		}
	})

	t.Run("negative time_to_live from float64", func(t *testing.T) {
		cfg := baseConfig()
		cfg["time_to_live"] = float64(-60)
		_, err := NewProvider(cfg)
		if err == nil || err.Error() != "jpush: time_to_live must be >= 0" {
			t.Errorf("NewProvider = %v, want ttl validation", err)
		}
	})

	t.Run("zero time_to_live accepted", func(t *testing.T) {
		cfg := baseConfig()
		cfg["time_to_live"] = 0
		if _, err := NewProvider(cfg); err != nil {
			t.Errorf("NewProvider with ttl 0: %v", err)
		}
	})
}

func TestParseConfigPlatform(t *testing.T) {
	cases := []struct {
		name  string
		value interface{}
		want  interface{}
	}{
		{"absent defaults to all", nil, "all"},
		{"string all", "all", "all"},
		{"string ALL folds", "ALL", "all"},
		{"comma list", "android,ios", []string{"android", "ios"}},
		{"comma list trimmed", " android , ios , ", []string{"android", "ios"}},
		{"only separators", ",", "all"},
		{"yaml list", []interface{}{"android", "ios"}, []string{"android", "ios"}},
		{"yaml list skips non-strings", []interface{}{42, "ios"}, []string{"ios"}},
		{"yaml list empty entries", []interface{}{""}, "all"},
		{"yaml list lone all", []interface{}{"all"}, "all"},
		{"string list", []string{"android", "ios"}, []string{"android", "ios"}},
		{"wrong type ignored", 42, "all"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := baseConfig()
			if tc.value != nil {
				cfg["platform"] = tc.value
			}
			p, err := NewProvider(cfg)
			if err != nil {
				t.Fatalf("NewProvider: %v", err)
			}
			got := p.(*Provider).platform
			if !platformEqual(got, tc.want) {
				t.Errorf("platform = %#v, want %#v", got, tc.want)
			}
		})
	}
}

// platformEqual compares the two legal platform shapes ("all" or
// []string) without reflect.DeepEqual.
func platformEqual(got, want interface{}) bool {
	gs, gok := got.(string)
	ws, wok := want.(string)
	if gok && wok {
		return gs == ws
	}
	gl, gok := got.([]string)
	wl, wok := want.([]string)
	if !gok || !wok || len(gl) != len(wl) {
		return false
	}
	for i := range gl {
		if gl[i] != wl[i] {
			return false
		}
	}
	return true
}

func TestParseConfigAPNsProduction(t *testing.T) {
	cases := []struct {
		name  string
		value interface{}
		want  bool
	}{
		{"absent defaults to production", nil, true},
		{"bool false", false, false},
		{"bool true", true, true},
		{"string false", "false", false},
		{"string true", "true", true},
		{"garbage string ignored", "yes-please", true},
		{"wrong type ignored", float64(0), true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := baseConfig()
			if tc.value != nil {
				cfg["apns_production"] = tc.value
			}
			p, err := NewProvider(cfg)
			if err != nil {
				t.Fatalf("NewProvider: %v", err)
			}
			if got := p.(*Provider).apnsProduction; got != tc.want {
				t.Errorf("apnsProduction = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestParseConfigTimeToLive(t *testing.T) {
	cases := []struct {
		name  string
		value interface{}
		want  interface{} // nil, or int
	}{
		{"absent", nil, nil},
		{"int", 60, 60},
		{"int64", int64(90), 90},
		{"float64 from JSON API", float64(120), 120},
		{"string ignored", "60", nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := baseConfig()
			if tc.value != nil {
				cfg["time_to_live"] = tc.value
			}
			p, err := NewProvider(cfg)
			if err != nil {
				t.Fatalf("NewProvider: %v", err)
			}
			got := p.(*Provider).timeToLive
			if tc.want == nil {
				if got != nil {
					t.Errorf("timeToLive = %d, want unset", *got)
				}
				return
			}
			if got == nil || *got != tc.want.(int) {
				t.Errorf("timeToLive = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestDeliverValidation(t *testing.T) {
	p := newTestProvider(t)
	ctx := context.Background()

	if err := p.Deliver(ctx, nil); err == nil || err.Error() != "jpush: task is nil" {
		t.Errorf("Deliver(nil) = %v", err)
	}
	if err := p.Deliver(ctx, &core.DeliveryTask{}); err == nil || err.Error() != "jpush: at least one target (registration_id) is required" {
		t.Errorf("Deliver without targets = %v", err)
	}
	err := p.Deliver(ctx, &core.DeliveryTask{Targets: []string{""}})
	if err == nil || err.Error() != "jpush: empty target found in targets" {
		t.Errorf("Deliver empty target = %v", err)
	}
	err = p.Deliver(ctx, &core.DeliveryTask{Targets: []string{"reg-1"}})
	if err == nil || err.Error() != "jpush: payload needs content or raw data" {
		t.Errorf("Deliver empty payload = %v", err)
	}
}

// TestDeliverNotificationPayload pins the wire format for a display
// alert: path, Basic auth, platform, audience, and the notification body.
func TestDeliverNotificationPayload(t *testing.T) {
	var gotPath, gotAuth string
	var gotBody []byte
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("authorization")
		gotBody, _ = readBody(r)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(ts.Close)

	p := newTestProvider(t)
	p.endpoint = ts.URL

	task := &core.DeliveryTask{
		Targets: []string{"reg-device-1"},
		Payload: core.DeliveryPayload{
			Kind:    core.PayloadContent,
			Content: &core.RenderedContent{Title: "磁盘告警", Body: "/dev/sda1 95%", Format: "plain"},
		},
	}
	if err := p.Deliver(context.Background(), task); err != nil {
		t.Fatalf("Deliver: %v", err)
	}

	if gotPath != "/3/push" {
		t.Errorf("path = %q, want /3/push", gotPath)
	}
	if gotAuth != expectedAuth() {
		t.Errorf("authorization = %q, want %q", gotAuth, expectedAuth())
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(gotBody, &payload); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	if payload["platform"] != "all" {
		t.Errorf("platform = %v, want all", payload["platform"])
	}
	audience, _ := payload["audience"].(map[string]interface{})
	ids, _ := audience["registration_id"].([]interface{})
	if len(ids) != 1 || ids[0] != "reg-device-1" {
		t.Errorf("registration_id = %v", audience["registration_id"])
	}

	notification, _ := payload["notification"].(map[string]interface{})
	if notification == nil {
		t.Fatalf("body = %s, want notification section", gotBody)
	}
	if notification["alert"] != "/dev/sda1 95%" {
		t.Errorf("alert = %v, want shared body", notification["alert"])
	}
	android, _ := notification["android"].(map[string]interface{})
	if android["title"] != "磁盘告警" || android["alert"] != "/dev/sda1 95%" {
		t.Errorf("android = %v", android)
	}
	ios, _ := notification["ios"].(map[string]interface{})
	if ios["alert"] != "/dev/sda1 95%" || ios["sound"] != "default" {
		t.Errorf("ios = %v", ios)
	}

	if _, ok := payload["message"]; ok {
		t.Errorf("message = %v, want absent for content-only push", payload["message"])
	}
	options, _ := payload["options"].(map[string]interface{})
	if options["apns_production"] != true {
		t.Errorf("apns_production = %v, want default true", options["apns_production"])
	}
	if _, ok := options["time_to_live"]; ok {
		t.Errorf("time_to_live = %v, want absent when unconfigured", options["time_to_live"])
	}
}

// TestDeliverTitleOnly checks the alert fallback: with no body the title
// becomes the alert everywhere.
func TestDeliverTitleOnly(t *testing.T) {
	var gotBody []byte
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = readBody(r)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(ts.Close)

	p := newTestProvider(t)
	p.endpoint = ts.URL

	task := &core.DeliveryTask{
		Targets: []string{"reg-device-1"},
		Payload: core.DeliveryPayload{
			Kind:    core.PayloadContent,
			Content: &core.RenderedContent{Title: "构建完成", Format: "plain"},
		},
	}
	if err := p.Deliver(context.Background(), task); err != nil {
		t.Fatalf("Deliver: %v", err)
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(gotBody, &payload); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	notification, _ := payload["notification"].(map[string]interface{})
	if notification["alert"] != "构建完成" {
		t.Errorf("alert = %v, want title fallback", notification["alert"])
	}
	android, _ := notification["android"].(map[string]interface{})
	if android["title"] != "构建完成" || android["alert"] != "构建完成" {
		t.Errorf("android = %v", android)
	}
	ios, _ := notification["ios"].(map[string]interface{})
	if ios["alert"] != "构建完成" {
		t.Errorf("ios.alert = %v, want title fallback", ios["alert"])
	}
}

// TestDeliverRawOnly pins pass-through pushes: no notification section,
// msg_content falls back to the JSON-encoded raw payload, extras
// stringified.
func TestDeliverRawOnly(t *testing.T) {
	var gotBody []byte
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = readBody(r)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(ts.Close)

	p := newTestProvider(t)
	p.endpoint = ts.URL

	task := &core.DeliveryTask{
		Targets: []string{"reg-device-1"},
		Payload: core.DeliveryPayload{
			Kind: core.PayloadRaw,
			Raw:  map[string]any{"event": "deploy", "code": 200},
		},
	}
	if err := p.Deliver(context.Background(), task); err != nil {
		t.Fatalf("Deliver: %v", err)
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(gotBody, &payload); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	if _, ok := payload["notification"]; ok {
		t.Errorf("notification = %v, want absent for raw-only push", payload["notification"])
	}
	message, _ := payload["message"].(map[string]interface{})
	if message == nil {
		t.Fatalf("body = %s, want message section", gotBody)
	}
	// encoding/json sorts map keys, so the fallback body is deterministic.
	if message["msg_content"] != `{"code":200,"event":"deploy"}` {
		t.Errorf("msg_content = %v", message["msg_content"])
	}
	extras, _ := message["extras"].(map[string]interface{})
	if extras["event"] != "deploy" || extras["code"] != "200" {
		t.Errorf("extras = %v", extras)
	}
}

// TestDeliverContentAndRaw checks both sections go out together: the
// notification shows the content, the message carries the raw payload
// with msg_content taken from the readable body.
func TestDeliverContentAndRaw(t *testing.T) {
	var gotBody []byte
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = readBody(r)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(ts.Close)

	p := newTestProvider(t)
	p.endpoint = ts.URL

	task := &core.DeliveryTask{
		Targets: []string{"reg-device-1"},
		Payload: core.DeliveryPayload{
			Kind:    core.PayloadContent,
			Content: &core.RenderedContent{Title: "发布", Body: "v1.2.3 已上线", Format: "plain"},
			Raw:     map[string]any{"version": "1.2.3"},
		},
	}
	if err := p.Deliver(context.Background(), task); err != nil {
		t.Fatalf("Deliver: %v", err)
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(gotBody, &payload); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	notification, _ := payload["notification"].(map[string]interface{})
	if notification == nil {
		t.Fatalf("body = %s, want notification section", gotBody)
	}
	message, _ := payload["message"].(map[string]interface{})
	if message["msg_content"] != "v1.2.3 已上线" {
		t.Errorf("msg_content = %v, want readable body", message["msg_content"])
	}
	extras, _ := message["extras"].(map[string]interface{})
	if extras["version"] != "1.2.3" {
		t.Errorf("extras = %v", extras)
	}
}

// TestDeliverRawMarshalError ensures an unencodable raw payload (only
// reachable when there is no readable content to fall back to) surfaces
// as the wrapped encode error before any network call.
func TestDeliverRawMarshalError(t *testing.T) {
	var called bool
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(ts.Close)

	p := newTestProvider(t)
	p.endpoint = ts.URL

	task := &core.DeliveryTask{
		Targets: []string{"reg-device-1"},
		Payload: core.DeliveryPayload{
			Kind: core.PayloadRaw,
			Raw:  map[string]any{"boom": make(chan int)},
		},
	}
	err := p.Deliver(context.Background(), task)
	if err == nil || !strings.Contains(err.Error(), "jpush: failed to encode raw payload:") {
		t.Errorf("Deliver = %v, want encode error", err)
	}
	if called {
		t.Error("endpoint reached, want failure before any network call")
	}
}

// TestDeliverConfiguredOptions checks explicit configuration wins:
// apns_production false, time_to_live, and a platform list all reach the
// wire as configured (parsed end to end through NewProvider).
func TestDeliverConfiguredOptions(t *testing.T) {
	var gotBody []byte
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = readBody(r)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(ts.Close)

	cfg := baseConfig()
	cfg["apns_production"] = false
	cfg["time_to_live"] = 60
	cfg["platform"] = "android,ios"
	cfg["endpoint"] = ts.URL
	prov, err := NewProvider(cfg)
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	p := prov.(*Provider)

	task := &core.DeliveryTask{
		Targets: []string{"reg-device-1"},
		Payload: core.DeliveryPayload{
			Kind:    core.PayloadContent,
			Content: &core.RenderedContent{Title: "t", Body: "b", Format: "plain"},
		},
	}
	if err := p.Deliver(context.Background(), task); err != nil {
		t.Fatalf("Deliver: %v", err)
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(gotBody, &payload); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	platform, _ := payload["platform"].([]interface{})
	if len(platform) != 2 || platform[0] != "android" || platform[1] != "ios" {
		t.Errorf("platform = %v, want [android ios]", payload["platform"])
	}
	options, _ := payload["options"].(map[string]interface{})
	if options["apns_production"] != false {
		t.Errorf("apns_production = %v, want false", options["apns_production"])
	}
	if options["time_to_live"] != float64(60) {
		t.Errorf("time_to_live = %v, want 60", options["time_to_live"])
	}
}

func TestDeliverPartialFailureAggregates(t *testing.T) {
	var successCount int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := readBody(r)
		var payload map[string]interface{}
		_ = json.Unmarshal(body, &payload)
		audience, _ := payload["audience"].(map[string]interface{})
		ids, _ := audience["registration_id"].([]interface{})
		target, _ := ids[0].(string)
		if strings.HasPrefix(target, "bad") {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"code":1011,"message":"not valid"}}`))
			return
		}
		atomic.AddInt32(&successCount, 1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(ts.Close)

	p := newTestProvider(t)
	p.endpoint = ts.URL

	task := &core.DeliveryTask{
		Targets: []string{"bad-registration-id", "good-registration", "bad1"},
		Payload: core.DeliveryPayload{
			Kind:    core.PayloadContent,
			Content: &core.RenderedContent{Title: "t", Body: "b", Format: "plain"},
		},
	}
	err := p.Deliver(context.Background(), task)
	if err == nil {
		t.Fatal("Deliver with failing targets = nil error")
	}
	// Long failing targets are truncated at 8 runes; the short one passes
	// through whole — both arms of truncate must appear in the aggregate.
	want := "jpush: 1/3 succeeded - failed: bad-regi"
	if !strings.HasPrefix(err.Error(), want) {
		t.Errorf("error = %q, want prefix %q", err.Error(), want)
	}
	if !strings.Contains(err.Error(), "bad1:") {
		t.Errorf("error = %q, want untruncated short target", err.Error())
	}
	if !strings.Contains(err.Error(), "1011") {
		t.Errorf("error = %q, want JPush error body", err.Error())
	}
	if got := atomic.LoadInt32(&successCount); got != 1 {
		t.Errorf("successful sends = %d, want 1", got)
	}
}

// TestDeliverHTTPErrorSurfacesBody checks a rate-limited response (429,
// JPush error code 2002) reaches the aggregated error with both status
// and body — retryability stays with httpclient's statusError.
func TestDeliverHTTPErrorSurfacesBody(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"code":2002,"message":"Rate limit exceeded"}}`))
	}))
	t.Cleanup(ts.Close)

	p := newTestProvider(t)
	p.endpoint = ts.URL

	task := &core.DeliveryTask{
		Targets: []string{"reg-device-1"},
		Payload: core.DeliveryPayload{
			Kind:    core.PayloadContent,
			Content: &core.RenderedContent{Title: "t", Body: "b", Format: "plain"},
		},
	}
	err := p.Deliver(context.Background(), task)
	if err == nil {
		t.Fatal("Deliver against 429 = nil error")
	}
	if !strings.Contains(err.Error(), "429") || !strings.Contains(err.Error(), "2002") {
		t.Errorf("error = %v, want status and JPush code", err)
	}
	if !strings.Contains(err.Error(), "succeeded") {
		t.Errorf("error = %v, want aggregated form", err)
	}
}

func TestProviderSurface(t *testing.T) {
	p := newTestProvider(t)

	if got := p.Name(); got != "jpush" {
		t.Errorf("Name() = %q", got)
	}
	if got := p.Type(); got != "jpush" {
		t.Errorf("Type() = %q", got)
	}
	if st := p.Status(); st == nil || st.Status != "available" {
		t.Errorf("Status() = %+v", st)
	}
	// Close is a convention outside the core.Provider interface.
	if err := p.Close(); err != nil {
		t.Errorf("Close() = %v", err)
	}

	cfg := p.GetConfig()
	if cfg["app_key"] != "test_app_key" || cfg["master_secret"] != "test_master_secret" {
		t.Errorf("GetConfig credentials = %v", cfg)
	}
	if cfg["platform"] != "all" {
		t.Errorf("GetConfig platform = %v, want all", cfg["platform"])
	}
	if cfg["apns_production"] != true {
		t.Errorf("GetConfig apns_production = %v, want default true", cfg["apns_production"])
	}
	if cfg["endpoint"] != defaultEndpoint {
		t.Errorf("GetConfig endpoint = %v", cfg["endpoint"])
	}
	if _, ok := cfg["time_to_live"]; ok {
		t.Errorf("GetConfig time_to_live = %v, want absent when unconfigured", cfg["time_to_live"])
	}

	// The API layer masks master_secret; the provider itself hands out
	// the real value so merge-updates keep working.
	if masked := core.MaskConfig(cfg); masked["master_secret"] != "******" {
		t.Errorf("MaskConfig master_secret = %v, want ******", masked["master_secret"])
	}

	// Configured ttl round-trips through GetConfig.
	cfgIn := baseConfig()
	cfgIn["time_to_live"] = 45
	withTTL, err := NewProvider(cfgIn)
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	if got := withTTL.(*Provider).GetConfig()["time_to_live"]; got != 45 {
		t.Errorf("GetConfig time_to_live = %v, want 45", got)
	}

	cap := p.Capability()
	if len(cap.PayloadKinds) != 2 || cap.ContentFormats[0] != "plain" {
		t.Errorf("Capability() = %+v", cap)
	}

	f := &Factory{}
	if f.Name() != "jpush" || f.Type() != "jpush" {
		t.Errorf("Factory name/type = %q/%q", f.Name(), f.Type())
	}
	built, err := f.Create(baseConfig())
	if err != nil || built == nil {
		t.Errorf("Factory.Create() = %v, %v", built, err)
	}
	if _, err := f.Create(map[string]interface{}{}); err == nil {
		t.Error("Factory.Create(empty) = nil error")
	}
}
