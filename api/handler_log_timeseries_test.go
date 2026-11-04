package api

import (
	"net/http"
	"testing"
)

func TestLogsTimeseriesDefaultBuckets(t *testing.T) {
	cases := []struct {
		window     string
		wantBucket string
		wantCount  int
	}{
		{"1h", "1m0s", 60},
		{"6h", "15m0s", 24},
		{"24h", "1h0m0s", 24},
	}
	for _, tc := range cases {
		env := newTestEnv(t)
		seedLogs(t, env)
		code, resp := env.do(t, http.MethodGet, "/api/v1/logs/timeseries?window="+tc.window, "", nil)
		if code != http.StatusOK {
			t.Fatalf("window %s: expected 200, got %d", tc.window, code)
		}
		data := dataOf(t, resp)
		if data["bucket"] != tc.wantBucket {
			t.Errorf("window %s: expected bucket %s, got %v", tc.window, tc.wantBucket, data["bucket"])
		}
		points, _ := data["points"].([]any)
		if len(points) != tc.wantCount {
			t.Errorf("window %s: expected %d points, got %d", tc.window, tc.wantCount, len(points))
		}
	}
}

func TestLogsTimeseriesCountsRecentDeliveries(t *testing.T) {
	env := newTestEnv(t)
	seedLogs(t, env)

	code, resp := env.do(t, http.MethodGet, "/api/v1/logs/timeseries?window=1h&bucket=15m", "", nil)
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
	points := dataOf(t, resp)["points"].([]any)
	if len(points) != 4 {
		t.Fatalf("expected 4 buckets, got %d", len(points))
	}
	last := points[len(points)-1].(map[string]any)
	if num(t, last["total"]) != 3 {
		t.Errorf("expected the fresh deliveries in the last bucket, got %v", last)
	}
	if num(t, last["success"]) != 2 || num(t, last["failed"]) != 1 {
		t.Errorf("unexpected outcome split in the last bucket: %v", last)
	}
}

func TestLogsTimeseriesExplicitBucket(t *testing.T) {
	env := newTestEnv(t)
	code, resp := env.do(t, http.MethodGet, "/api/v1/logs/timeseries?window=6h&bucket=1h", "", nil)
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
	data := dataOf(t, resp)
	if data["bucket"] != "1h0m0s" {
		t.Errorf("expected bucket 1h, got %v", data["bucket"])
	}
	if points := data["points"].([]any); len(points) != 6 {
		t.Errorf("expected 6 points, got %d", len(points))
	}
}

func TestLogsTimeseriesRejectsBadParams(t *testing.T) {
	cases := map[string]int{
		"/api/v1/logs/timeseries":                     http.StatusBadRequest, // 缺 window
		"/api/v1/logs/timeseries?window=nope":         http.StatusBadRequest,
		"/api/v1/logs/timeseries?window=0s":           http.StatusBadRequest,
		"/api/v1/logs/timeseries?window=1h&bucket=x":  http.StatusBadRequest,
		"/api/v1/logs/timeseries?window=1h&bucket=0":  http.StatusBadRequest,
		"/api/v1/logs/timeseries?window=1h&bucket=2h": http.StatusBadRequest,
	}
	for path, want := range cases {
		env := newTestEnv(t)
		code, _ := env.do(t, http.MethodGet, path, "", nil)
		if code != want {
			t.Errorf("%s: expected %d, got %d", path, want, code)
		}
	}
}

func TestLogsTimeseriesRejectsNonGET(t *testing.T) {
	env := newTestEnv(t)
	code, _ := env.do(t, http.MethodPost, "/api/v1/logs/timeseries?window=1h", "", nil)
	if code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", code)
	}
}
