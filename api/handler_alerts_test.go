package api

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cuihairu/herald/core/ack"
	"github.com/cuihairu/herald/core/escalation"
)

func withAckStore(s ack.Store) func(*Config) {
	return func(c *Config) { c.AckStore = s }
}

func TestHandleAlertsWithoutStore(t *testing.T) {
	env := newTestEnv(t) // no AckStore configured

	for _, tc := range []struct {
		name   string
		method string
		path   string
	}{
		{"status", http.MethodGet, "/api/v1/alerts/a1"},
		{"ack", http.MethodPost, "/api/v1/alerts/a1/ack"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, resp := env.do(t, tc.method, tc.path, "{}", nil)
			if code != http.StatusServiceUnavailable {
				t.Fatalf("expected status 503, got %d", code)
			}
			if resp["message"] != "ack store is not configured" {
				t.Errorf("unexpected message: %v", resp["message"])
			}
		})
	}
}

func TestHandleAlertAckLifecycle(t *testing.T) {
	env := newTestEnv(t, withAckStore(ack.NewMemoryStore()))

	// Before any ack the alert reads as unacknowledged.
	code, resp := env.do(t, http.MethodGet, "/api/v1/alerts/incident-1", "", nil)
	if code != http.StatusOK {
		t.Fatalf("status before ack: %d", code)
	}
	data := dataOf(t, resp)
	if data["acknowledged"] != false {
		t.Fatalf("expected acknowledged=false, got %v", data["acknowledged"])
	}

	// Record the ack.
	code, resp = env.do(t, http.MethodPost, "/api/v1/alerts/incident-1/ack", `{"acked_by":"alice"}`, nil)
	if code != http.StatusOK {
		t.Fatalf("ack failed with status %d", code)
	}
	rec := dataOf(t, resp)
	if rec["alert_id"] != "incident-1" || rec["acked_by"] != "alice" || rec["source"] != "api" {
		t.Fatalf("unexpected ack record: %v", rec)
	}

	// Status now reads as acknowledged.
	_, resp = env.do(t, http.MethodGet, "/api/v1/alerts/incident-1", "", nil)
	data = dataOf(t, resp)
	if data["acknowledged"] != true {
		t.Fatalf("expected acknowledged=true, got %v", data["acknowledged"])
	}
	stored, _ := data["ack"].(map[string]any)
	if stored == nil || stored["acked_by"] != "alice" {
		t.Fatalf("expected embedded ack record, got %v", stored)
	}

	// A second ack is idempotent: the first record wins.
	code, resp = env.do(t, http.MethodPost, "/api/v1/alerts/incident-1/ack", `{"acked_by":"bob"}`, nil)
	if code != http.StatusOK {
		t.Fatalf("second ack failed with status %d", code)
	}
	rec = dataOf(t, resp)
	if rec["acked_by"] != "alice" {
		t.Fatalf("first ack must win, got %v", rec["acked_by"])
	}
}

func TestHandleAlertAckEmptyBody(t *testing.T) {
	env := newTestEnv(t, withAckStore(ack.NewMemoryStore()))

	// An empty body is a valid anonymous ack.
	code, resp := env.do(t, http.MethodPost, "/api/v1/alerts/a1/ack", "", nil)
	if code != http.StatusOK {
		t.Fatalf("empty-body ack failed with status %d", code)
	}
	rec := dataOf(t, resp)
	if rec["alert_id"] != "a1" {
		t.Fatalf("unexpected record: %v", rec)
	}
	if by, ok := rec["acked_by"]; ok && by != "" {
		t.Errorf("acked_by should stay absent, got %v", by)
	}
}

func TestHandleAlertAckMalformedBody(t *testing.T) {
	env := newTestEnv(t, withAckStore(ack.NewMemoryStore()))

	code, _ := env.do(t, http.MethodPost, "/api/v1/alerts/a1/ack", `{broken`, nil)
	if code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", code)
	}
}

func TestHandleAlertAckCancelsPendingUpgrade(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pendings.json")
	esc := escalation.NewManager(ack.NewMemoryStore(), nil, path)
	env := newTestEnv(t, withAckStore(ack.NewMemoryStore()), func(c *Config) { c.Escalation = esc })

	// Arm a pending upgrade (persisted), then ack through the API.
	err := esc.Schedule(context.Background(), escalation.Pending{
		RuleID: "r-up", AlertID: "inc-1", To: []string{"phone"},
		Timeout: time.Minute, CreatedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("Schedule: %v", err)
	}
	if code, _ := env.do(t, http.MethodPost, "/api/v1/alerts/inc-1/ack", `{"acked_by":"alice"}`, nil); code != http.StatusOK {
		t.Fatalf("ack failed with status %d", code)
	}
	// The ack cancelled the pending upgrade: the persisted table is empty.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read pendings: %v", err)
	}
	var f struct {
		Pendings []escalation.Pending `json:"pendings"`
	}
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatalf("parse pendings: %v", err)
	}
	if len(f.Pendings) != 0 {
		t.Fatalf("ack must cancel the pending upgrade, got %+v", f.Pendings)
	}
}

func TestHandleAlertAckMethodNotAllowed(t *testing.T) {
	env := newTestEnv(t, withAckStore(ack.NewMemoryStore()))

	for _, tc := range []struct {
		name   string
		method string
		path   string
	}{
		{"put ack", http.MethodPut, "/api/v1/alerts/a1/ack"},
		{"post status", http.MethodPost, "/api/v1/alerts/a1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, _ := env.do(t, tc.method, tc.path, "{}", nil)
			if code != http.StatusMethodNotAllowed {
				t.Fatalf("expected status 405, got %d", code)
			}
		})
	}
}
