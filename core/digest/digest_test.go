package digest

import (
	"testing"
	"time"

	"github.com/cuihairu/herald/core"
	"github.com/cuihairu/herald/core/audience"
)

func mustLoc(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatalf("LoadLocation(%q): %v", name, err)
	}
	return loc
}

func at(y int, mo time.Month, d, h, mi int, loc *time.Location) time.Time {
	return time.Date(y, mo, d, h, mi, 0, 0, loc)
}

func ev(audienceID, category, title string, createdAt time.Time) *core.Notification {
	return &core.Notification{
		ID:         title,
		Type:       category,
		AudienceID: audienceID,
		Channels:   []string{"email"},
		Content:    &core.DirectContent{Title: title, Body: "body"},
		CreatedAt:  createdAt,
	}
}

func TestParseDaily(t *testing.T) {
	loc := mustLoc(t, "Asia/Shanghai")
	spec, err := ParseDaily("09:30", loc)
	if err != nil {
		t.Fatalf("ParseDaily: %v", err)
	}
	if spec.Hour != 9 || spec.Minute != 30 || spec.Loc != loc {
		t.Errorf("spec = %+v, want 09:30 Asia/Shanghai", spec)
	}
	if _, err := ParseDaily("9:00", loc); err != nil {
		// single-digit hour is fine
		t.Errorf("ParseDaily(9:00) = %v", err)
	}
	if _, err := ParseDaily("2500:00", loc); err == nil {
		t.Error("ParseDaily(2500:00) = nil, want range error")
	}
	if _, err := ParseDaily("09:00", loc); /* valid */ err != nil {
		t.Errorf("ParseDaily(09:00) = %v", err)
	}
	if _, err := ParseDaily("24:00", loc); err == nil {
		t.Error("ParseDaily(24:00) = nil, want range error")
	}
	if _, err := ParseDaily("nope", loc); err == nil {
		t.Error("ParseDaily(nope) = nil, want format error")
	}
	if _, err := ParseDaily("09:xy", loc); err == nil {
		t.Error("ParseDaily(09:xy) = nil, want minute parse error")
	}
	if _, err := ParseDaily("xx:09", loc); err == nil {
		t.Error("ParseDaily(xx:09) = nil, want hour parse error")
	}
}

func TestParseWeekly(t *testing.T) {
	loc := mustLoc(t, "UTC")
	spec, err := ParseWeekly("Mon 09:00", loc)
	if err != nil {
		t.Fatalf("ParseWeekly: %v", err)
	}
	if spec.Weekday != time.Monday || spec.Hour != 9 {
		t.Errorf("spec = %+v, want Monday 09:00", spec)
	}
	if spec2, _ := ParseWeekly("sunday 23:59", loc); spec2.Weekday != time.Sunday {
		t.Errorf("sunday parse = %+v", spec2)
	}
	if _, err := ParseWeekly("09:00", loc); err == nil {
		t.Error("ParseWeekly(09:00) = nil, want weekday error")
	}
	if _, err := ParseWeekly("Funday 09:00", loc); err == nil {
		t.Error("ParseWeekly(Funday 09:00) = nil, want weekday error")
	}
	if _, err := ParseWeekly("Mon 25:00", loc); err == nil {
		t.Error("ParseWeekly(Mon 25:00) = nil, want range error")
	}
}

