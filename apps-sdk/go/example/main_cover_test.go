package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestMainProcessSuccessPath builds the demo with `go build -cover`, runs
// it as a real child process under GOCOVERDIR pointed at an in-test stub
// herald, and asserts that main's success path executed. The demo is
// short-lived — the child exits through plain return once the §13 walk
// completes, so the coverage dump flushes without any signal
// choreography. The os.Exit(1) failure path cannot flush a dump and
// stays exempted.
func TestMainProcessSuccessPath(t *testing.T) {
	s := newDemoStub(t, "", []string{"config", "trigger", "query"})

	bin := filepath.Join(t.TempDir(), "example-under-cover")
	build := exec.Command("go", "build", "-cover", "-coverpkg=./...", "-o", bin, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build -cover: %v\n%s", err, out)
	}

	covdir := t.TempDir()
	cmd := exec.Command(bin)
	cmd.Env = append(os.Environ(),
		"GOCOVERDIR="+covdir,
		"HERALD_URL="+s.server.URL,
		"HERALD_TOKEN=demo-token",
	)
	var logs strings.Builder
	cmd.Stdout = &logs
	cmd.Stderr = &logs
	if err := cmd.Run(); err != nil {
		t.Fatalf("child run: %v\n%s", err, logs.String())
	}
	if !strings.Contains(logs.String(), "app=demo-app") {
		t.Fatalf("child never reported the self-check; output:\n%s", logs.String())
	}
	assertMainCovered(t, covdir, "main.go", "github.com/cuihairu/herald/apps-sdk/go/example/main.go", mainExitAllow)
}

// mainExitAllow lists main's os.Exit line: exiting skips the GOCOVERDIR
// dump by design, so that statement is unmeasurable by the Go toolchain.
var mainExitAllow = map[int]string{}

func init() {
	src, err := os.ReadFile("main.go")
	if err == nil {
		if n := findLine(string(src), "os.Exit(1)"); n > 0 {
			mainExitAllow[n] = "os.Exit skips the GOCOVERDIR dump"
		}
	}
}

// assertMainCovered converts the child's GOCOVERDIR dump to a profile,
// persists it for the covermerge step when HERALD_MAIN_COVERDIR is set,
// and fails if any main() block stayed zero beyond the allowed lines.
func assertMainCovered(t *testing.T, covdir, mainFile, importPath string, allow map[int]string) {
	t.Helper()

	prof := filepath.Join(t.TempDir(), "main.cov")
	conv := exec.Command("go", "tool", "covdata", "textfmt", "-i", covdir, "-o", prof)
	if out, err := conv.CombinedOutput(); err != nil {
		t.Fatalf("covdata textfmt: %v\n%s", err, out)
	}

	// HERALD_MAIN_COVERDIR (CI coverage-merge flow): keep the converted
	// dump outside t.TempDir() so tools/covermerge.py can fold these
	// subprocess counts into the gate profile. The file name takes the
	// last three import-path segments: the plain basename ("example")
	// collides with the worker-sdk demo's dump.
	if dir := os.Getenv("HERALD_MAIN_COVERDIR"); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("HERALD_MAIN_COVERDIR mkdir: %v", err)
		}
		pkg := strings.TrimSuffix(importPath, "/main.go")
		parts := strings.Split(pkg, "/")
		out := filepath.Join(dir, strings.Join(parts[len(parts)-3:], "-")+".cov")
		data, err := os.ReadFile(prof)
		if err != nil {
			t.Fatalf("read converted profile: %v", err)
		}
		if err := os.WriteFile(out, data, 0o644); err != nil {
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

func findLine(src, needle string) int {
	for i, l := range strings.Split(src, "\n") {
		if strings.Contains(l, needle) {
			return i + 1
		}
	}
	return -1
}

func atoi(t *testing.T, s string) int {
	t.Helper()
	n := 0
	for _, c := range s {
		n = n*10 + int(c-'0')
	}
	return n
}
