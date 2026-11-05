package metrics

import (
	"errors"
	"os"
	"runtime"
	"sync"
	"testing"
	"time"
)

type fakeCollector struct {
	cpu float64
	rss uint64
}

func (f *fakeCollector) CPUPercent() float64 { return f.cpu }
func (f *fakeCollector) RSSBytes() uint64    { return f.rss }

func newTestSampler(collector Collector, workers func() int, now func() time.Time) *Sampler {
	return NewSampler(10*time.Second, time.Hour, collector, workers, now)
}

func TestSampleOnceRecordsAllFields(t *testing.T) {
	base := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	clock := base
	s := newTestSampler(&fakeCollector{cpu: 12.5, rss: 1024}, func() int { return 3 }, func() time.Time { return clock })

	s.SampleOnce()

	samples := s.History(base.Add(-time.Minute))
	if len(samples) != 1 {
		t.Fatalf("expected 1 sample, got %d", len(samples))
	}
	sample := samples[0]
	if !sample.Time.Equal(base) {
		t.Errorf("expected sample time %v, got %v", base, sample.Time)
	}
	if sample.CPUPercent != 12.5 {
		t.Errorf("expected cpu 12.5, got %v", sample.CPUPercent)
	}
	if sample.RSSBytes != 1024 {
		t.Errorf("expected rss 1024, got %v", sample.RSSBytes)
	}
	if sample.Goroutines <= 0 {
		t.Errorf("expected a positive goroutine count, got %d", sample.Goroutines)
	}
	if sample.Workers != 3 {
		t.Errorf("expected 3 workers, got %d", sample.Workers)
	}

	latest, ok := s.Latest()
	if !ok || !latest.Time.Equal(base) {
		t.Errorf("expected latest sample at %v, got ok=%v %v", base, ok, latest)
	}
}

func TestHistoryReturnsOnlySamplesInsideWindow(t *testing.T) {
	base := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	clock := base
	s := newTestSampler(&fakeCollector{}, func() int { return 0 }, func() time.Time { return clock })

	for i := 0; i < 3; i++ {
		s.SampleOnce()
		clock = clock.Add(10 * time.Minute)
		s.SampleOnce()
		s.SampleOnce()
	}

	recent := s.History(base.Add(-time.Minute))
	if len(recent) != 9 {
		t.Fatalf("expected all 9 samples in window, got %d", len(recent))
	}
	old := s.History(base.Add(35 * time.Minute))
	if len(old) != 0 {
		t.Fatalf("expected the future window to exclude everything, got %d", len(old))
	}
}

func TestPruneDropsSamplesOlderThanRetention(t *testing.T) {
	base := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	clock := base
	s := newTestSampler(&fakeCollector{}, func() int { return 0 }, func() time.Time { return clock })

	s.SampleOnce()
	clock = base.Add(2 * time.Hour)
	s.SampleOnce()

	s.prune()

	samples := s.History(base)
	if len(samples) != 1 {
		t.Fatalf("expected the expired sample to be dropped, got %d", len(samples))
	}
	if !samples[0].Time.Equal(clock) {
		t.Errorf("expected the fresh sample to survive, got %v", samples[0].Time)
	}
}

func TestStopHalvesTheLoop(t *testing.T) {
	s := NewSampler(5*time.Millisecond, time.Hour, &fakeCollector{}, func() int { return 0 }, time.Now)
	s.Start()
	s.Stop()
	select {
	case <-s.done:
	default:
		t.Fatal("expected Stop to close the done channel")
	}
	// Stop 幂等，重复调用不能 panic。
	s.Stop()
}

func TestNilWorkersFuncCountsZero(t *testing.T) {
	s := NewSampler(time.Hour, time.Hour, &fakeCollector{}, nil, func() time.Time { return time.Now() })
	s.SampleOnce()
	if s.History(time.Now().Add(-time.Hour))[0].Workers != 0 {
		t.Fatal("expected zero workers with a nil workers func")
	}
}

