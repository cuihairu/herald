package rules

import (
	"strings"
	"testing"
	"time"
)

func TestParseSilenceWindow(t *testing.T) {
	t.Run("ok", func(t *testing.T) {
		w, err := ParseSilenceWindow("22:30", "06:00", "")
		if err != nil {
			t.Fatalf("ParseSilenceWindow: %v", err)
		}
		if w.StartMin != 22*60+30 || w.EndMin != 6*60 {
			t.Fatalf("unexpected window: %+v", w)
		}
		if w.loc != nil {
			t.Errorf("loc = %v, want nil (the time's own zone) when tz is unset", w.loc)
		}
	})

	t.Run("bad end", func(t *testing.T) {
		if _, err := ParseSilenceWindow("22:30", "not-a-time", ""); err == nil {
			t.Fatal("a malformed end time must be rejected")
		}
	})

	t.Run("bad start", func(t *testing.T) {
		if _, err := ParseSilenceWindow("nope", "06:00", ""); err == nil {
			t.Fatal("a malformed start time must be rejected")
		}
	})

	t.Run("zero length", func(t *testing.T) {
		if _, err := ParseSilenceWindow("08:00", "08:00", ""); err == nil {
			t.Fatal("a zero-length window must be rejected")
		}
	})

	t.Run("valid tz", func(t *testing.T) {
		w, err := ParseSilenceWindow("22:30", "06:00", "Asia/Shanghai")
		if err != nil {
			t.Fatalf("ParseSilenceWindow: %v", err)
		}
		if w.loc == nil {
			t.Fatal("loc = nil, want the configured zone")
		}
	})

	t.Run("unknown tz", func(t *testing.T) {
		_, err := ParseSilenceWindow("22:30", "06:00", "Mars/Olympus")
		if err == nil {
			t.Fatal("an unknown tz must be rejected")
		}
		if !strings.Contains(err.Error(), `silence tz "Mars/Olympus"`) {
			t.Errorf("error = %v, want it to name the tz field", err)
		}
	})
}

// TestSilenceWindowContainsTZ pins the zone conversion: a configured tz
// decides the wall clock the window is read against, whatever zone the
// queried time carries; an unset tz keeps the pre-tz behavior (the
// time's own zone).
func TestSilenceWindowContainsTZ(t *testing.T) {
	shanghai, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("LoadLocation: %v (zoneinfo present in the test env?)", err)
	}
	w, err := ParseSilenceWindow("22:00", "06:00", "Asia/Shanghai")
	if err != nil {
		t.Fatalf("ParseSilenceWindow: %v", err)
	}

	for _, tc := range []struct {
		name string
		t    time.Time
		want bool
	}{
		// 14:30 UTC is 22:30 in Shanghai: inside the window even though
		// 14:30 in any unconverted reading is not.
		{"converted into window", time.Date(2026, 5, 10, 14, 30, 0, 0, time.UTC), true},
		// 23:00 UTC is 07:00 the next day in Shanghai: outside, even
		// though 23:00 itself would fall inside an unconverted reading.
		{"converted out of window", time.Date(2026, 5, 10, 23, 0, 0, 0, time.UTC), false},
		// 13:59 UTC is 21:59 in Shanghai: before the start bound.
		{"before start", time.Date(2026, 5, 10, 13, 59, 0, 0, time.UTC), false},
	} {
		if got := w.Contains(tc.t); got != tc.want {
			t.Errorf("%s: Contains(%v) = %v, want %v (read in %s)", tc.name, tc.t, got, tc.want, shanghai)
		}
	}

	local, err := ParseSilenceWindow("22:00", "06:00", "")
	if err != nil {
		t.Fatalf("ParseSilenceWindow: %v", err)
	}
	if !local.Contains(time.Date(2026, 5, 10, 23, 0, 0, 0, time.UTC)) {
		t.Error("unset tz must keep reading the time's own zone")
	}
}
