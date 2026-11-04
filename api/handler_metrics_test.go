package api

import (
	"net/http"
	"testing"
	"time"

	"github.com/cuihairu/herald/core/metrics"
)

type fakeMetricsCollector struct{}

func (fakeMetricsCollector) CPUPercent() float64 { return 42.5 }
func (fakeMetricsCollector) RSSBytes() uint64    { return 2048 }

// seedSampler 落三份样本：约 now-90m、now-60m、now-30m（相对请求时钟留
// 出余量，避免窗口边界被毫秒级漂移翻过）。
func seedSampler() *metrics.Sampler {
	base := time.Now().Add(-90 * time.Minute)
	clock := base
	s := metrics.NewSampler(30*time.Minute, 24*time.Hour, fakeMetricsCollector{}, func() int { return 2 }, func() time.Time { return clock })
	for i := 0; i < 3; i++ {
		s.SampleOnce()
		clock = clock.Add(30 * time.Minute)
	}
	return s
}

func TestMetricsHistoryReturnsWindowedSamples(t *testing.T) {
	env := newTestEnv(t, func(cfg *Config) {
		cfg.Metrics = seedSampler()
	})
	code, resp := env.do(t, http.MethodGet, "/api/v1/metrics/history?window=1h", "", nil)
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
	data := dataOf(t, resp)
	samples, ok := data["samples"].([]any)
	if !ok {
		t.Fatalf("expected samples array, got %T", data["samples"])
	}
	if len(samples) != 1 {
		t.Fatalf("expected exactly the newest samples in the 1h window, got %d", len(samples))
	}
	first := samples[0].(map[string]any)
	if first["cpu_percent"].(float64) != 42.5 {
		t.Errorf("expected cpu 42.5, got %v", first["cpu_percent"])
	}
	if first["workers"].(float64) != 2 {
		t.Errorf("expected workers 2, got %v", first["workers"])
	}
	if first["goroutines"].(float64) <= 0 {
		t.Errorf("expected a positive goroutine count, got %v", first["goroutines"])
	}
	if first["rss_bytes"].(float64) != 2048 {
		t.Errorf("expected rss 2048, got %v", first["rss_bytes"])
	}
}

func TestMetricsHistoryWidensWithWindow(t *testing.T) {
	env := newTestEnv(t, func(cfg *Config) {
		cfg.Metrics = seedSampler()
	})
	code, resp := env.do(t, http.MethodGet, "/api/v1/metrics/history?window=6h", "", nil)
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
	samples := dataOf(t, resp)["samples"].([]any)
	if len(samples) != 3 {
		t.Fatalf("expected all 3 samples in the 6h window, got %d", len(samples))
	}
}

func TestMetricsHistoryRejectsBadWindow(t *testing.T) {
	env := newTestEnv(t)
	code, _ := env.do(t, http.MethodGet, "/api/v1/metrics/history?window=notaduration", "", nil)
	if code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", code)
	}
}

func TestMetricsHistoryWithoutSamplerIsEmpty(t *testing.T) {
	env := newTestEnv(t)
	code, resp := env.do(t, http.MethodGet, "/api/v1/metrics/history?window=6h", "", nil)
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
	samples := dataOf(t, resp)["samples"].([]any)
	if len(samples) != 0 {
		t.Fatalf("expected empty samples without a sampler, got %d", len(samples))
	}
}

func TestMetricsHistoryRejectsNonGET(t *testing.T) {
	env := newTestEnv(t)
	code, _ := env.do(t, http.MethodPost, "/api/v1/metrics/history", "", nil)
	if code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", code)
	}
}
