package main

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/cuihairu/herald/protocol"
	"github.com/gorilla/websocket"
)

// TestMainProcessSuccessPath builds the demo with `go build -cover`, runs it
// as a real child process under GOCOVERDIR, and asserts that main's success
// path was executed. The demo's CoreURL is the fixed ws://localhost:8081, so
// this test binds that port (skipping when it is already taken) and answers
// the register handshake; SIGTERM then cancels main's signal context, run
// returns nil, and the process exits through return so the coverage dump is
// flushed. The os.Exit(1) failure path cannot flush a dump and stays
// exempted.
func TestMainProcessSuccessPath(t *testing.T) {
	ln, err := net.Listen("tcp", "localhost:8081")
	if err != nil {
		t.Skipf("localhost:8081 is already taken, cannot host the demo control plane: %v", err)
	}
	srv := &http.Server{Handler: ackHandler()}
	go func() { _ = srv.Serve(ln) }()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}()

	bin := filepath.Join(t.TempDir(), "example-under-cover")
	build := exec.Command("go", "build", "-cover", "-coverpkg=./...", "-o", bin, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build -cover: %v\n%s", err, out)
	}

	covdir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin)
	cmd.WaitDelay = 5 * time.Second
	cmd.Env = append(os.Environ(), "GOCOVERDIR="+covdir)
	var logs strings.Builder
	cmd.Stdout = &logs
	cmd.Stderr = &logs
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}

	// Give the demo a moment to register, then shut it down the way an
	// operator would.
	time.Sleep(500 * time.Millisecond)
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		_ = cmd.Process.Kill()
		t.Fatalf("SIGTERM: %v", err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("child did not exit cleanly: %v; output: %q", err, logs.String())
	}
	if !strings.Contains(logs.String(), "Connected to Herald core") {
		t.Fatalf("demo never reported a successful registration; output: %q", logs.String())
	}
	assertMainCovered(t, covdir, "main.go", "github.com/cuihairu/herald/worker-sdk/go/example/main.go", mainExitAllow)
}

// ackHandler upgrades websocket connections and answers every register
// message with a success ack, dropping later frames (heartbeats).
func ackHandler() http.Handler {
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var envelope struct {
				Type string `json:"type"`
			}
			if err := json.Unmarshal(data, &envelope); err != nil || envelope.Type != protocol.MessageTypeRegister {
				continue
			}
			var msg protocol.RegisterMessage
			if err := json.Unmarshal(data, &msg); err != nil {
				continue
			}
			ack, _ := protocol.MarshalMessage(&protocol.RegisterAckMessage{
				WorkerID: msg.WorkerID,
				Success:  true,
				ServerID: "example-cover-test",
			})
			if err := conn.WriteMessage(websocket.TextMessage, ack); err != nil {
				return
			}
		}
	})
}

// mainExitAllow lists main's os.Exit line: exiting skips the GOCOVERDIR dump
// by design, so that statement is unmeasurable by the Go toolchain.
var mainExitAllow = map[int]string{}

func init() {
	src, err := os.ReadFile("main.go")
	if err == nil {
		if n := findLine(string(src), "os.Exit(1)"); n > 0 {
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
// asserts that every statement of main() is covered, except blocks whose
// span covers a line listed in allow.
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
	data, err := os.ReadFile(prof)
	if err != nil {
		t.Fatalf("read profile: %v", err)
	}

	src, err := os.ReadFile(mainFile)
	if err != nil {
		t.Fatalf("read source: %v", err)
	}
	start, end := mainFuncRange(string(src))

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
		t.Fatalf("no coverage blocks inside main() [%d,%d] in the dump", start, end)
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
