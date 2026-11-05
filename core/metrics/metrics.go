// Package metrics samples process-level runtime indicators (CPU, RSS,
// goroutines, online workers) into a bounded ring for the dashboard's
// trend charts. The sampler is deliberately dependency-free: procfs reads
// replace any psutil-style library.
package metrics

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Sample is one process snapshot.
type Sample struct {
	Time       time.Time `json:"time"`
	CPUPercent float64   `json:"cpu_percent"`
	RSSBytes   uint64    `json:"rss_bytes"`
	Goroutines int       `json:"goroutines"`
	Workers    int       `json:"workers"`
}

// Collector supplies the host-level readings; tests inject fakes.
type Collector interface {
	CPUPercent() float64
	RSSBytes() uint64
}

var errBadParams = errors.New("metrics: interval and retention must be positive")

// validate guards the sampler knobs; main wires config through it so a
// bad config fails loudly instead of silently never sampling.
func Validate(interval, retention time.Duration) error {
	if interval <= 0 || retention <= 0 {
		return fmt.Errorf("%w: interval=%v retention=%v", errBadParams, interval, retention)
	}
	return nil
}

// Sampler records samples on a fixed cadence and keeps the most recent
// retention worth of them.
type Sampler struct {
	mu        sync.Mutex
	interval  time.Duration
	retention time.Duration
	samples   []Sample
	collector Collector
	workers   func() int
	now       func() time.Time
	stop      chan struct{}
	done      chan struct{}
	started   bool
	stopped   bool
}

// NewSampler builds a sampler; Start drives it, tests may drive
// sampleOnce directly with an injected clock.
func NewSampler(interval, retention time.Duration, collector Collector, workers func() int, now func() time.Time) *Sampler {
	if now == nil {
		now = time.Now
	}
	return &Sampler{
		interval:  interval,
		retention: retention,
		collector: collector,
		workers:   workers,
		now:       now,
		stop:      make(chan struct{}),
		done:      make(chan struct{}),
	}
}

// Start launches the sampling loop; the first sample is immediate so a
// fresh dashboard has at least one point.
func (s *Sampler) Start() {
	s.mu.Lock()
	if s.started || s.stopped {
		s.mu.Unlock()
		return
	}
	s.started = true
	s.mu.Unlock()

	go func() {
		defer close(s.done)
		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()
		s.SampleOnce()
		for {
			select {
			case <-s.stop:
				return
			case <-ticker.C:
				s.SampleOnce()
			}
		}
	}()
}

// Stop terminates the loop; safe to call concurrently, twice, and even
// before Start (when no loop is running it returns immediately).
func (s *Sampler) Stop() {
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return
	}
	s.stopped = true
	close(s.stop)
	wasStarted := s.started
	s.mu.Unlock()
	if wasStarted {
		<-s.done
	}
}

// sampleOnce records one snapshot and prunes the ring. Tests call it
// directly with a fixed clock; the loop calls it on the ticker.
func (s *Sampler) SampleOnce() {
	sample := Sample{Time: s.now(), Goroutines: runtime.NumGoroutine()}
	if s.collector != nil {
		sample.CPUPercent = s.collector.CPUPercent()
		sample.RSSBytes = s.collector.RSSBytes()
	}
	if s.workers != nil {
		sample.Workers = s.workers()
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.samples = append(s.samples, sample)
	s.pruneLocked()
}

// pruneLocked drops samples older than the retention window. Callers hold
// the lock.
func (s *Sampler) pruneLocked() {
	cutoff := s.now().Add(-s.retention)
	kept := s.samples[:0]
	for _, sample := range s.samples {
		if sample.Time.After(cutoff) {
			kept = append(kept, sample)
		}
	}
	s.samples = kept
}

// prune reaps expired samples; tests use it to verify the retention cut.
func (s *Sampler) prune() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked()
}

// History returns the samples inside [since, now], oldest first. The
// slice is a copy: callers cannot mutate the ring.
func (s *Sampler) History(since time.Time) []Sample {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Sample, 0, len(s.samples))
	for _, sample := range s.samples {
		if !sample.Time.Before(since) {
			out = append(out, sample)
		}
	}
	return out
}

// Latest returns the most recent sample, if any.
func (s *Sampler) Latest() (Sample, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.samples) == 0 {
		return Sample{}, false
	}
	return s.samples[len(s.samples)-1], true
}

// Interval exposes the sampling cadence for the history endpoint.
func (s *Sampler) Interval() time.Duration { return s.interval }

