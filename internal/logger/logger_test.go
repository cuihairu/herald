package logger

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func newBufferLogger(buf *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))
}

func TestDefault(t *testing.T) {
	if Default() == nil {
		t.Fatal("expected non-nil default logger")
	}
}

func TestSetDefault(t *testing.T) {
	original := Default()
	defer SetDefault(original)

	var buf bytes.Buffer
	l := newBufferLogger(&buf)
	SetDefault(l)
	if Default() != l {
		t.Error("expected Default to return the logger set by SetDefault")
	}
}

func TestInfo(t *testing.T) {
	original := Default()
	defer SetDefault(original)

	var buf bytes.Buffer
	SetDefault(newBufferLogger(&buf))

	Info("info message", "key", "value")
	if !strings.Contains(buf.String(), "level=INFO") {
		t.Errorf("expected INFO level in output, got %s", buf.String())
	}
	if !strings.Contains(buf.String(), "msg=\"info message\"") {
		t.Errorf("expected message in output, got %s", buf.String())
	}
	if !strings.Contains(buf.String(), "key=value") {
		t.Errorf("expected key=value in output, got %s", buf.String())
	}
}

func TestError(t *testing.T) {
	original := Default()
	defer SetDefault(original)

	var buf bytes.Buffer
	SetDefault(newBufferLogger(&buf))

	Error("error message")
	if !strings.Contains(buf.String(), "level=ERROR") {
		t.Errorf("expected ERROR level in output, got %s", buf.String())
	}
	if !strings.Contains(buf.String(), "msg=\"error message\"") {
		t.Errorf("expected message in output, got %s", buf.String())
	}
}

func TestDebug(t *testing.T) {
	original := Default()
	defer SetDefault(original)

	var buf bytes.Buffer
	SetDefault(newBufferLogger(&buf))

	Debug("debug message")
	if !strings.Contains(buf.String(), "level=DEBUG") {
		t.Errorf("expected DEBUG level in output, got %s", buf.String())
	}
}

func TestWarn(t *testing.T) {
	original := Default()
	defer SetDefault(original)

	var buf bytes.Buffer
	SetDefault(newBufferLogger(&buf))

	Warn("warn message")
	if !strings.Contains(buf.String(), "level=WARN") {
		t.Errorf("expected WARN level in output, got %s", buf.String())
	}
}

func TestWith(t *testing.T) {
	original := Default()
	defer SetDefault(original)

	var buf bytes.Buffer
	SetDefault(newBufferLogger(&buf))

	l := With("component", "test")
	if l == nil {
		t.Fatal("expected non-nil logger")
	}
	l.Info("with message")
	if !strings.Contains(buf.String(), "component=test") {
		t.Errorf("expected component=test in output, got %s", buf.String())
	}
	if !strings.Contains(buf.String(), "msg=\"with message\"") {
		t.Errorf("expected message in output, got %s", buf.String())
	}
}