func TestNewSamplerDefaultsToWallClock(t *testing.T) {
	s := NewSampler(time.Hour, time.Hour, &fakeCollector{}, nil, nil)
	s.SampleOnce()
	if _, ok := s.Latest(); !ok {
		t.Fatal("expected a sample with the default clock")
	}
	if s.Interval() != time.Hour {
		t.Fatalf("expected the interval accessor to report 1h, got %v", s.Interval())
	}
}

func TestLatestOnEmptySamplerIsFalse(t *testing.T) {
	s := NewSampler(time.Hour, time.Hour, &fakeCollector{}, nil, func() time.Time { return time.Now() })
	if _, ok := s.Latest(); ok {
		t.Fatal("expected no sample on an empty ring")
	}
}

func TestStartSamplesOnEveryTick(t *testing.T) {
	s := NewSampler(5*time.Millisecond, time.Hour, &fakeCollector{cpu: 1}, func() int { return 0 }, time.Now)
	s.Start()
	defer s.Stop()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(s.History(time.Now().Add(-time.Hour))) >= 2 {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("expected the ticker to record repeated samples")
}

func TestHostCollectorReadsProcFS(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("host collector reads procfs")
	}
	c := NewHostCollector()
	// 首次调用没有基线，返回 0。
	if cpu := c.CPUPercent(); cpu != 0 {
		t.Fatalf("expected 0 on the first call, got %v", cpu)
	}
	if c.RSSBytes() <= 0 {
		t.Fatal("expected a positive rss reading from procfs")
	}
}

func TestParseProcStatFields(t *testing.T) {
	selfStat := []byte("42 (heraldd) S 1 42 42 0 -1 4194560 0 0 0 0 120 60 0 0 20 0 5 0 12345 12345678 900 18446744073709551615 1 1 0 0 0 0 0 0 0 0 0 0 17 0 0 0 0 0 0\n")
	utime, stime, err := parseProcSelfStat(selfStat)
	if err != nil {
		t.Fatalf("parse proc/self/stat: %v", err)
	}
	if utime != 120 || stime != 60 {
		t.Errorf("expected utime 120 stime 60, got %d/%d", utime, stime)
	}

	total, err := parseProcStatTotal([]byte("cpu  100 0 50 1000 0 0 0 0 0 0\n"))
	if err != nil {
		t.Fatalf("parse proc/stat: %v", err)
	}
	if total != 1150 {
		t.Errorf("expected total 1150, got %d", total)
	}

	rss, err := parseVmRSS([]byte("VmPeak:  100 kB\nVmRSS:     2048 kB\nVmData:  100 kB\n"))
	if err != nil {
		t.Fatalf("parse VmRSS: %v", err)
	}
	if rss != 2048*1024 {
		t.Errorf("expected rss bytes 2097152, got %d", rss)
	}
}

func TestParseProcStatRejectsMalformedInput(t *testing.T) {
	if _, _, err := parseProcSelfStat([]byte("42 (x) S 1 2\n")); err == nil {
		t.Fatal("expected an error for a truncated /proc/self/stat")
	}
	if _, _, err := parseProcSelfStat([]byte("garbage\n")); err == nil {
		t.Fatal("expected an error for a truncated /proc/self/stat")
	}
	if _, _, err := parseProcSelfStat([]byte("42 (x) S 1 2 3 4 5 6 7 8 9 10 x 12 13\n")); err == nil {
		t.Fatal("expected an error for a non-numeric utime")
	}
	if _, _, err := parseProcSelfStat([]byte("42 (x) S 1 2 3 4 5 6 7 8 9 10 11 x 13\n")); err == nil {
		t.Fatal("expected an error for a non-numeric stime")
	}
	if _, err := parseProcStatTotal([]byte("cpuX 1 2 3\n")); err == nil {
		t.Fatal("expected an error for a non-cpu line")
	}
	if _, err := parseProcStatTotal([]byte("cpu\n")); err == nil {
		t.Fatal("expected an error for a cpu line without fields")
	}
	if _, err := parseProcStatTotal([]byte("cpu 1 xx 3\n")); err == nil {
		t.Fatal("expected an error for a non-numeric cpu field")
	}
	if _, err := parseVmRSS([]byte("VmPeak: 100 kB\n")); err == nil {
		t.Fatal("expected an error when VmRSS is missing")
	}
	if _, err := parseVmRSS([]byte("VmRSS:\n")); err == nil {
		t.Fatal("expected an error for a VmRSS line without a value")
	}
	if _, err := parseVmRSS([]byte("VmRSS: notanumber kB\n")); err == nil {
		t.Fatal("expected an error for a non-numeric VmRSS")
	}
}

