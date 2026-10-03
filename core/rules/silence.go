package rules

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// SilenceWindow is the parsed form of a rule's silence window: a daily
// time-of-day range. startMin/endMin are minutes after midnight; endMin <
// startMin is a window crossing midnight. loc is the zone the window is
// read in: nil means the queried time's own zone (production clocks are
// time.Now, i.e. process local — the pre-tz behavior).
type SilenceWindow struct {
	StartMin int
	EndMin   int
	loc      *time.Location
}

// ParseSilenceTime parses one "HH:MM" time-of-day field.
func ParseSilenceTime(s string) (int, error) {
	parts := strings.Split(s, ":")
	if len(parts) != 2 {
		return 0, fmt.Errorf("rules: silence time %q must be HH:MM", s)
	}
	h, err := strconv.Atoi(parts[0])
	if err != nil || h < 0 || h > 23 {
		return 0, fmt.Errorf("rules: silence time %q has an invalid hour", s)
	}
	m, err := strconv.Atoi(parts[1])
	if err != nil || m < 0 || m > 59 {
		return 0, fmt.Errorf("rules: silence time %q has an invalid minute", s)
	}
	return h*60 + m, nil
}

// ParseSilenceWindow parses the start/end pair and the optional IANA tz
// the window is read in (empty = the queried time's own zone, process
// local in production). A zero-length window (start == end) is rejected:
// it either never or always applies depending on the comparison
// convention, and both readings are traps.
func ParseSilenceWindow(start, end, tz string) (*SilenceWindow, error) {
	startMin, err := ParseSilenceTime(start)
	if err != nil {
		return nil, err
	}
	endMin, err := ParseSilenceTime(end)
	if err != nil {
		return nil, err
	}
	if startMin == endMin {
		return nil, fmt.Errorf("rules: silence window %s-%s has zero length", start, end)
	}
	var loc *time.Location
	if tz != "" {
		// LoadLocation doubles as validation: an unknown zone name must
		// fail the rule's save, not silently fall back to UTC.
		loc, err = time.LoadLocation(tz)
		if err != nil {
			return nil, fmt.Errorf("rules: silence tz %q is not a valid time zone", tz)
		}
	}
	return &SilenceWindow{StartMin: startMin, EndMin: endMin, loc: loc}, nil
}

// Contains reports whether the window covers t, read in the window's own
// time zone when one is configured — a 22:00-06:00 Asia/Shanghai window
// answers for Shanghai wall-clock time whatever zone t carries. The end
// bound is exclusive: a 00:00-06:00 window silences 00:00 up to (not
// including) 06:00.
func (w *SilenceWindow) Contains(t time.Time) bool {
	if w.loc != nil {
		t = t.In(w.loc)
	}
	minutes := t.Hour()*60 + t.Minute()
	if w.StartMin < w.EndMin {
		return minutes >= w.StartMin && minutes < w.EndMin
	}
	// Window crosses midnight (e.g. 22:00-06:00).
	return minutes >= w.StartMin || minutes < w.EndMin
}
