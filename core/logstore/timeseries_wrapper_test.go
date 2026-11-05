package logstore

import (
	"testing"
	"time"
)

// TestTimeseriesPublicWrapperClock defaults the clock to time.Now: the
// public Timeseries call must return the same shape as the injected-clock
// variant for a window that ends "now".
func TestTimeseriesPublicWrapperClock(t *testing.T) {
	s := New(1000)
	now := time.Now()
	seedTimeseriesLogs(s, now)

	points := s.Timeseries(2*time.Hour, 30*time.Minute)
	if len(points) != 4 {
		t.Fatalf("expected 4 buckets, got %d", len(points))
	}
	var total int
	for _, p := range points {
		total += p.Total
	}
	if total == 0 {
		t.Error("expected seeded attempts to appear in the public wrapper's series")
	}
}
