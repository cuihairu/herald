package main

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestServeCmdFeedsLifecycle: feeds.enabled brings the §9 pull half up
// with the server — the endpoints serve the configured channel metadata,
// and everything tears down through the normal signal path.
func TestServeCmdFeedsLifecycle(t *testing.T) {
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
feeds:
  enabled: true
  title: "Test Feed"
  link: "https://herald.example"
`, httpPort, wsPort)
	path := writeTestConfig(t, cfgYAML)

	done := make(chan int, 1)
	go func() { done <- serveCmd([]string{"--config", path}) }()

	waitHTTPReady(t, fmt.Sprintf("http://127.0.0.1:%d/", httpPort), 10*time.Second)

	// The public feed endpoint answers with the configured metadata —
	// proof the store, the registries and the routes all came up wired.
	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/feeds/alerts.xml", httpPort))
	if err != nil {
		t.Fatalf("GET feed: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET feed = %d\n%s", resp.StatusCode, body)
	}
	if !strings.Contains(string(body), "<title>Test Feed</title>") {
		t.Errorf("feed misses configured channel title:\n%s", body)
	}

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
