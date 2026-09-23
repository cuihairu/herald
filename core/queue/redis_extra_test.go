package queue

import (
	"context"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/cuihairu/herald/core"
)

func TestRedisQueuePushMarshalError(t *testing.T) {
	fr := newFakeRedis(t)
	q := newTestRedisQueue(t, fr)

	// A NaN inside the raw payload cannot be encoded as JSON, so Push must
	// fail before any command reaches the server.
	task := &core.DeliveryTask{
		ID:       "task-nan",
		Provider: "email",
		Payload:  core.DeliveryPayload{Raw: map[string]any{"v": math.NaN()}},
	}
	err := q.Push(context.Background(), task)
	if err == nil || !strings.Contains(err.Error(), "failed to marshal task") {
		t.Errorf("Push(NaN payload) error = %v, want marshal failure", err)
	}
}

// popWithCtxCancelInFlight drives one Pop whose XREADGROUP reply is held by
// the fake until the caller's context is already cancelled, then releases it.
// go-redis reads the reply with a detached context, so the select inside Pop
// must deterministically take the ctx.Done branch.
func popWithCtxCancelInFlight(t *testing.T, emptyStreamsReply bool) error {
	t.Helper()

	fr := newFakeRedis(t)
	q := newTestRedisQueue(t, fr)

	gate := make(chan struct{})
	fr.holdXRead = gate
	arrived := make(chan struct{})
	fr.xreadArrived = arrived

	if emptyStreamsReply {
		fr.setFailFlags(false, false, false, true)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	type popResult struct {
		err error
	}
	done := make(chan popResult, 1)
	go func() {
		_, err := q.Pop(ctx)
		done <- popResult{err: err}
	}()

	select {
	case <-arrived:
	case <-time.After(3 * time.Second):
		t.Fatal("XREADGROUP never reached the fake server")
	}

	cancel()    // arm ctx.Done while the reply is still held
	close(gate) // release the reply

	select {
	case r := <-done:
		return r.err
	case <-time.After(3 * time.Second):
		t.Fatal("Pop did not return after gate release")
		return nil
	}
}

func TestRedisQueuePopCanceledCtxRedisNil(t *testing.T) {
	// Empty stream → the fake replies with a RESP null array, which go-redis
	// surfaces as redis.Nil; Pop's select must take the ctx.Done branch.
	err := popWithCtxCancelInFlight(t, false)
	if err != context.Canceled {
		t.Errorf("Pop() error = %v, want context.Canceled", err)
	}
}

func TestRedisQueuePopCanceledCtxEmptyStreams(t *testing.T) {
	// Fake replies with an empty streams array; Pop's select on
	// len(streams)==0 must take the ctx.Done branch.
	err := popWithCtxCancelInFlight(t, true)
	if err != context.Canceled {
		t.Errorf("Pop() error = %v, want context.Canceled", err)
	}
}
