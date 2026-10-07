package api

import (
	"crypto/sha1"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"testing/iotest"
	"time"

	"github.com/cuihairu/herald/core/audience"
)

// sourceTestEnv wires the §8 half: adapter over fresh registries, the
// bot entry with a secret and one default category, the MP entry with a
// verification token and one default category.
func sourceTestEnv(t *testing.T, secret, mpToken string) (*testEnv, *audience.SurfaceRegistry, *audience.Registry) {
	t.Helper()
	surfaces := audience.NewSurfaceRegistry()
	relations := audience.NewRegistry()
	env := newTestEnv(t, func(c *Config) {
		c.Sources = audience.NewSourceAdapter(surfaces, relations)
		c.SourceSurfaces = surfaces
		c.SourceBot = BotSourceConfig{Secret: secret, Defaults: []string{"notices"}}
		c.SourceWeChatMP = WeChatMPSourceConfig{Token: mpToken, Defaults: []string{"notices"}}
	})
	return env, surfaces, relations
}

// TestBotCallbackGating covers the states where the endpoint must not
// act: unconfigured, secret mismatch, wrong method, malformed body.
func TestBotCallbackGating(t *testing.T) {
	env, surfaces, _ := sourceTestEnv(t, "s3cret", "mp-tok")
	base := env.ts.URL

	// Unconfigured server: endpoint 404s.
	bare := newTestEnv(t, func(c *Config) {})
	resp, err := http.Get(bare.ts.URL + "/api/v1/callbacks/bot")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("unconfigured bot endpoint = %d, want 404", resp.StatusCode)
	}

	// Wrong secret.
	resp, _ = http.Post(base+"/api/v1/callbacks/bot", "application/json", strings.NewReader(`{"message":null}`))
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("no secret header = %d, want 403", resp.StatusCode)
	}

	// Right secret, wrong method.
	req, _ := http.NewRequest(http.MethodGet, base+"/api/v1/callbacks/bot", nil)
	req.Header.Set("X-Telegram-Bot-Api-Secret-Token", "s3cret")
	resp, _ = http.DefaultClient.Do(req)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("GET = %d, want 405", resp.StatusCode)
	}

	// Right secret, malformed JSON.
	resp, _ = postJSON(t, base+"/api/v1/callbacks/bot", "s3cret", `{`)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("malformed body = %d, want 400", resp.StatusCode)
	}

	// Non-message update: 200, nothing done.
	resp, _ = postJSON(t, base+"/api/v1/callbacks/bot", "s3cret", `{"update_id":1}`)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("non-message update = %d, want 200", resp.StatusCode)
	}
	if len(surfaces.Surfaces("nobody")) != 0 {
		t.Error("non-message update touched the registries")
	}
}

func postJSON(t *testing.T, urlStr, secret, body string) (*http.Response, error) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, urlStr, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Telegram-Bot-Api-Secret-Token", secret)
	return http.DefaultClient.Do(req)
}

func readAll(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer func() { _ = resp.Body.Close() }()
	buf, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(buf)
}

