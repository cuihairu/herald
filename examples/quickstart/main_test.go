package main

import (
	"context"
	"testing"
	"time"
)

// TestRunDelivers exercises the whole quickstart path: config, App
// construction, dispatch, background delivery and graceful Close.
func TestRunDelivers(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := run(ctx); err != nil {
		t.Fatalf("run() error = %v", err)
	}
}

// TestRunCanceledContext covers the error path: a dead context makes the
// synchronous dispatch give up, and run reports the failure.
func TestRunCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := run(ctx); err == nil {
		t.Error("run() with canceled context should fail")
	}
}
