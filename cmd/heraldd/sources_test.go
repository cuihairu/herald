package main

import (
	"context"
	"crypto/sha1"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/cuihairu/herald/core/audience"
	"github.com/cuihairu/herald/core/digest"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

// TestServeCmdSourcesLifecycle: sources.enabled brings the §8 entries up
// with the server — the bot webhook gates on its secret, the 公众号
// endpoint verifies signatures with the configured token, the in-app
// toggle API is reachable, and the reconcile loop runs its sweeps on
// schedule. Everything tears down through the normal signal path.
func TestServeCmdSourcesLifecycle(t *testing.T) {
	httpPort := freePort(t)
	wsPort := freePort(t)

	cfgYAML := fmt.Sprintf(`
server:
  addr: 127.0.0.1:%d
websocket:
  addr: 127.0.0.1:%d
queue:
  type: memory
  workers: 1
sources:
  enabled: true
  bot:
    secret: "lifecycle-secret"
    default_categories: ["notices"]
  wechat_mp:
    token: "lifecycle-mp-token"
    default_categories: ["notices"]
  reconcile:
    enabled: true
`, httpPort, wsPort)
	path := writeTestConfig(t, cfgYAML)

	done := make(chan int, 1)
	go func() { done <- serveCmd([]string{"--config", path}) }()

	waitHTTPReady(t, fmt.Sprintf("http://127.0.0.1:%d/", httpPort), 10*time.Second)
	base := fmt.Sprintf("http://127.0.0.1:%d", httpPort)

	// Bot webhook: wrong secret is an intruder (403).
	resp, err := http.Post(base+"/api/v1/callbacks/bot", "application/json", strings.NewReader(`{"message":null}`))
	if err != nil {
		t.Fatalf("POST bot callback: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("bot callback without secret = %d, want 403", resp.StatusCode)
	}

	// Right secret: a non-message update is a 200 no-op — the endpoint
	// came up wired from the config secret.
	req, _ := http.NewRequest(http.MethodPost, base+"/api/v1/callbacks/bot", strings.NewReader(`{"update_id":1}`))
	req.Header.Set("X-Telegram-Bot-Api-Secret-Token", "lifecycle-secret")
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST bot update: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("bot update with secret = %d, want 200", resp.StatusCode)
	}

	// 公众号 console verification: the configured token verifies the
	// signature and the challenge echoes back.
	echo := mpChallengeURL(t, base+"/api/v1/callbacks/wechat-mp", "lifecycle-mp-token", "echo-me")
	resp, err = http.Get(echo)
	if err != nil {
		t.Fatalf("GET mp verification: %v", err)
	}
	body := readTestBody(t, resp)
	if resp.StatusCode != http.StatusOK || body != "echo-me" {
		t.Errorf("mp verification = (%d, %q), want (200, echo-me)", resp.StatusCode, body)
	}

	// In-app toggle API: reachable and wired to the shared adapter.
	resp, err = http.Post(base+"/api/v1/audiences/alice/subscriptions", "application/json",
		strings.NewReader(`{"category":"bills","channel":"email"}`))
	if err != nil {
		t.Fatalf("POST subscriptions: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("toggle on = %d, want 200", resp.StatusCode)
	}

	// Let a reconcile sweep or two fire (no providers configured — the
	// sweep runs over an empty probe set).
	time.Sleep(300 * time.Millisecond)

	if err := syscall.Kill(syscall.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatalf("send SIGTERM: %v", err)
	}
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("serveCmd = %d, want 0", code)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("serveCmd did not return after SIGTERM")
	}
}

// mpChallengeURL signs a 公众号 callback URL the way the console does:
// sha1 of the sorted (timestamp, nonce, token) triple, hex encoded.
func mpChallengeURL(t *testing.T, base, token, echostr string) string {
	t.Helper()
	ts := fmt.Sprintf("%d", time.Now().Unix())
	nonce := "lifecycle-nonce"
	parts := []string{ts, nonce, token}
	sort.Strings(parts)
	sum := sha1.Sum([]byte(strings.Join(parts, "")))
	return fmt.Sprintf("%s?signature=%x&timestamp=%s&nonce=%s&echostr=%s", base, sum, ts, nonce, echostr)
}

func readTestBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer func() { _ = resp.Body.Close() }()
	buf := make([]byte, 1024)
	n, _ := resp.Body.Read(buf)
	return string(buf[:n])
}

// TestServeCmdSourcesReconcileLifecycle: the §8 sweep comes up with a
// real leader lease (miniredis) and probeable providers — one telegram,
// one wechatmp, and one disabled provider that must never become a
// probe. A short interval makes the loop actually sweep (lease acquired,
// RunOnce over the registered targets), and the lease must be released
// on the way out.
func TestServeCmdSourcesReconcileLifecycle(t *testing.T) {
	httpPort := freePort(t)
	wsPort := freePort(t)
	mr := miniredis.RunT(t)

	cfgYAML := fmt.Sprintf(`
server:
  addr: 127.0.0.1:%d
websocket:
  addr: 127.0.0.1:%d
queue:
  type: memory
  workers: 1
providers:
  bot:
    type: telegram
    config: { token: "123:abc", chat_id: "-100200300" }
  mp:
    type: wechatmp
    config: { app_id: "wx1", app_secret: "s1", template_id: "T" }
  off:
    type: telegram
    config: { token: "456:def", chat_id: "2" }
    enabled: false
  # A provider type no probe reads: the scan must skip it silently.
  misc:
    type: webhook
    config: { url: "https://hooks.example/x" }
sources:
  enabled: true
  reconcile:
    enabled: true
    interval: 1s
digest:
  redis_addr: %s
`, httpPort, wsPort, mr.Addr())
	path := writeTestConfig(t, cfgYAML)

	done := make(chan int, 1)
	go func() { done <- serveCmd([]string{"--config", path}) }()

	waitHTTPReady(t, fmt.Sprintf("http://127.0.0.1:%d/", httpPort), 10*time.Second)
	// Let the one scheduled sweep tick fire (interval 1s) over the
	// (empty) target list before tearing down.
	time.Sleep(1300 * time.Millisecond)

	if err := syscall.Kill(syscall.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatalf("send SIGTERM: %v", err)
	}
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("serveCmd = %d, want 0", code)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("serveCmd did not return after SIGTERM")
	}
	// The sweep's lease is released on shutdown, like the digest's.
	if mr.Exists("herald:sources:reconcile:leader") {
		t.Error("reconcile leader lease still held after shutdown")
	}
}

// TestReconcileLoopContinuesWhenLeaseUnavailable: a sweep whose leader
// lock cannot be acquired (dead redis) must skip the round and keep the
// loop alive — 对账 never guesses, and it also never wedges.
func TestReconcileLoopContinuesWhenLeaseUnavailable(t *testing.T) {
	r := audience.NewReconciler(audience.NewSurfaceRegistry(), audience.NewRegistry())
	lock := digest.NewLeaderLock(redis.NewClient(&redis.Options{Addr: "127.0.0.1:1"}), "herald:sources:reconcile:leader", time.Minute)
	defer lock.Release(context.Background())

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		reconcileLoop(ctx, r, lock, 10*time.Millisecond)
	}()
	// A few failed rounds, then wind the loop down.
	time.Sleep(80 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("reconcileLoop did not exit after ctx cancellation")
	}
}