func TestBotStartRedeemsBindingAndDefaults(t *testing.T) {
	env, surfaces, relations := sourceTestEnv(t, "s3cret", "mp-tok")
	tok, err := surfaces.IssueBinding("alice", "telegram")
	if err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf(`{"message":{"chat":{"id":777},"text":"/start %s"}}`, tok)
	resp, err := postJSON(t, env.ts.URL+"/api/v1/callbacks/bot", "s3cret", body)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/start = %d", resp.StatusCode)
	}
	surface, ok := surfaces.Surface("alice", "telegram")
	if !ok || surface.Status != audience.SurfaceActive || surface.Target != "777" {
		t.Fatalf("surface = %+v ok=%v; want active 777", surface, ok)
	}
	if _, ok := relations.Lookup("alice", "notices", "telegram"); !ok {
		t.Error("default category not subscribed on /start")
	}
	rel, _ := relations.Lookup("alice", "notices", "telegram")
	if rel.Source != audience.SourceBot || !rel.Policy.AllowUnsubscribe {
		t.Errorf("default relation = %+v", rel)
	}

	// One-time: replaying the same token redeems nothing, answers 200.
	resp, _ = postJSON(t, env.ts.URL+"/api/v1/callbacks/bot", "s3cret", body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("replayed token = %d, want 200", resp.StatusCode)
	}

	// Unknown token: 200, nothing bound.
	resp, _ = postJSON(t, env.ts.URL+"/api/v1/callbacks/bot", "s3cret",
		`{"message":{"chat":{"id":778},"text":"/start deadbeef"}}`)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("unknown token = %d, want 200", resp.StatusCode)
	}
	if _, ok := surfaces.Surface("778", "telegram"); ok {
		t.Error("unknown token bound a surface")
	}

	// Bare /start and chatter: 200, no state.
	for _, text := range []string{"/start", "hello"} {
		resp, _ = postJSON(t, env.ts.URL+"/api/v1/callbacks/bot", "s3cret",
			fmt.Sprintf(`{"message":{"chat":{"id":777},"text":%q}}`, text))
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("text %q = %d, want 200", text, resp.StatusCode)
		}
	}
}

func TestBotStartRebindParksBehindIncumbent(t *testing.T) {
	env, surfaces, relations := sourceTestEnv(t, "s3cret", "mp-tok")
	if _, err := surfaces.IssueBinding("alice", "telegram"); err != nil {
		t.Fatal(err)
	}
	// chat-1 activates via a direct follow (bot), then chat-2's token
	// redeem must park behind it.
	if _, err := surfaces.Activate("alice", "telegram", "chat-1", "test"); err != nil {
		t.Fatal(err)
	}
	tok, err := surfaces.IssueBinding("alice", "telegram")
	if err != nil {
		t.Fatal(err)
	}
	resp, _ := postJSON(t, env.ts.URL+"/api/v1/callbacks/bot", "s3cret",
		fmt.Sprintf(`{"message":{"chat":{"id":222},"text":"/start %s"}}`, tok))
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("parked redeem = %d", resp.StatusCode)
	}
	// The incumbent keeps the slot and the parked default did not land:
	// the rebind is still pending the old channel's confirm.
	surface, _ := surfaces.Surface("alice", "telegram")
	if surface.Target != "chat-1" {
		t.Errorf("surface target = %q, want incumbent chat-1", surface.Target)
	}
	if _, ok := relations.Lookup("alice", "notices", "telegram"); ok {
		t.Error("pending rebind applied defaults before confirm")
	}
}

func TestBotStopSweepsTheChannel(t *testing.T) {
	env, surfaces, relations := sourceTestEnv(t, "s3cret", "mp-tok")
	adapter := audience.NewSourceAdapter(surfaces, relations)
	if _, err := adapter.Follow("alice", "telegram", "777", audience.SourceBot, []string{"notices"}); err != nil {
		t.Fatal(err)
	}
	resp, err := postJSON(t, env.ts.URL+"/api/v1/callbacks/bot", "s3cret",
		`{"message":{"chat":{"id":777},"text":"/stop"}}`)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/stop = %d", resp.StatusCode)
	}
	surface, _ := surfaces.Surface("alice", "telegram")
	if surface.Status != audience.SurfaceInvalid {
		t.Errorf("surface = %s, want invalid", surface.Status)
	}
	if _, ok := relations.Lookup("alice", "notices", "telegram"); ok {
		t.Error("/stop left the subscription registered")
	}

	// A /stop from an unknown chat is a 200 no-op.
	resp, _ = postJSON(t, env.ts.URL+"/api/v1/callbacks/bot", "s3cret",
		`{"message":{"chat":{"id":999},"text":"/stop"}}`)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("unknown /stop = %d, want 200", resp.StatusCode)
	}
}

