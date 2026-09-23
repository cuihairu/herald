package api

import (
	"testing"

	"github.com/cuihairu/herald/core/queue"
	"github.com/cuihairu/herald/core/runtime"
	"github.com/cuihairu/herald/core/template"
	"github.com/cuihairu/herald/core/worker"
)

func TestNewHandler(t *testing.T) {
	t.Run("nil template manager creates default", func(t *testing.T) {
		h := NewHandler(nil, nil, nil)
		if h == nil {
			t.Fatal("expected non-nil handler")
		}
		if h.GetTemplateManager() == nil {
			t.Error("expected non-nil template manager")
		}
	})

	t.Run("keeps provided dependencies", func(t *testing.T) {
		rt := runtime.NewManager(10)
		tm := template.NewManager()
		h := NewHandler(nil, rt, tm)
		if h.runtime != rt {
			t.Error("expected runtime manager to be kept")
		}
		if h.GetTemplateManager() != tm {
			t.Error("expected template manager to be kept")
		}
	})
}

func TestHandler_Setters(t *testing.T) {
	h := NewHandler(nil, nil, nil)

	h.SetWebSocketServer(nil)
	if h._wsServer != nil {
		t.Error("expected nil websocket server")
	}

	reg := worker.NewRegistry()
	h.SetWorkerRegistry(reg)
	if h.workerRegistry != reg {
		t.Error("expected worker registry to be set")
	}

	if h.getQueue() != nil {
		t.Error("expected nil queue by default")
	}

	q, err := queue.NewMemoryQueue(&queue.QueueConfig{})
	if err != nil {
		t.Fatalf("failed to create queue: %v", err)
	}
	h.SetQueue(q)
	if h.getQueue() != q {
		t.Error("expected queue to be set")
	}
}
