package roster

import (
	"strings"
	"testing"
	"time"
)

func validPeriods() []Period {
	base := time.Date(2026, 10, 5, 9, 0, 0, 0, time.UTC)
	return []Period{
		{Start: base, End: base.Add(8 * time.Hour)},
		{Start: base.Add(48 * time.Hour), End: base.Add(48*time.Hour + 8*time.Hour)},
	}
}

func validRoster() Roster {
	return Roster{ID: "ops-oncall", Description: "primary rotation", Periods: validPeriods()}
}

func TestRosterValidate(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		if err := validRoster().Validate(); err != nil {
			t.Fatalf("expected acceptance, got %v", err)
		}
	})

	t.Run("bad ids", func(t *testing.T) {
		for _, id := range []string{"", " lead", "has space", "has/slash", strings.Repeat("x", 65)} {
			r := validRoster()
			r.ID = id
			if err := r.Validate(); err == nil {
				t.Errorf("id %q: expected rejection", id)
			}
		}
		for _, id := range []string{"a", "A9._-", strings.Repeat("x", 64)} {
			r := validRoster()
			r.ID = id
			if err := r.Validate(); err != nil {
				t.Errorf("id %q: expected acceptance, got %v", id, err)
			}
		}
	})

	t.Run("description bound", func(t *testing.T) {
		r := validRoster()
		r.Description = strings.Repeat("d", maxDescriptionChars+1)
		if err := r.Validate(); err == nil || !strings.Contains(err.Error(), "description") {
			t.Fatalf("expected description bound, got %v", err)
		}
	})

	t.Run("periods required", func(t *testing.T) {
		r := validRoster()
		r.Periods = nil
		if err := r.Validate(); err == nil || !strings.Contains(err.Error(), "at least one period") {
			t.Fatalf("expected empty-periods rejection, got %v", err)
		}
	})

	t.Run("periods bound", func(t *testing.T) {
		r := validRoster()
		r.Periods = make([]Period, maxPeriods+1)
		for i := range r.Periods {
			r.Periods[i] = Period{Start: time.Now().Add(time.Duration(i) * time.Hour), End: time.Now().Add(time.Duration(i)*time.Hour + time.Minute)}
		}
		if err := r.Validate(); err == nil || !strings.Contains(err.Error(), "max") {
			t.Fatalf("expected period-count bound, got %v", err)
		}
	})

	t.Run("missing timestamps", func(t *testing.T) {
		r := validRoster()
		r.Periods = []Period{{End: time.Now()}}
		if err := r.Validate(); err == nil || !strings.Contains(err.Error(), "missing a start or end") {
			t.Fatalf("expected missing-timestamp rejection, got %v", err)
		}
	})

	t.Run("zero-length and reversed periods", func(t *testing.T) {
		at := time.Date(2026, 10, 5, 9, 0, 0, 0, time.UTC)
		for _, p := range []Period{
			{Start: at, End: at},
			{Start: at, End: at.Add(-time.Hour)},
		} {
			r := validRoster()
			r.Periods = []Period{p}
			if err := r.Validate(); err == nil || !strings.Contains(err.Error(), "ends at or before") {
				t.Fatalf("expected end-bound rejection for %+v, got %v", p, err)
			}
		}
	})

	t.Run("overlap and order", func(t *testing.T) {
		base := time.Date(2026, 10, 5, 9, 0, 0, 0, time.UTC)
		r := validRoster()
		r.Periods = []Period{
			{Start: base, End: base.Add(8 * time.Hour)},
			{Start: base.Add(7 * time.Hour), End: base.Add(10 * time.Hour)},
		}
		if err := r.Validate(); err == nil || !strings.Contains(err.Error(), "overlaps") {
			t.Fatalf("expected overlap rejection, got %v", err)
		}

		// Normalize sorts, so unsorted input is accepted once normalized.
		r.Periods = []Period{
			{Start: base.Add(24 * time.Hour), End: base.Add(24*time.Hour + time.Hour)},
			{Start: base, End: base.Add(time.Hour)},
		}
		if err := r.Validate(); err == nil || !strings.Contains(err.Error(), "out of start order") {
			t.Fatalf("expected order rejection before Normalize, got %v", err)
		}
		r.Normalize()
		if err := r.Validate(); err != nil {
			t.Fatalf("Normalize must fix order, got %v", err)
		}
	})
}

func TestRosterNormalize(t *testing.T) {
	r := Roster{ID: "  ops  "}
	r.Normalize()
	if r.ID != "ops" {
		t.Errorf("expected trimmed id, got %q", r.ID)
	}
}

func TestRosterCovers(t *testing.T) {
	base := time.Date(2026, 10, 5, 9, 0, 0, 0, time.UTC)
	r := Roster{ID: "ops", Periods: []Period{
		{Start: base, End: base.Add(8 * time.Hour)},          // Mon 09:00-17:00
		{Start: base.Add(48 * time.Hour), End: base.Add(56 * time.Hour)}, // Wed 09:00-17:00
	}}

	for _, tc := range []struct {
		at     time.Time
		covers bool
	}{
		{base.Add(-time.Minute), false},               // before the first period
		{base, true},                                   // start inclusive
		{base.Add(4 * time.Hour), true},               // inside
		{base.Add(8 * time.Hour), false},              // end exclusive
		{base.Add(24 * time.Hour), false},             // between periods
		{base.Add(48 * time.Hour), true},              // second period start
		{base.Add(56 * time.Hour), false},             // second period end
	} {
		if got := r.Covers(tc.at); got != tc.covers {
			t.Errorf("Covers(%v) = %v, want %v", tc.at, got, tc.covers)
		}
	}

	// An empty roster covers nothing (the fail-open default for unknown or
	// not-yet-pushed schedules).
	empty := Roster{ID: "none"}
	if empty.Covers(base) {
		t.Error("an empty roster must cover nothing")
	}
}