// mpSignedURL builds the callback URL with a valid signature.
func mpSignedURL(base, token, query string) string {
	ts := fmt.Sprintf("%d", time.Now().Unix())
	nonce := "nonce-1"
	parts := []string{ts, nonce, token}
	sort.Strings(parts)
	sum := sha1.Sum([]byte(strings.Join(parts, "")))
	return fmt.Sprintf("%s?signature=%x&timestamp=%s&nonce=%s&%s", base, sum, ts, nonce, query)
}

func TestWeChatMPCallbackGatingAndVerification(t *testing.T) {
	env, _, _ := sourceTestEnv(t, "s3cret", "mp-tok")
	base := env.ts.URL + "/api/v1/callbacks/wechat-mp"

	// Unconfigured: 404.
	bare := newTestEnv(t, func(c *Config) {})
	resp, _ := http.Get(bare.ts.URL + "/api/v1/callbacks/wechat-mp")
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("unconfigured MP endpoint = %d, want 404", resp.StatusCode)
	}

	// Missing signature params: 403.
	resp, _ = http.Get(base)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("missing signature = %d, want 403", resp.StatusCode)
	}

	// Bad signature: 403.
	bad := strings.Replace(mpSignedURL(base, "mp-tok", "echostr=hi"), "timestamp=", "timestamp=x", 1)
	resp, _ = http.Get(bad)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("bad signature = %d, want 403", resp.StatusCode)
	}

	// Console verification: echo back.
	u := mpSignedURL(base, "mp-tok", "echostr=verify-me")
	resp, _ = http.Get(u)
	body := readAll(t, resp)
	if resp.StatusCode != http.StatusOK || body != "verify-me" {
		t.Errorf("echo verification = (%d, %q), want 200 verify-me", resp.StatusCode, body)
	}

	// PUT: not allowed (signature valid so the method check is reached).
	req, _ := http.NewRequest(http.MethodPut, mpSignedURL(base, "mp-tok", ""), nil)
	resp, _ = http.DefaultClient.Do(req)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("PUT = %d, want 405", resp.StatusCode)
	}

	// Malformed XML: 400.
	resp, _ = http.Post(mpSignedURL(base, "mp-tok", ""), "text/xml", strings.NewReader(`<xml>`))
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("malformed XML = %d, want 400", resp.StatusCode)
	}
}

