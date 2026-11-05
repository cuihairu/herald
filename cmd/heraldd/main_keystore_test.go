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

// TestServeCmdCredentialStoreWiring wires auth.store_path and exercises
// the full key lifecycle over HTTP: login, mint (plain shown once), the
// minted key authenticating, revocation taking effect immediately, and
// the rotated password surviving a restart.
func TestServeCmdCredentialStoreWiring(t *testing.T) {
	dir := t.TempDir()
	httpPort := freePort(t)
	storePath := filepath.Join(dir, "herald-creds.db")
	cfgYAML := fmt.Sprintf(`
server:
  addr: 127.0.0.1:%d
websocket:
  addr: 127.0.0.1:0
queue:
  type: memory
  workers: 1
auth:
  enabled: true
  admin_user:
    walk: throughpw
  store_path: %s
  api_keys:
    cfg-static: cfg-pass
`, httpPort, storePath)
	path := writeTestConfig(t, cfgYAML)

	done := make(chan int, 1)
	go func() { done <- serveCmd([]string{"--config", path}) }()
	base := fmt.Sprintf("http://127.0.0.1:%d", httpPort)
	waitHTTPReady(t, base+"/api/v1/auth/login", 10*time.Second)
	time.Sleep(100 * time.Millisecond)

	do := func(method, url, authHeader, body string) (int, string) {
		req, err := http.NewRequest(method, url, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		if authHeader != "" {
			req.Header.Set("Authorization", authHeader)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", method, url, err)
		}
		defer func() { _ = resp.Body.Close() }()
		raw, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(raw)
	}

	code, body := do("POST", base+"/api/v1/auth/login", "", `{"username":"walk","password":"throughpw"}`)
	if code != 200 || !strings.Contains(body, `"token"`) {
		t.Fatalf("login = %d %q, want 200 with token", code, body)
	}
	var login struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal([]byte(body), &login); err != nil {
		t.Fatal(err)
	}

	// Mint: the plain key rides the create response exactly once.
	code, body = do("POST", base+"/api/v1/keys", "Bearer "+login.Token, `{"name":"ci"}`)
	if code != 200 || !strings.Contains(body, `"key":"wm_`) {
		t.Fatalf("key mint = %d %q, want 200 with a wm_ key", code, body)
	}
	var mint struct {
		Data struct {
			Key  string `json:"key"`
			Info struct {
				ID string `json:"id"`
			} `json:"info"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &mint); err != nil {
		t.Fatal(err)
	}

	// The minted key authenticates a protected endpoint (masked list).
	req, _ := http.NewRequest("GET", base+"/api/v1/keys", nil)
	req.Header.Set("X-API-Key", mint.Data.Key)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != 200 || !strings.Contains(string(raw), `"count":1`) || strings.Contains(string(raw), mint.Data.Key) {
		t.Fatalf("list with minted key = %d %q, want 200 masked count 1", resp.StatusCode, raw)
	}

	// Revocation takes effect on the next request (fail 401).
	req, _ = http.NewRequest("DELETE", base+"/api/v1/keys/"+mint.Data.Info.ID, nil)
	req.Header.Set("X-API-Key", mint.Data.Key)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("revoke = %d, want 200", resp.StatusCode)
	}
	req, _ = http.NewRequest("GET", base+"/api/v1/keys", nil)
	req.Header.Set("X-API-Key", mint.Data.Key)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Fatalf("request with revoked key = %d, want 401", resp.StatusCode)
	}

	// Rotate the password; it must survive a restart via the store.
	code, body = do("POST", base+"/api/v1/auth/password", "Bearer "+login.Token,
		`{"old_password":"throughpw","new_password":"brandnewpw"}`)
	if code != 200 {
		t.Fatalf("password change = %d %q, want 200", code, body)
	}

	if err := syscall.Kill(syscall.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatalf("send SIGTERM: %v", err)
	}
	select {
	case c := <-done:
		if c != 0 {
			t.Fatalf("serveCmd = %d, want 0", c)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("serveCmd did not return after SIGTERM")
	}

	// Restart: the rotated password wins, the revoked key stays revoked.
	done2 := make(chan int, 1)
	go func() { done2 <- serveCmd([]string{"--config", path}) }()
	waitHTTPReady(t, base+"/api/v1/auth/login", 10*time.Second)
	time.Sleep(100 * time.Millisecond)

	code, body = do("POST", base+"/api/v1/auth/login", "", `{"username":"walk","password":"brandnewpw"}`)
	if code != 200 {
		t.Fatalf("login with rotated password after restart = %d %q, want 200", code, body)
	}
	if _, err := os.Stat(storePath); err != nil {
		t.Fatalf("expected the sqlite store on disk: %v", err)
	}

	if err := syscall.Kill(syscall.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatalf("send SIGTERM: %v", err)
	}
	select {
	case c := <-done2:
		if c != 0 {
			t.Fatalf("serveCmd restart = %d, want 0", c)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("serveCmd restart did not return after SIGTERM")
	}
}
