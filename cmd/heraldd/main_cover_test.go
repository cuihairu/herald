package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	gws "github.com/gorilla/websocket"
)

// TestServeCmdStartupFailures drives each startup-time rejection with a
// minimal config: every case must exit with code 1 before binding anything.
func TestServeCmdStartupFailures(t *testing.T) {
	dir := t.TempDir()
	cases := map[string]string{
		// A rules store pointing at a directory cannot be opened.
		"rules store is a dir": fmt.Sprintf("rules_store: %s\n", dir),
		// Only memory and redis are valid rule-state backends.
		"unknown rules state": "rules_state:\n  type: etcd\n",
		// An unreachable redis fails the state store at startup.
		"dead redis state": "rules_state:\n  type: redis\n  addr: 127.0.0.1:1\n",
		// A rule that does not compile is rejected at load time.
		"invalid rule": "rules:\n  - id: bad\n    match: \"&&&\"\n    route:\n      - channels: [log]\n",
	}
	for name, cfgYAML := range cases {
		t.Run(name, func(t *testing.T) {
			path := writeTestConfig(t, cfgYAML)
			if code := serveCmd([]string{"--config", path}); code != 1 {
				t.Fatalf("serveCmd(%s) = %d, want 1", name, code)
			}
		})
	}
}

// TestServeCmdFullFeaturedLifecycle starts the server with every optional
// subsystem enabled — persistent rules, redis rule state, card-callback
// encryption, a rate limit, and a broken escalation store (which must only
// be logged) — and shuts it down via SIGTERM.
func TestServeCmdFullFeaturedLifecycle(t *testing.T) {
	httpPort := freePort(t)
	wsPort := freePort(t)
	mr := miniredis.RunT(t)
	rulesPath := filepath.Join(t.TempDir(), "rules.json")
	// A directory as escalation store makes Restore fail; the server must
	// come up anyway.
	escStore := t.TempDir()

	cfgYAML := fmt.Sprintf(`
server:
  addr: 127.0.0.1:%d
websocket:
  addr: 127.0.0.1:%d
queue:
  type: memory
  workers: 1
rules_store: %s
rules_state:
  type: redis
  addr: %s
escalation_store: %s
card_callback:
  encrypt_key: test-encrypt-key
providers:
  hook:
    type: webhook
    config:
      url: http://127.0.0.1:1/hook
    rate_limit:
      type: token_bucket
      rate: 10
      burst: 10
rules:
  - id: r-hook
    match: "type == 'quickstart'"
    route:
      - channels: [hook]
`, httpPort, wsPort, rulesPath, mr.Addr(), escStore)
	path := writeTestConfig(t, cfgYAML)

	done := make(chan int, 1)
	go func() { done <- serveCmd([]string{"--config", path}) }()

	waitHTTPReady(t, fmt.Sprintf("http://127.0.0.1:%d/", httpPort), 10*time.Second)
	time.Sleep(200 * time.Millisecond)
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

	// The persistent rules store must hold the configured rule.
	data, err := os.ReadFile(rulesPath)
	if err != nil || !strings.Contains(string(data), "r-hook") {
		t.Fatalf("rules store not persisted: %q / %v", data, err)
	}
}

// TestServeCmdWebSocketPortTaken covers the websocket server failing to
// bind: the error cancels the run context, and the process still exits
// through its normal signal path.
func TestServeCmdWebSocketPortTaken(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()
	wsPort := ln.Addr().(*net.TCPAddr).Port
	httpPort := freePort(t)

	cfgYAML := fmt.Sprintf(`
server:
  addr: 127.0.0.1:%d
websocket:
  addr: 127.0.0.1:%d
queue:
  type: memory
  workers: 1
`, httpPort, wsPort)
	path := writeTestConfig(t, cfgYAML)

	done := make(chan int, 1)
	go func() { done <- serveCmd([]string{"--config", path}) }()

	// Give the websocket server time to hit the taken port, then tear the
	// process down the way the signal handler expects.
	time.Sleep(300 * time.Millisecond)
	if err := syscall.Kill(syscall.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatalf("send SIGTERM: %v", err)
	}
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("serveCmd = %d, want 0 after websocket bind failure", code)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("serveCmd did not exit after the websocket bind failure")
	}
}

// TestReadRemoteWorkerMessagesCancel covers the context-cancelled exit of
// the reader loop while the connection is still alive.
func TestReadRemoteWorkerMessagesCancel(t *testing.T) {
	up := gws.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		// Hold the connection open until the test tears down.
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}))
	t.Cleanup(server.Close)
	url := "ws" + strings.TrimPrefix(server.URL, "http")

	conn, _, err := gws.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		readRemoteWorkerMessages(ctx, conn)
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("readRemoteWorkerMessages did not return after cancel")
	}
}

