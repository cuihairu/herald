package main

import (
	"context"
	"fmt"
	"io"
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
	// A store that parses but holds an invalid group trips the reload
	// (validation), not the open.
	badGroups := filepath.Join(dir, "groups-invalid.json")
	if err := os.WriteFile(badGroups, []byte(`{"version":1,"groups":[{"id":"bad","members":[]}]}`), 0o600); err != nil {
		t.Fatalf("write bad groups store: %v", err)
	}
	// Same shape for rosters: parses, then fails validation on reload.
	badRosters := filepath.Join(dir, "rosters-invalid.json")
	if err := os.WriteFile(badRosters, []byte(`{"version":1,"rosters":[{"id":"bad","periods":[]}]}`), 0o600); err != nil {
		t.Fatalf("write bad rosters store: %v", err)
	}
	cases := map[string]string{
		// A rules store pointing at a directory cannot be opened.
		"rules store is a dir": fmt.Sprintf("rules_store: %s\n", dir),
		// Only memory and redis are valid rule-state backends.
		"unknown rules state": "rules_state:\n  type: etcd\n",
		// An unreachable redis fails the state store at startup.
		"dead redis state": "rules_state:\n  type: redis\n  addr: 127.0.0.1:1\n",
		// A rule that does not compile is rejected at load time.
		"invalid rule": "rules:\n  - id: bad\n    match: \"&&&\"\n    route:\n      - channels: [log]\n",
		// The default policy must name a known value.
		"unknown rules policy": "rules_default_policy: sometimes\n",
		// deny is a valid policy (the switch arm must run); the broken
		// state store fails the startup right after it.
		"deny rules policy": "rules_default_policy: deny\nrules_state:\n  type: etcd\n",
		// A groups store pointing at a directory cannot be opened.
		"groups store is a dir": fmt.Sprintf("groups_store: %s\n", dir),
		// A groups store that opens but fails validation aborts the load.
		"groups reload failure": fmt.Sprintf("groups_store: %s\n", badGroups),
		// A group that does not validate is rejected at load time.
		"invalid group": "groups:\n  - id: bad\n    members: []\n",
		// A rosters store pointing at a directory cannot be opened.
		"rosters store is a dir": fmt.Sprintf("rosters_store: %s\n", dir),
		// A rosters store that opens but fails validation aborts the load.
		"rosters reload failure": fmt.Sprintf("rosters_store: %s\n", badRosters),
		// An audience referencing an unknown recipient is configuration
		// drift and refuses to start (user-level audience tables).
		"audience references unknown recipient": "audiences:\n  ops:\n    recipients: [ghost]\n",
		// A recipient with no endpoints can receive nothing and must not
		// start either.
		"recipient without endpoints": "recipients:\n  alice:\n    endpoints: []\n",
		// A channel that lists no providers can deliver nothing and must
		// not start.
		"channel without providers": "providers:\n  webhook:\n    type: webhook\nchannels:\n  ci:\n    providers: []\n",
		// A channel referencing a provider that is not configured is
		// configuration drift and refuses to start.
		"channel references unknown provider": "channels:\n  ci:\n    providers: [ghost]\n",
		// Digest wiring (关系详设 §10): an unknown timezone, a broken flip
		// schedule and an unreachable lease redis are all startup errors —
		// the aggregator must not half-start.
		"digest location invalid":  "digest:\n  enabled: true\n  location: Bogus/Zone\n",
		"digest daily invalid":     "digest:\n  enabled: true\n  daily: \"2500:00\"\n",
		"digest weekly invalid":    "digest:\n  enabled: true\n  weekly: \"Funday 09:00\"\n",
		"digest redis unreachable": "digest:\n  enabled: true\n  redis_addr: 127.0.0.1:1\n",
		// 来源适配器 (§8): a reconcile probe whose provider config cannot
		// build (missing bot token) and an unreachable lease redis are
		// both startup errors — the sweep must not half-start.
		"source reconcile probe invalid":     "sources:\n  enabled: true\n  reconcile:\n    enabled: true\nproviders:\n  bot:\n    type: telegram\n    config: {}\n",
		"source reconcile redis unreachable": "sources:\n  enabled: true\n  reconcile:\n    enabled: true\ndigest:\n  redis_addr: 127.0.0.1:1\n",
		// §6: an unparseable taxonomy override refuses to start where the
		// policy is built, never a silent re-grade.
		"delivery urgency invalid": "delivery:\n  category_urgency:\n    alerts: hourly\n",
		// §13.1: a token outside the config/trigger/query vocabulary
		// refuses the whole registry at startup.
		"app scope invalid": "apps:\n  demo-app:\n    tokens:\n      - secret: s\n        scopes: [sudo]\n",
		// §13.5: the callback dispatcher's name is reserved — a config
		// provider claiming it collides at registration and refuses the
		// start.
		"callback provider name reserved": "providers:\n  app-callback:\n    type: webhook\n    config:\n      url: https://example.com/hook\n",
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
// subsystem enabled — persistent rules, redis rule state, persistent
// notification groups, the user-level audience tables, card-callback
// encryption, a rate limit, and a broken escalation store (which must
// only be logged) — and shuts it down via SIGTERM.
func TestServeCmdFullFeaturedLifecycle(t *testing.T) {
	httpPort := freePort(t)
	wsPort := freePort(t)
	mr := miniredis.RunT(t)
	rulesPath := filepath.Join(t.TempDir(), "rules.json")
	groupsPath := filepath.Join(t.TempDir(), "groups.json")
	// The rosters store starts absent (an external scheduler pushes through
	// the API); opening and reloading an empty file must still succeed.
	rostersPath := filepath.Join(t.TempDir(), "rosters.json")
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
groups_store: %s
rosters_store: %s
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
channels:
  ci:
    providers: [hook]
rules:
  - id: r-hook
    match: "type == 'quickstart'"
    route:
      - channels: [hook]
groups:
  - id: ops-oncall
    description: the webhook audience
    members:
      - channel: hook
recipients:
  alice:
    endpoints:
      - type: hook
        target: alice
audiences:
  ops:
    recipients: [alice]
apps:
  demo-app:
    tokens:
      - secret: demo-app-lifecycle
        scopes: [config, trigger, query]
`, httpPort, wsPort, rulesPath, mr.Addr(), groupsPath, rostersPath, escStore)
	path := writeTestConfig(t, cfgYAML)

	done := make(chan int, 1)
	go func() { done <- serveCmd([]string{"--config", path}) }()

	waitHTTPReady(t, fmt.Sprintf("http://127.0.0.1:%d/", httpPort), 10*time.Second)
	time.Sleep(200 * time.Millisecond)

	// §13.1 face: the seeded namespace answers an app-token
	// introspection read before the operator surface shuts it down.
	req, err := http.NewRequest("GET", fmt.Sprintf("http://127.0.0.1:%d/api/v1/apps/demo-app", httpPort), nil)
	if err != nil {
		t.Fatalf("build app request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer demo-app-lifecycle")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("app introspection: %v", err)
	}
	bodyBytes, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != 200 || !strings.Contains(string(bodyBytes), "demo-app") || !strings.Contains(string(bodyBytes), "query") {
		t.Fatalf("app introspection = %d %q, want 200 with demo-app scopes", resp.StatusCode, bodyBytes)
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

	// The persistent rules store must hold the configured rule.
	data, err := os.ReadFile(rulesPath)
	if err != nil || !strings.Contains(string(data), "r-hook") {
		t.Fatalf("rules store not persisted: %q / %v", data, err)
	}
	// The persistent groups store must hold the configured group.
	gdata, err := os.ReadFile(groupsPath)
	if err != nil || !strings.Contains(string(gdata), "ops-oncall") {
		t.Fatalf("groups store not persisted: %q / %v", gdata, err)
	}
}

// TestServeCmdDigestLifecycle starts the server with the digest flip
// loop enabled and the leader lease pointed at redis, omitting every
// optional digest field so the defaults run (Asia/Shanghai, 09:00 /
// Mon 09:00, a 1m tick and a 60s lease), then shuts down via SIGTERM —
// the lease must be released on the way out.
func TestServeCmdDigestLifecycle(t *testing.T) {
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
digest:
  enabled: true
  redis_addr: %s
`, httpPort, wsPort, mr.Addr())
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
	// The lease is released on shutdown: the key must be gone.
	if mr.Exists("herald:digest:leader") {
		t.Error("digest leader lease still held after shutdown")
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

// TestServeCmdAdminLoginWiring boots the server with auth.admin_user and
// walks POST /api/v1/auth/login: the configured account must authenticate
// and return a token, and a wrong password must 401. The heraldd wiring
// used to drop AdminUser (and SecretKey) when constructing auth.New, so
// every configured Dashboard login silently failed with 401.
func TestServeCmdAdminLoginWiring(t *testing.T) {
	httpPort := freePort(t)
	cfgYAML := fmt.Sprintf(`
server:
  addr: 127.0.0.1:%d
queue:
  type: memory
  workers: 1
auth:
  enabled: true
  api_keys:
    "hk-walkthrough": "acceptance"
  admin_user:
    walk: throughpw
`, httpPort)
	path := writeTestConfig(t, cfgYAML)

	done := make(chan int, 1)
	go func() { done <- serveCmd([]string{"--config", path}) }()
	waitHTTPReady(t, fmt.Sprintf("http://127.0.0.1:%d/api/v1/auth/login", httpPort), 10*time.Second)
	time.Sleep(100 * time.Millisecond)

	base := fmt.Sprintf("http://127.0.0.1:%d/api/v1/auth/login", httpPort)
	post := func(body string) (int, string) {
		resp, err := http.Post(base, "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatalf("login post %q: %v", body, err)
		}
		defer func() { _ = resp.Body.Close() }()
		raw, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(raw)
	}

	code, body := post(`{"username":"walk","password":"throughpw"}`)
	if code != 200 || !strings.Contains(body, `"token"`) {
		t.Fatalf("configured login = %d %q, want 200 with token", code, body)
	}
	code, body = post(`{"username":"walk","password":"wrong"}`)
	if code != 401 || !strings.Contains(body, "invalid username or password") {
		t.Fatalf("wrong password = %d %q, want 401", code, body)
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

	// Cancel before the reader starts so its first loop iteration observes
	// the cancelled context. Cancelling after launching the goroutine would
	// race with its first ReadMessage: nothing wakes a read in progress
	// (production shuts down by closing the conn, covered by
	// TestReadRemoteWorkerMessages), so a reader that wins the race blocks
	// until the deadline and fails the test. With the context cancelled
	// first, the exit path is deterministic and the conn is never touched.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan struct{})
	go func() {
		readRemoteWorkerMessages(ctx, conn)
		close(done)
	}()
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

	// HERALD_MAIN_COVERDIR (CI coverage-merge flow): keep the converted dump
	// outside t.TempDir() so tools/covermerge.py can fold these subprocess
	// counts into the gate profile.
	if dir := os.Getenv("HERALD_MAIN_COVERDIR"); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("HERALD_MAIN_COVERDIR mkdir: %v", err)
		}
		pkg := strings.TrimSuffix(importPath, "/main.go")
		out := filepath.Join(dir, pkg[strings.LastIndex(pkg, "/")+1:]+".cov")
		if data, err := os.ReadFile(prof); err != nil {
			t.Fatalf("read converted profile: %v", err)
		} else if err := os.WriteFile(out, data, 0o644); err != nil {
			t.Fatalf("persist converted profile: %v", err)
		}
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
