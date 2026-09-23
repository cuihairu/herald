package sdk

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cuihairu/herald/core"
)

func TestRunAckErrorPath(t *testing.T) {
	q := newFakeQueue()
	q.ackErr = errors.New("ack boom")
	c := NewClient(testConfig("worker-ackerr"), q)

	handled := make(chan string, 1)
	c.OnTask(func(task *core.DeliveryTask) error {
		handled <- task.ID
		return nil
	})
	connected := make(chan struct{}, 1)
	c.OnConnect(func() { connected <- struct{}{} })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- c.Run(ctx) }()

	select {
	case <-connected:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for connect callback")
	}

	q.push(t, &core.DeliveryTask{ID: "task-ack-err"})
	select {
	case got := <-q.acks:
		if got != "task-ack-err" {
			t.Errorf("acked = %q, want task-ack-err", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for ack attempt")
	}

	// Let the consume loop observe the ack error, then shut down.
	time.Sleep(100 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Run() = %v, want nil", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for Run to return")
	}
}