func TestHostCollectorHandlesUnparsableProcFiles(t *testing.T) {
	dir := t.TempDir()
	selfStat := dir + "/self_stat"
	totalStat := dir + "/stat"
	statusFile := dir + "/status"

	write := func(path, content string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	restore := procPaths()
	defer restore()
	setProcPaths(selfStat, totalStat, statusFile)

	c := NewHostCollector()

	// 基线读数失败：坏的自进程文件。
	write(selfStat, "garbage\n")
	write(totalStat, "cpu  10 0 10 980\n")
	if cpu := c.CPUPercent(); cpu != 0 {
		t.Errorf("expected cpu 0 when /proc/self/stat is unparsable, got %v", cpu)
	}

	// 聚合行解析失败。
	write(selfStat, "42 (x) S 1 2 3 4 5 6 7 8 9 10 11 12 13\n")
	write(totalStat, "cpu\n")
	if cpu := c.CPUPercent(); cpu != 0 {
		t.Errorf("expected cpu 0 when /proc/stat is unparsable, got %v", cpu)
	}

	// 聚合行文件读取失败。
	setProcPaths(selfStat, "/nonexistent/stat", statusFile)
	if cpu := c.CPUPercent(); cpu != 0 {
		t.Errorf("expected cpu 0 when /proc/stat is unreadable, got %v", cpu)
	}
	setProcPaths(selfStat, totalStat, statusFile)

	// 基线已立、但机器侧两次读数之间无 tick：除零守卫。
	write(totalStat, "cpu  10 0 10 980\n")
	if cpu := c.CPUPercent(); cpu != 0 {
		t.Errorf("expected the baseline call to report 0, got %v", cpu)
	}
	if cpu := c.CPUPercent(); cpu != 0 {
		t.Errorf("expected cpu 0 with a zero jiffy delta, got %v", cpu)
	}

	// VmRSS 非数值。
	write(statusFile, "VmRSS: broken kB\n")
	if rss := c.RSSBytes(); rss != 0 {
		t.Errorf("expected rss 0 for a broken VmRSS, got %d", rss)
	}
}

func TestHostCollectorReportsZeroOnMissingProcFS(t *testing.T) {
	restore := procPaths()
	defer restore()
	setProcPaths("/nonexistent/self/stat", "/nonexistent/stat", "/nonexistent/status")

	c := NewHostCollector()
	if cpu := c.CPUPercent(); cpu != 0 {
		t.Errorf("expected cpu 0 on read failure, got %v", cpu)
	}
	if rss := c.RSSBytes(); rss != 0 {
		t.Errorf("expected rss 0 on read failure, got %v", rss)
	}
}

func TestHostCollectorComputesCPUDelta(t *testing.T) {
	dir := t.TempDir()
	selfStat := dir + "/self_stat"
	totalStat := dir + "/stat"
	statusFile := dir + "/status"

	write := func(path, content string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(selfStat, "42 (heraldd) S 1 42 42 0 -1 0 0 0 0 0 100 20 0 0 20 0\n")
	write(totalStat, "cpu  10 0 10 980 0 0 0 0 0 0\n")
	write(statusFile, "VmRSS:  512 kB\n")

	restore := procPaths()
	defer restore()
	setProcPaths(selfStat, totalStat, statusFile)

	c := NewHostCollector()
	if cpu := c.CPUPercent(); cpu != 0 {
		t.Fatalf("expected 0 on the baseline call, got %v", cpu)
	}

	// 进程走 40 jiffies，机器共走 60 jiffies → 66.7%。
	write(selfStat, "42 (heraldd) S 1 42 42 0 -1 0 0 0 0 0 140 20 0 0 20 0\n")
	write(totalStat, "cpu  10 0 10 1040 0 0 0 0 0 0\n")
	write(statusFile, "VmRSS:  1024 kB\n")

	if cpu := c.CPUPercent(); cpu < 66.0 || cpu > 67.0 {
		t.Fatalf("expected ~66.7%% cpu, got %v", cpu)
	}
	if rss := c.RSSBytes(); rss != 1024*1024 {
		t.Fatalf("expected rss 1MiB, got %d", rss)
	}
}

func TestSamplerRejectsNonPositiveInterval(t *testing.T) {
	if err := Validate(0, time.Hour); err == nil {
		t.Fatal("expected an error for a zero interval")
	}
	if err := Validate(10*time.Second, 0); err == nil {
		t.Fatal("expected an error for a zero retention")
	}
	if err := Validate(10*time.Second, time.Hour); err != nil {
		t.Fatalf("expected valid params to pass, got %v", err)
	}
	if !errors.Is(Validate(-1, time.Hour), errBadParams) {
		t.Fatal("expected errBadParams for a negative interval")
	}
}

func TestHistoryReturnsACopy(t *testing.T) {
	now := func() time.Time { return time.Now() }
	s := newTestSampler(&fakeCollector{}, nil, now)
	s.SampleOnce()
	first := s.History(time.Now().Add(-time.Hour))
	first[0].CPUPercent = 999
	second := s.History(time.Now().Add(-time.Hour))
	if second[0].CPUPercent == 999 {
		t.Fatal("expected History to return copies, not the backing array")
	}
	l1, _ := s.Latest()
	l2, _ := s.Latest()
	if !l1.Time.Equal(l2.Time) {
		t.Fatal("expected Latest to be stable")
	}
}

func TestStopBeforeStartDoesNotDeadlock(t *testing.T) {
	s := NewSampler(time.Hour, time.Hour, &fakeCollector{}, nil, time.Now)
	done := make(chan struct{})
	go func() {
		s.Stop()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Stop before Start deadlocked")
	}
}

func TestConcurrentStopIsSafe(t *testing.T) {
	s := NewSampler(5*time.Millisecond, time.Hour, &fakeCollector{}, func() int { return 0 }, time.Now)
	s.Start()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.Stop()
		}()
	}
	wg.Wait()
}

func TestDoubleStartRunsOneLoop(t *testing.T) {
	s := NewSampler(5*time.Millisecond, time.Hour, &fakeCollector{}, func() int { return 0 }, time.Now)
	s.Start()
	s.Start()
	defer s.Stop()
	// The extra Start must not spawn a second goroutine that closes done
	// again; if it did the Stop below would panic on a double close.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if len(s.History(time.Now().Add(-time.Hour))) >= 2 {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("expected the ticker to record repeated samples")
}

func TestParseProcStatTotalExcludesGuestFields(t *testing.T) {
	// user=nice=system=idle=iowait=irq=softirq=steal=10 each; guest=100,
	// guest_nice=200. The kernel folds guest into user and guest_nice
	// into nice, so they must NOT be added again. Total = 8*10 = 80.
	data := []byte("cpu  10 10 10 10 10 10 10 10 100 200\n")
	total, err := parseProcStatTotal(data)
	if err != nil {
		t.Fatalf("parse /proc/stat: %v", err)
	}
	if total != 80 {
		t.Fatalf("expected total 80 (guest fields excluded), got %d", total)
	}
}