// procPathVars are seams so the read-failure paths stay testable on a
// machine that has a real procfs.
var (
	procSelfStatPath = "/proc/self/stat"
	procStatPath     = "/proc/stat"
	procStatusPath   = "/proc/self/status"
)

func procPaths() func() {
	selfStat, stat, status := procSelfStatPath, procStatPath, procStatusPath
	return func() { setProcPaths(selfStat, stat, status) }
}

func setProcPaths(selfStat, stat, status string) {
	procSelfStatPath = selfStat
	procStatPath = stat
	procStatusPath = status
}

// HostCollector reads CPU and RSS from procfs. CPU percent is computed
// between consecutive calls: the first call establishes the baseline and
// returns 0.
type HostCollector struct {
	mu          sync.Mutex
	prevProc    uint64
	prevTotal   uint64
	hasBaseline bool
}

// NewHostCollector creates an unprimed collector.
func NewHostCollector() *HostCollector { return &HostCollector{} }

// CPUPercent returns the process CPU usage since the previous call.
func (h *HostCollector) CPUPercent() float64 {
	h.mu.Lock()
	defer h.mu.Unlock()

	data, err := os.ReadFile(procSelfStatPath)
	if err != nil {
		return 0
	}
	utime, stime, err := parseProcSelfStat(data)
	if err != nil {
		return 0
	}
	totalData, err := os.ReadFile(procStatPath)
	if err != nil {
		return 0
	}
	total, err := parseProcStatTotal(totalData)
	if err != nil {
		return 0
	}

	proc := utime + stime
	if !h.hasBaseline {
		h.prevProc, h.prevTotal, h.hasBaseline = proc, total, true
		return 0
	}
	dProc := proc - h.prevProc
	dTotal := total - h.prevTotal
	h.prevProc, h.prevTotal = proc, total
	if dTotal == 0 {
		return 0
	}
	return float64(dProc) / float64(dTotal) * 100
}

// RSSBytes returns the resident set size in bytes, 0 when procfs is
// unavailable.
func (h *HostCollector) RSSBytes() uint64 {
	data, err := os.ReadFile(procStatusPath)
	if err != nil {
		return 0
	}
	rss, err := parseVmRSS(data)
	if err != nil {
		return 0
	}
	return rss
}

// parseProcSelfStat extracts utime+stime (fields 14 and 15, 1-based) from
// a /proc/self/stat line. The comm field may contain spaces, so parsing
// starts after the closing parenthesis.
func parseProcSelfStat(data []byte) (uint64, uint64, error) {
	line := string(data)
	idx := strings.LastIndex(line, ")")
	if idx < 0 {
		return 0, 0, fmt.Errorf("metrics: malformed /proc/self/stat")
	}
	fields := strings.Fields(line[idx+1:])
	// After the ")" the remaining slice starts at field 3 (state); utime
	// is field 14, i.e. index 11 in this slice.
	if len(fields) < 13 {
		return 0, 0, fmt.Errorf("metrics: /proc/self/stat truncated")
	}
	utime, err := strconv.ParseUint(fields[11], 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("metrics: bad utime: %w", err)
	}
	stime, err := strconv.ParseUint(fields[12], 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("metrics: bad stime: %w", err)
	}
	return utime, stime, nil
}

// parseProcStatTotal sums the jiffies on the aggregate "cpu" line of
// /proc/stat.
func parseProcStatTotal(data []byte) (uint64, error) {
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || fields[0] != "cpu" {
			continue
		}
		if len(fields) < 2 {
			return 0, fmt.Errorf("metrics: /proc/stat cpu line without fields")
		}
		// guest/guest_nice (fields 9/10) are already folded into
		// user/nice by the kernel; including them inflates the total
		// jiffies and deflates process CPU% on hosts running guests.
		fieldLimit := len(fields)
		if fieldLimit > 9 {
			fieldLimit = 9
		}
		var total uint64
		for _, f := range fields[1:fieldLimit] {
			v, err := strconv.ParseUint(f, 10, 64)
			if err != nil {
				return 0, fmt.Errorf("metrics: bad cpu field %q: %w", f, err)
			}
			total += v
		}
		return total, nil
	}
	return 0, fmt.Errorf("metrics: no cpu line in /proc/stat")
}

// parseVmRSS reads the VmRSS entry from /proc/self/status into bytes.
func parseVmRSS(data []byte) (uint64, error) {
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, "VmRSS:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return 0, fmt.Errorf("metrics: VmRSS line without a value")
		}
		kb, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			return 0, fmt.Errorf("metrics: bad VmRSS value: %w", err)
		}
		return kb * 1024, nil
	}
	return 0, fmt.Errorf("metrics: no VmRSS entry")
}
