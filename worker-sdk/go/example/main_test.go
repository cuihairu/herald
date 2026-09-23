package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cuihairu/herald/core"
	"github.com/cuihairu/herald/protocol"
)

func TestDemoProviderDeliver(t *testing.T) {
	p := &DemoProvider{}
	task := &core.DeliveryTask{
		ID:       "task-demo",
		Provider: "demo",
		Targets:  []string{"user-1"},
	}
	if err := p.Deliver(context.Background(), task); err != nil {
		t.Errorf("Deliver() error = %v, want nil", err)
	}
}

func TestHandleDemoTask(t *testing.T) {
	// With content.
	task := &core.DeliveryTask{
		ID:       "task-1",
		Provider: "demo",
		Payload: core.DeliveryPayload{
			Content: &core.RenderedContent{Title: "hi", Body: "there"},
		},
	}
	if err := handleDemoTask(task); err != nil {
		t.Errorf("handleDemoTask(content) error = %v, want nil", err)
	}

	// Without content.
	if err := handleDemoTask(&core.DeliveryTask{ID: "task-2"}); err != nil {
		t.Errorf("handleDemoTask(no content) error = %v, want nil", err)
	}
}

func TestDemoCallbacks(t *testing.T) {
	onDemoEvent(&protocol.EventMessage{EventType: "task.created"})
	onDemoConnect()
	onDemoDisconnect(errors.New("conn lost"))
	onDemoStateChange(protocol.StateReady)
}

func TestRunReturnsWhenCtxCanceled(t *testing.T) {
	// The SDK register step is currently a stub that succeeds without a real
	// connection, so run() must block until ctx is canceled and then return
	// nil.
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	if err := run(ctx); err != nil {
		t.Errorf("run() error = %v, want nil after ctx cancel", err)
	}
}
