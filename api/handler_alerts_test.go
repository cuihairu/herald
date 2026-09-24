package api

import (
	"net/http"
	"testing"

	"github.com/cuihairu/herald/core/ack"
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
