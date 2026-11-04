package main

import (
	"testing"
	"time"

	"github.com/cuihairu/herald/config"
	"github.com/cuihairu/herald/core/worker"
)

func TestNewMetricsSamplerAppliesDefaultsAndCountsWorkers(t *testing.T) {
	registry := worker.NewRegistry()
	if err := registry.Register(&worker.Info{ID: "w1", Mode: worker.Local}); err != nil {
		t.Fatal(err)
	}

	// 零值配置：回退到内建默认值。
	cfg := &config.Config{}
	sampler, err := newMetricsSampler(cfg, registry)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer sampler.Stop()

	if got := sampler.Interval(); got != 10*time.Second {
		t.Fatalf("expected the default 10s interval, got %v", got)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if samples := sampler.History(time.Now().Add(-time.Hour)); len(samples) >= 1 {
			if samples[0].Workers != 1 {
				t.Fatalf("expected the worker count in the sample, got %d", samples[0].Workers)
			}
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("expected the started sampler to record at least one sample")
}

func TestNewMetricsSamplerHonorsConfiguredKnobs(t *testing.T) {
	cfg := config.Default()
	cfg.Metrics.SampleInterval = 5 * time.Second
	cfg.Metrics.Retention = time.Hour

	sampler, err := newMetricsSampler(cfg, worker.NewRegistry())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer sampler.Stop()

	if got := sampler.Interval(); got != 5*time.Second {
		t.Fatalf("expected the configured 5s interval, got %v", got)
	}
}

func TestNewMetricsSamplerRejectsNegativeConfig(t *testing.T) {
	cfg := config.Default()
	cfg.Metrics.SampleInterval = -1 * time.Second

	if _, err := newMetricsSampler(cfg, worker.NewRegistry()); err == nil {
		t.Fatal("expected a negative sample interval to be rejected")
	}
}