func TestWeChatMPFollowEventsConverge(t *testing.T) {
	env, surfaces, relations := sourceTestEnv(t, "s3cret", "mp-tok")
	base := env.ts.URL + "/api/v1/callbacks/wechat-mp"
	adapter := audience.NewSourceAdapter(surfaces, relations)
	// alice bound her MP earlier, then unfollowed.
	if _, err := adapter.Follow("alice", "wechat_mp", "openid-a", audience.SourceWeChatMP, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := adapter.Unfollow("alice", "wechat_mp", audience.SourceWeChatMP); err != nil {
		t.Fatal(err)
	}

	xmlEvent := func(event string) string {
		return fmt.Sprintf(`<xml><ToUserName><![CDATA[herald]]></ToUserName><FromUserName><![CDATA[%s]]></FromUserName><Event><![CDATA[%s]]></Event></xml>`, "openid-a", event)
	}
	// subscribe: the invalid slot re-activates with the default group.
	resp, err := http.Post(mpSignedURL(base, "mp-tok", ""), "text/xml", strings.NewReader(xmlEvent("subscribe")))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("subscribe = %d", resp.StatusCode)
	}
	surface, _ := surfaces.Surface("alice", "wechat_mp")
	if surface.Status != audience.SurfaceActive || surface.Target != "openid-a" {
		t.Errorf("surface after subscribe = %+v", surface)
	}
	if _, ok := relations.Lookup("alice", "notices", "wechat_mp"); !ok {
		t.Error("MP follow default not subscribed")
	}

	// unsubscribe: 全停.
	resp, _ = http.Post(mpSignedURL(base, "mp-tok", ""), "text/xml", strings.NewReader(xmlEvent("unsubscribe")))
	_ = resp.Body.Close()
	surface, _ = surfaces.Surface("alice", "wechat_mp")
	if surface.Status != audience.SurfaceInvalid {
		t.Errorf("surface after unsubscribe = %+v", surface)
	}
	if _, ok := relations.Lookup("alice", "notices", "wechat_mp"); ok {
		t.Error("unsubscribe left the subscription registered")
	}

	// Unknown openid (never bound): 200, nothing happens.
	unknown := fmt.Sprintf(`<xml><FromUserName><![CDATA[%s]]></FromUserName><Event><![CDATA[subscribe]]></Event></xml>`, "openid-new")
	resp, _ = http.Post(mpSignedURL(base, "mp-tok", ""), "text/xml", strings.NewReader(unknown))
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("unknown openid = %d, want 200", resp.StatusCode)
	}
	if len(surfaces.Surfaces("openid-new")) != 0 {
		t.Error("unknown openid created state")
	}

	// Other events (CLICK etc.): 200 no-op — alice stays invalid.
	click := fmt.Sprintf(`<xml><FromUserName><![CDATA[%s]]></FromUserName><Event><![CDATA[CLICK]]></Event></xml>`, "openid-a")
	resp, _ = http.Post(mpSignedURL(base, "mp-tok", ""), "text/xml", strings.NewReader(click))
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("CLICK = %d, want 200", resp.StatusCode)
	}
	surface, _ = surfaces.Surface("alice", "wechat_mp")
	if surface.Status != audience.SurfaceInvalid {
		t.Error("unrelated event revived the surface")
	}

	// Empty FromUserName: 200 no-op.
	resp, _ = http.Post(mpSignedURL(base, "mp-tok", ""), "text/xml", strings.NewReader(`<xml><Event><![CDATA[subscribe]]></Event></xml>`))
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("empty openid = %d, want 200", resp.StatusCode)
	}
}

func TestSubscriptionsToggleAPI(t *testing.T) {
	env, _, relations := sourceTestEnv(t, "s3cret", "mp-tok")
	base := env.ts.URL + "/api/v1/audiences/alice/subscriptions"

	// Unconfigured: 404.
	bare := newTestEnv(t, func(c *Config) {})
	resp, _ := http.Post(bare.ts.URL+"/api/v1/audiences/alice/subscriptions", "application/json", strings.NewReader(`{}`))
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("unconfigured toggle = %d, want 404", resp.StatusCode)
	}

	// POST on: relation lands under preference_center.
	resp, err := http.Post(base, "application/json", strings.NewReader(`{"category":"bills","channel":"email"}`))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("toggle on = %d", resp.StatusCode)
	}
	rel, ok := relations.Lookup("alice", "bills", "email")
	if !ok || rel.Source != audience.SourcePreferenceCenter || rel.Type != audience.RelationSubscription {
		t.Fatalf("relation = %+v ok=%v", rel, ok)
	}

	// Validation: bad id, empty category, empty channel, malformed body.
	for name, do := range map[string]func() *http.Response{
		"bad audience id": func() *http.Response {
			r, _ := http.Post(env.ts.URL+"/api/v1/audiences/bad%20id/subscriptions", "application/json", strings.NewReader(`{"category":"bills","channel":"email"}`))
			return r
		},
		"empty category": func() *http.Response {
			r, _ := http.Post(base, "application/json", strings.NewReader(`{"category":"","channel":"email"}`))
			return r
		},
		"empty channel": func() *http.Response {
			r, _ := http.Post(base, "application/json", strings.NewReader(`{"category":"bills","channel":""}`))
			return r
		},
		"malformed body": func() *http.Response {
			r, _ := http.Post(base, "application/json", strings.NewReader(`{`))
			return r
		},
	} {
		r := do()
		_ = r.Body.Close()
		if r.StatusCode != http.StatusUnprocessableEntity && r.StatusCode != http.StatusBadRequest {
			t.Errorf("%s = %d, want 422/400", name, r.StatusCode)
		}
	}

	// DELETE off: relation gone.
	req, _ := http.NewRequest(http.MethodDelete, base+"?category=bills&channel=email", nil)
	resp, _ = http.DefaultClient.Do(req)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("toggle off = %d", resp.StatusCode)
	}
	if _, ok := relations.Lookup("alice", "bills", "email"); ok {
		t.Error("toggle-off left the relation")
	}

	// DELETE unknown slot: 404.
	req, _ = http.NewRequest(http.MethodDelete, base+"?category=bills&channel=email", nil)
	resp, _ = http.DefaultClient.Do(req)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("unknown toggle-off = %d, want 404", resp.StatusCode)
	}

	// DELETE on a must-deliver enrollment: 409 (the bottom line).
	if err := relations.Enroll(audience.Relation{
		AudienceID: "alice", Category: "system", Channel: "email",
		Type: audience.RelationEnrollment, Source: audience.SourceAdmin,
		Policy: audience.Policy{MustDeliver: true},
	}); err != nil {
		t.Fatal(err)
	}
	req, _ = http.NewRequest(http.MethodDelete, base+"?category=system&channel=email", nil)
	resp, _ = http.DefaultClient.Do(req)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("must-deliver toggle-off = %d, want 409", resp.StatusCode)
	}
	if _, ok := relations.Lookup("alice", "system", "email"); !ok {
		t.Error("must-deliver enrollment was taken down")
	}

	// PUT: 405.
	req, _ = http.NewRequest(http.MethodPut, base, strings.NewReader(`{}`))
	resp, _ = http.DefaultClient.Do(req)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("PUT = %d, want 405", resp.StatusCode)
	}

}

