package queue

import (
	"context"
	"math"
	"strings"
	"testing"

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
