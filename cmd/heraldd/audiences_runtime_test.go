package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestServeCmdAudiencesRuntimeLifecycle boots the daemon with a
// pre-populated audiences store plus a config seed and drives the runtime
// API end to end: the store entry and the seed both serve, a POST persists
// to the file, and the merged table survives through shutdown.
func TestServeCmdAudiencesRuntimeLifecycle(t *testing.T) {
	httpPort := freePort(t)
	audiencesPath := filepath.Join(t.TempDir(), "audiences.json")
	// The store already holds an audience the config does not seed: the
	// runtime API must keep it alongside the config seed.
	if err := os.WriteFile(audiencesPath, []byte(`{"version":1,"audiences":[{"id":"stored","recipients":["alice"]}]}`), 0o600); err != nil {
		t.Fatalf("write audiences store: %v", err)
	}

	cfgYAML := fmt.Sprintf(`
server:
  addr: 127.0.0.1:%d
queue:
  type: memory
  workers: 1
auth:
  enabled: true
  api_keys:
    "audience-runtime-key": test key
audiences_store: %s
recipients:
  alice:
    endpoints:
      - type: log
        target: alice
audiences:
  ops:
    recipients: [alice]
`, httpPort, audiencesPath)
	path := writeTestConfig(t, cfgYAML)

	done := make(chan int, 1)
	go func() { done <- serveCmd([]string{"--config", path}) }()
	waitHTTPReady(t, fmt.Sprintf("http://127.0.0.1:%d/", httpPort), 10*time.Second)
	time.Sleep(200 * time.Millisecond)

	base := fmt.Sprintf("http://127.0.0.1:%d/api/v1/audiences", httpPort)
	do := func(method, url, body string) (int, string) {
		t.Helper()
		var rd io.Reader
		if body != "" {
			rd = strings.NewReader(body)
		}
		req, err := http.NewRequest(method, url, rd)
		if err != nil {
			t.Fatalf("build %s: %v", method, err)
		}
		req.Header.Set("X-API-Key", "audience-runtime-key")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", method, url, err)
		}
		defer func() { _ = resp.Body.Close() }()
		raw, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(raw)
	}

	// The stored entry and the config seed both serve: count 2, and both
	// ids are present.
	code, body := do(http.MethodGet, base, "")
	if code != http.StatusOK {
		t.Fatalf("list = %d %q, want 200", code, body)
	}
	var list struct {
		Data struct {
			Count int `json:"count"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &list); err != nil {
		t.Fatalf("parse list: %v (%q)", err, body)
	}
	if list.Data.Count != 2 || !strings.Contains(body, `"stored"`) || !strings.Contains(body, `"ops"`) {
		t.Fatalf("expected the stored audience and the config seed, got %q", body)
	}

	// A runtime write is served at once and persists to the store file.
	code, body = do(http.MethodPost, base, `{"id":"runtime","recipients":["alice"]}`)
	if code != http.StatusOK {
		t.Fatalf("create = %d %q, want 200", code, body)
	}
	code, body = do(http.MethodPut, base+"/ops", `{"recipients":["alice"]}`)
	if code != http.StatusOK {
		t.Fatalf("update = %d %q, want 200", code, body)
	}
	code, body = do(http.MethodDelete, base+"/stored", "")
	if code != http.StatusOK {
		t.Fatalf("delete = %d %q, want 200", code, body)
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

	// The file reflects the runtime session: the seed and the runtime
	// write are there, the deleted stored entry is gone.
	data, err := os.ReadFile(audiencesPath)
	if err != nil {
		t.Fatalf("read audiences store: %v", err)
	}
	if !strings.Contains(string(data), `"ops"`) || !strings.Contains(string(data), `"runtime"`) {
		t.Fatalf("audiences store must hold the seed and the runtime write, got %q", data)
	}
	if strings.Contains(string(data), `"stored"`) {
		t.Fatalf("deleted audience must be gone from the store, got %q", data)
	}
}