func TestDailyWindowFlipsAtConfiguredTime(t *testing.T) {
	loc := mustLoc(t, "Asia/Shanghai")
	daily, _ := ParseDaily("09:00", loc)
	weekly, _ := ParseWeekly("Mon 09:00", loc)
	agg := NewAggregator(daily, weekly)

	base := at(2026, time.October, 7, 8, 0, loc) // Wednesday 08:00
	agg.SetClock(func() time.Time { return base })

	// An event before the flip time flips the same day at 09:00.
	agg.Add(ev("alice", "bills", "e1", base), Daily)
	if got := agg.Due(); len(got) != 0 {
		t.Fatalf("Due() before flip = %v, want empty", got)
	}

	agg.SetClock(func() time.Time { return at(2026, time.October, 7, 9, 0, loc) })
	got := agg.Due()
	if len(got) != 1 {
		t.Fatalf("Due() at 09:00 = %d batches, want 1", len(got))
	}
	if got[0].Key != (Key{AudienceID: "alice", Category: "bills"}) || got[0].Mode != Daily {
		t.Errorf("batch key/mode = %+v/%v, want alice/bills daily", got[0].Key, got[0].Mode)
	}
	if len(got[0].Events) != 1 || got[0].Events[0].ID != "e1" {
		t.Errorf("batch events = %+v, want [e1]", got[0].Events)
	}
	// The window restarted: nothing due until the next day's 09:00.
	if got := agg.Due(); len(got) != 0 {
		t.Fatalf("Due() right after flip = %v, want empty", got)
	}
	agg.SetClock(func() time.Time { return at(2026, time.October, 7, 21, 0, loc) })
	if got := agg.Due(); len(got) != 0 {
		t.Fatalf("Due() at 21:00 = %v, want empty (next flip is tomorrow)", got)
	}
	agg.SetClock(func() time.Time { return at(2026, time.October, 8, 9, 0, loc) })
	if got := agg.Due(); len(got) != 0 {
		t.Fatalf("Due() with an empty restarted window = %v, want empty", got)
	}
}

func TestEventAtFlipMomentBelongsToNextWindow(t *testing.T) {
	loc := mustLoc(t, "UTC")
	daily, _ := ParseDaily("09:00", loc)
	weekly, _ := ParseWeekly("Mon 09:00", loc)
	agg := NewAggregator(daily, weekly)
	agg.SetClock(func() time.Time { return at(2026, time.October, 7, 9, 0, loc) })

	agg.Add(ev("alice", "bills", "e1", agg.clock()), Daily)
	if got := agg.Due(); len(got) != 0 {
		t.Fatalf("event at the flip moment flipped instantly: %v", got)
	}
	agg.SetClock(func() time.Time { return at(2026, time.October, 8, 9, 0, loc) })
	if got := agg.Due(); len(got) != 1 {
		t.Fatalf("Due() next day = %d, want the event delivered then", len(got))
	}
}

func TestEventsAfterFlipLandInNextWindow(t *testing.T) {
	loc := mustLoc(t, "UTC")
	daily, _ := ParseDaily("09:00", loc)
	weekly, _ := ParseWeekly("Mon 09:00", loc)
	agg := NewAggregator(daily, weekly)
	now := at(2026, time.October, 7, 10, 0, loc) // after today's flip
	agg.SetClock(func() time.Time { return now })

	agg.Add(ev("alice", "bills", "late", now), Daily)
	agg.SetClock(func() time.Time { return at(2026, time.October, 8, 8, 0, loc) })
	if got := agg.Due(); len(got) != 0 {
		t.Fatalf("Due() before tomorrow's flip = %v", got)
	}
	agg.SetClock(func() time.Time { return at(2026, time.October, 8, 9, 1, loc) })
	got := agg.Due()
	if len(got) != 1 || len(got[0].Events) != 1 {
		t.Fatalf("Due() after tomorrow's flip = %+v, want the late event", got)
	}
}

func TestWeeklyWindowFlipsOnConfiguredWeekday(t *testing.T) {
	loc := mustLoc(t, "UTC")
	daily, _ := ParseDaily("09:00", loc)
	weekly, _ := ParseWeekly("Mon 09:00", loc)
	agg := NewAggregator(daily, weekly)

	// Wednesday Oct 7 2026: an event on Wednesday flips next Monday Oct 12.
	agg.SetClock(func() time.Time { return at(2026, time.October, 7, 12, 0, loc) })
	agg.Add(ev("alice", "notices", "w1", agg.clock()), Weekly)
	agg.SetClock(func() time.Time { return at(2026, time.October, 11, 23, 0, loc) }) // Sunday
	if got := agg.Due(); len(got) != 0 {
		t.Fatalf("Due() on Sunday = %v, want empty", got)
	}
	agg.SetClock(func() time.Time { return at(2026, time.October, 12, 9, 0, loc) }) // Monday 09:00
	got := agg.Due()
	if len(got) != 1 || got[0].Mode != Weekly {
		t.Fatalf("Due() on Monday 09:00 = %+v, want the weekly batch", got)
	}
}