// TestSourceEndpointsRejectUnreadableBodies drives the three body reads
// with a failing reader (a client that hangs up mid-body): each entry
// answers 400 instead of acting on a truncated payload.
func TestSourceEndpointsRejectUnreadableBodies(t *testing.T) {
	env, _, _ := sourceTestEnv(t, "s3cret", "mp-tok")
	h := env.server.handler
	boom := errors.New("client hung up")

	// Bot webhook: valid secret, unreadable body.
	req := httptest.NewRequest(http.MethodPost, "/api/v1/callbacks/bot", iotest.ErrReader(boom))
	req.Header.Set("X-Telegram-Bot-Api-Secret-Token", "s3cret")
	rec := httptest.NewRecorder()
	h.HandleBotCallback(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("bot unreadable body = %d, want 400", rec.Code)
	}

	// MP callback: valid signature, unreadable body.
	req = httptest.NewRequest(http.MethodPost, mpSignedURL("/api/v1/callbacks/wechat-mp", "mp-tok", ""), iotest.ErrReader(boom))
	rec = httptest.NewRecorder()
	h.HandleWeChatMPCallback(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("mp unreadable body = %d, want 400", rec.Code)
	}

	// In-app toggle: unreadable body.
	req = httptest.NewRequest(http.MethodPost, "/api/v1/audiences/alice/subscriptions", iotest.ErrReader(boom))
	rec = httptest.NewRecorder()
	h.HandleSubscriptions(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("toggle unreadable body = %d, want 400", rec.Code)
	}
}

// TestSubscriptionsDeleteValidatesFields: the DELETE face runs the same
// field checks as POST — a missing category is a 422, not a silent off.
func TestSubscriptionsDeleteValidatesFields(t *testing.T) {
	env, _, _ := sourceTestEnv(t, "s3cret", "mp-tok")

	req, _ := http.NewRequest(http.MethodDelete, env.ts.URL+"/api/v1/audiences/alice/subscriptions", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("DELETE without category = %d, want 422", resp.StatusCode)
	}
}
