package main

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

// TestServeCmdGroupMemberChannelBlock runs a channels block through a group
// member over the full pipeline (config → router → service → queue → worker
// → provider): a member naming a block key fans out to every provider in
// the block, one delivery each; an explicit provider instance still passes
// through untouched.
func TestServeCmdGroupMemberChannelBlock(t *testing.T) {
	httpPort := freePort(t)
	wsPort := freePort(t)

	// Two webhook sinks so the fan-out is observable at both ends.
	var aHits, bHits atomic.Int32
	a := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		aHits.Add(1)
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(a.Close)
	b := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bHits.Add(1)
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(b.Close)

	cfgYAML := fmt.Sprintf(`
server:
  addr: 127.0.0.1:%d
websocket:
  addr: 127.0.0.1:%d
queue:
  type: memory
  workers: 2
providers:
  hook-a:
    type: webhook
    config:
      url: %s
  hook-b:
    type: webhook
    config:
      url: %s
channels:
  oncall:
    providers: [hook-a, hook-b]
groups:
  - id: ops
    members:
      - channel: oncall
      - channel: hook-b
`, httpPort, wsPort, a.URL, b.URL)
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(cfgPath, []byte(cfgYAML), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	done := make(chan struct{})
	go func() {
		serveCmd([]string{"--config", cfgPath})
		close(done)
	}()

	waitHTTPReady(t, fmt.Sprintf("http://127.0.0.1:%d/", httpPort), 10*time.Second)
	time.Sleep(200 * time.Millisecond)

	body := `{"type":"alert","level":"error","content":{"title":"fan out","body":"member block"},"channels":["group:ops"]}`
	resp, err := http.Post(fmt.Sprintf("http://127.0.0.1:%d/api/v1/notify", httpPort), "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("notify: %v", err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("notify = %d, want 200", resp.StatusCode)
	}

	// One delivery per block provider plus the explicit member: hook-a once,
	// hook-b twice (block fan-out + explicit pass-through).
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if aHits.Load() >= 1 && bHits.Load() >= 2 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if got := aHits.Load(); got != 1 {
		t.Fatalf("hook-a hits = %d, want 1", got)
	}
	if got := bHits.Load(); got != 2 {
		t.Fatalf("hook-b hits = %d, want 2", got)
	}

	if err := syscall.Kill(syscall.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatalf("send SIGTERM: %v", err)
	}
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("serveCmd did not return after SIGTERM")
	}
}