func TestModesAreSeparateWindows(t *testing.T) {
	loc := mustLoc(t, "UTC")
	daily, _ := ParseDaily("09:00", loc)
	weekly, _ := ParseWeekly("Mon 09:00", loc)
	agg := NewAggregator(daily, weekly)
	now := at(2026, time.October, 7, 12, 0, loc)
	agg.SetClock(func() time.Time { return now })

	agg.Add(ev("alice", "bills", "d1", now), Daily)
	agg.Add(ev("alice", "bills", "w1", now), Weekly)
	agg.SetClock(func() time.Time { return at(2026, time.October, 8, 9, 30, loc) })
	got := agg.Due()
	if len(got) != 1 || got[0].Mode != Daily {
		t.Fatalf("Due() at Thursday 09:30 = %+v, want only the daily window", got)
	}
	agg.SetClock(func() time.Time { return at(2026, time.October, 12, 9, 30, loc) })
	got = agg.Due()
	if len(got) != 1 || got[0].Mode != Weekly {
		t.Fatalf("Due() next Monday = %+v, want the weekly window", got)
	}
}

func TestDueSortsEventsAndBatches(t *testing.T) {
	loc := mustLoc(t, "UTC")
	daily, _ := ParseDaily("09:00", loc)
	weekly, _ := ParseWeekly("Mon 09:00", loc)
	agg := NewAggregator(daily, weekly)
	now := at(2026, time.October, 7, 8, 0, loc)
	agg.SetClock(func() time.Time { return now })

	agg.Add(ev("bob", "alerts", "b2", now), Daily)
	agg.Add(ev("alice", "alerts", "x1", now), Daily)
	agg.Add(ev("alice", "bills", "a1", now.Add(-2*time.Hour)), Daily)
	agg.Add(ev("alice", "bills", "a2", now.Add(-1*time.Hour)), Daily)

	agg.SetClock(func() time.Time { return at(2026, time.October, 7, 9, 0, loc) })
	got := agg.Due()
	if len(got) != 3 {
		t.Fatalf("Due() = %d batches, want 3", len(got))
	}
	if got[0].Key.AudienceID != "alice" || got[2].Key.AudienceID != "bob" {
		t.Errorf("batch order = %s…%s, want alice first, bob last (stable key order)", got[0].Key.AudienceID, got[2].Key.AudienceID)
	}
	if got[0].Key.Category != "alerts" || got[1].Key.Category != "bills" {
		t.Errorf("alice's batch order = %s,%s, want alerts before bills (category tiebreak)", got[0].Key.Category, got[1].Key.Category)
	}
	if got[1].Events[0].ID != "a1" || got[1].Events[1].ID != "a2" {
		t.Errorf("events not in CreatedAt order: %s then %s", got[1].Events[0].ID, got[1].Events[1].ID)
	}
}

// TestSpecWithoutLocationFallsBackToLocal: a hand-built Spec without a
// location resolves flips in time.Local instead of panicking.
func TestSpecWithoutLocationFallsBackToLocal(t *testing.T) {
	agg := NewAggregator(Spec{Hour: 9, Minute: 0}, Spec{Hour: 9, Minute: 0})
	now := time.Date(2026, time.October, 7, 8, 0, 0, 0, time.Local)
	agg.SetClock(func() time.Time { return now })

	agg.Add(ev("alice", "bills", "e1", now), Daily)
	agg.SetClock(func() time.Time { return time.Date(2026, time.October, 7, 9, 0, 0, 0, time.Local) })
	if got := agg.Due(); len(got) != 1 || len(got[0].Events) != 1 {
		t.Fatalf("Due() at local 09:00 = %+v, want the event flipped in time.Local", got)
	}
}