// TestMainProcessSuccessPath builds the daemon with `go build -cover`, runs
// it as a real child process under GOCOVERDIR, and asserts after a clean
// SIGTERM shutdown that main's success-path statements were executed. The
// process exits via return (not os.Exit) on success, so the coverage data
// reaches the dump directory.
func TestMainProcessSuccessPath(t *testing.T) {
	httpPort := freePort(t)
	wsPort := freePort(t)
	cfg := writeTestConfig(t, fmt.Sprintf(`
server:
  addr: 127.0.0.1:%d
websocket:
  addr: 127.0.0.1:%d
queue:
  type: memory
  workers: 1
`, httpPort, wsPort))

	bin := filepath.Join(t.TempDir(), "heraldd-under-cover")
	build := exec.Command("go", "build", "-cover", "-coverpkg=./...", "-o", bin, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build -cover: %v\n%s", err, out)
	}

	covdir := t.TempDir()
	cmd := exec.Command(bin, "serve", "--config", cfg)
	cmd.Env = append(os.Environ(), "GOCOVERDIR="+covdir)
	var logs strings.Builder
	cmd.Stdout = &logs
	cmd.Stderr = &logs
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	// Inline readiness polling (instead of waitHTTPReady) so a timeout can
	// dump the child output — the only way to see why it never came up.
	url := fmt.Sprintf("http://127.0.0.1:%d/", httpPort)
	deadline := time.Now().Add(10 * time.Second)
	for {
		resp, err := http.Get(url)
		if err == nil {
			_ = resp.Body.Close()
			break
		}
		if time.Now().After(deadline) {
			_ = cmd.Process.Signal(syscall.SIGTERM)
			_ = cmd.Wait()
			t.Fatalf("server never became ready; child output: %q", logs.String())
		}
		time.Sleep(100 * time.Millisecond)
	}
	time.Sleep(200 * time.Millisecond)
	if err := syscall.Kill(cmd.Process.Pid, syscall.SIGTERM); err != nil {
		t.Fatalf("SIGTERM: %v", err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("child did not exit cleanly: %v; output: %q", err, logs.String())
	}

	// The os.Exit(error-code) line cannot flush a profile dump, so it is
	// the only allowed uncovered statement in main.
	assertMainCovered(t, covdir, "main.go", "github.com/cuihairu/herald/cmd/heraldd/main.go", mainExitAllow)
}

// mainExitAllow lists main's os.Exit line: exiting skips the profile dump
// by design, so that statement is unmeasurable by the Go toolchain.
var mainExitAllow = map[int]string{}

func init() {
	src, err := os.ReadFile("main.go")
	if err == nil {
		if n := findLine(string(src), "os.Exit(code)"); n > 0 {
			mainExitAllow[n] = "os.Exit skips the GOCOVERDIR dump"
		}
	}
}

func findLine(src, needle string) int {
	for i, l := range strings.Split(src, "\n") {
		if strings.Contains(l, needle) {
			return i + 1
		}
	}
	return 0
}

// assertMainCovered converts the GOCOVERDIR dump to a text profile and
// asserts that every statement of main() is covered, except lines listed in
// allow (the os.Exit error path cannot flush a profile).
func assertMainCovered(t *testing.T, covdir, mainFile, importPath string, allow map[int]string) {
	t.Helper()

	prof := filepath.Join(t.TempDir(), "main.cov")
	conv := exec.Command("go", "tool", "covdata", "textfmt", "-i", covdir, "-o", prof)
	if out, err := conv.CombinedOutput(); err != nil {
		t.Fatalf("covdata textfmt: %v\n%s", err, out)
	}

	src, err := os.ReadFile(mainFile)
	if err != nil {
		t.Fatalf("read source: %v", err)
	}
	start, end := mainFuncRange(string(src))

	data, err := os.ReadFile(prof)
	if err != nil {
		t.Fatalf("read profile: %v", err)
	}

	re := regexp.MustCompile(regexp.QuoteMeta(importPath) + `:(\d+)\.\d+,(\d+)\.\d+ (\d+) (\d+)`)
	seen := false
	for _, line := range strings.Split(string(data), "\n") {
		m := re.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		ln, endLn, cnt := atoi(t, m[1]), atoi(t, m[2]), atoi(t, m[4])
		if ln < start || ln > end {
			continue
		}
		if cnt == 0 {
			// A block whose span covers an allowed line (the os.Exit
			// statement) is exempt as a whole.
			exempt := false
			for allowLn := range allow {
				if ln <= allowLn && allowLn <= endLn {
					exempt = true
					break
				}
			}
			if exempt {
				continue
			}
			t.Errorf("main:%d was not covered by the child run", ln)
		}
		seen = true
	}
	if !seen {
		data, _ := os.ReadFile(prof)
		var mainLines []string
		for _, line := range strings.Split(string(data), "\n") {
			if strings.Contains(line, "cmd/heraldd/main.go") {
				mainLines = append(mainLines, line)
			}
		}
		t.Fatalf("no coverage blocks inside main() [%d,%d]; all main.go blocks: %v", start, end, mainLines)
	}
}

// mainFuncRange returns the [start, end] line numbers of func main.
func mainFuncRange(src string) (int, int) {
	lines := strings.Split(src, "\n")
	start, depth := 0, 0
	for i, l := range lines {
		if start == 0 && strings.HasPrefix(l, "func main()") {
			start = i + 1
			depth = 1
			continue
		}
		if start != 0 {
			depth += strings.Count(l, "{") - strings.Count(l, "}")
			if depth == 0 {
				return start, i + 1
			}
		}
	}
	return start, start
}

func atoi(t *testing.T, s string) int {
	t.Helper()
	n := 0
	for _, c := range s {
		n = n*10 + int(c-'0')
	}
	return n
}
