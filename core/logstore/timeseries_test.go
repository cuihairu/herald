package logstore

import (
	"testing"
	"time"
)

func seedTimeseriesLogs(s *LogStore, now time.Time) {
	mk := func(id, status string, at time.Time) {
		s.Add(&TaskLog{ID: id, Provider: "p1", Status: status, CreatedAt: at})
	}
	// now-2h-1s：窗口外，不计。
	mk("old", "success", now.Add(-2*time.Hour-time.Second))
	// now-115m..now-105m：落在第一桶 [-2h,-90m) 内。
	mk("a1", "success", now.Add(-115*time.Minute))
	mk("a2", "failed", now.Add(-110*time.Minute))
	// 同格再挂一条 pending 与一条 shadow：pending 计总量，shadow 不计。
	mk("a3", "pending", now.Add(-105*time.Minute))
	mk("a4", "shadow", now.Add(-105*time.Minute))
	// now-5m：最后一桶。
	mk("b1", "success", now.Add(-5*time.Minute))
	mk("b2", "failed", now.Add(-5*time.Minute))
	// 未来时间戳：不计。
	mk("future", "success", now.Add(time.Hour))
}

func TestTimeseriesBucketsCounts(t *testing.T) {
	s := New(1000)
	now := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	seedTimeseriesLogs(s, now)

	points := s.timeseriesAt(now, 2*time.Hour, 30*time.Minute)
	if len(points) != 4 {
		t.Fatalf("expected 4 buckets, got %d", len(points))
	}

	first := points[0]
	if first.Total != 3 || first.Success != 1 || first.Failed != 1 {
		t.Errorf("unexpected first bucket %+v", first)
	}
	if !first.Time.Equal(now.Add(-2 * time.Hour)) {
		t.Errorf("expected buckets anchored at the window start, got %v", first.Time)
	}
	for i := 1; i < 3; i++ {
		if points[i].Total != 0 {
			t.Errorf("expected an empty middle bucket, got %+v", points[i])
		}
	}
	last := points[3]
	if last.Total != 2 || last.Success != 1 || last.Failed != 1 {
		t.Errorf("unexpected last bucket %+v", last)
	}
}

func TestTimeseriesMinuteBuckets(t *testing.T) {
	s := New(1000)
	now := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	s.Add(&TaskLog{ID: "m1", Provider: "p1", Status: "success", CreatedAt: now.Add(-30 * time.Second)})

	points := s.timeseriesAt(now, 10*time.Minute, time.Minute)
	if len(points) != 10 {
		t.Fatalf("expected 10 minute buckets, got %d", len(points))
	}
	last := points[len(points)-1]
	if last.Total != 1 || last.Success != 1 {
		t.Errorf("expected the recent entry in the newest bucket, got %+v", last)
	}
}

func TestTimeseriesEdgeGuards(t *testing.T) {
	s := New(1000)
	now := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	// 桶宽大于窗口：收缩为单桶而不是零桶。
	s.Add(&TaskLog{ID: "g1", Provider: "p1", Status: "failed", CreatedAt: now.Add(-time.Minute)})
	points := s.timeseriesAt(now, 10*time.Minute, time.Hour)
	if len(points) != 1 {
		t.Fatalf("expected a single bucket, got %d", len(points))
	}
	if points[0].Total != 1 || points[0].Failed != 1 {
		t.Errorf("unexpected single bucket %+v", points[0])
	}

	// 恰好落在 now 的时间戳：下标越界，收敛进最后一桶。
	s.Add(&TaskLog{ID: "g2", Provider: "p1", Status: "success", CreatedAt: now})
	points = s.timeseriesAt(now, 10*time.Minute, 10*time.Minute)
	last := points[len(points)-1]
	if last.Total != 2 || last.Success != 1 {
		t.Errorf("expected the boundary entry clamped into the last bucket, got %+v", last)
	}
}

func TestTimeseriesOnEmptyStoreIsAllZeroes(t *testing.T) {
	s := New(1000)
	now := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	points := s.timeseriesAt(now, time.Hour, 15*time.Minute)
	if len(points) != 4 {
		t.Fatalf("expected 4 buckets, got %d", len(points))
	}
	for _, p := range points {
		if p.Total != 0 || p.Success != 0 || p.Failed != 0 {
			t.Errorf("expected zeroed bucket, got %+v", p)
		}
	}
}

// 公共入口把真实时钟交给 timeseriesAt：不 mock 也能走到委托语句。
func TestTimeseriesDelegatesToWallClock(t *testing.T) {
	s := New(1000)
	if points := s.Timeseries(time.Minute, time.Minute); len(points) != 1 {
		t.Fatalf("expected a single bucket from the public entry, got %d", len(points))
	}
}