// TestDueTiebreaksSameKeyByMode: a daily and a weekly window for the
// same audience and category can flip together; the daily batch sorts
// first.
func TestDueTiebreaksSameKeyByMode(t *testing.T) {
	loc := mustLoc(t, "UTC")
	daily, _ := ParseDaily("09:00", loc)
	weekly, _ := ParseWeekly("Mon 09:00", loc)
	agg := NewAggregator(daily, weekly)

	// The weekly event opens Wednesday Oct 7 and flips Monday Oct 12;
	// the daily event opens Sunday Oct 11 and flips Monday 09:00 too.
	agg.SetClock(func() time.Time { return at(2026, time.October, 7, 12, 0, loc) })
	agg.Add(ev("alice", "bills", "w1", agg.clock()), Weekly)
	sunday := at(2026, time.October, 11, 8, 0, loc)
	agg.SetClock(func() time.Time { return sunday })
	agg.Add(ev("alice", "bills", "d1", sunday), Daily)

	agg.SetClock(func() time.Time { return at(2026, time.October, 12, 9, 30, loc) })
	got := agg.Due()
	if len(got) != 2 {
		t.Fatalf("Due() = %d batches, want both windows", len(got))
	}
	if got[0].Mode != Daily || got[1].Mode != Weekly {
		t.Errorf("batch modes = %s,%s, want daily before weekly", got[0].Mode, got[1].Mode)
	}
}

func TestResolve(t *testing.T) {

	prefs := audience.NewPreferenceRegistry()
	if err := prefs.Set(audience.Preference{AudienceID: "alice", Category: "bills", Channel: "email", Frequency: audience.FreqDaily}); err != nil {
		t.Fatal(err)
	}
	if err := prefs.Set(audience.Preference{AudienceID: "bob", Category: "bills", Channel: "email", Frequency: audience.FreqWeekly}); err != nil {
		t.Fatal(err)
	}
	if err := prefs.Set(audience.Preference{AudienceID: "carol", Category: "bills", Channel: "email", Frequency: audience.FreqNone}); err != nil {
		t.Fatal(err)
	}
	if err := prefs.Set(audience.Preference{AudienceID: "dave", Category: "bills", Channel: "email", Frequency: audience.FreqDaily}); err != nil {
		t.Fatal(err)
	}
	if err := prefs.Set(audience.Preference{AudienceID: "dave", Category: "bills", Channel: "telegram", Frequency: audience.FreqWeekly}); err != nil {
		t.Fatal(err)
	}
	if err := prefs.Set(audience.Preference{AudienceID: "frank", Category: "bills", Channel: "email", Frequency: audience.FreqDaily}); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name     string
		prefs    *audience.PreferenceRegistry
		audience string
		category string
		channels []string
		wantMode Mode
		wantFold bool
	}{
		{"no audience", prefs, "", "bills", []string{"email"}, "", false},
		{"no channels", prefs, "alice", "bills", nil, "", false},
		{"daily preference folds", prefs, "alice", "bills", []string{"email"}, Daily, true},
		{"weekly preference folds", prefs, "bob", "bills", []string{"email"}, Weekly, true},
		{"none alone stays direct (silence at delivery)", prefs, "carol", "bills", []string{"email"}, "", false},
		{"realtime default stays direct", prefs, "erin", "alerts", []string{"telegram"}, "", false},
		{"unknown category defaults realtime", prefs, "erin", "custom.thing", []string{"email"}, "", false},
		{"realtime wins over daily", prefs, "frank", "bills", []string{"email", "telegram"}, "", false},
		{"daily wins over weekly (tighter window)", prefs, "dave", "bills", []string{"email", "telegram"}, Daily, true},
	}
	for _, tc := range cases {
		mode, ok := Resolve(tc.prefs, tc.audience, tc.category, tc.channels)
		if ok != tc.wantFold || mode != tc.wantMode {
			t.Errorf("%s: Resolve = (%q,%v), want (%q,%v)", tc.name, mode, ok, tc.wantMode, tc.wantFold)
		}
	}

	// Nil preferences fall through to the default policy table:
	// marketing defaults weekly, everything else realtime.
	if mode, ok := Resolve(nil, "alice", "marketing", []string{"email"}); !ok || mode != Weekly {
		t.Errorf("nil prefs marketing = (%q,%v), want weekly fold", mode, ok)
	}
	if _, ok := Resolve(nil, "alice", "bills", []string{"email"}); ok {
		t.Error("nil prefs bills folded, want realtime default keeping it direct")
	}
}
