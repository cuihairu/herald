package audience

import (
	"errors"
	"strings"
	"testing"
)

// TestPreferenceSetValidation: the preference slot fields are held to
// the same bounds as every other audience table, and an unknown
// frequency never lands in storage.
func TestPreferenceSetValidation(t *testing.T) {
	p := NewPreferenceRegistry()

	cases := []struct {
		name string
		pref Preference
	}{
		{"bad audience id", Preference{AudienceID: "-bad", Category: "alerts", Channel: "email", Frequency: FreqRealtime}},
		{"empty category", Preference{AudienceID: "alice", Category: "", Channel: "email", Frequency: FreqRealtime}},
		{"oversized category", Preference{AudienceID: "alice", Category: strings.Repeat("c", 65), Channel: "email", Frequency: FreqRealtime}},
		{"empty channel", Preference{AudienceID: "alice", Category: "alerts", Channel: "", Frequency: FreqRealtime}},
		{"empty frequency", Preference{AudienceID: "alice", Category: "alerts", Channel: "email", Frequency: ""}},
		{"unknown frequency", Preference{AudienceID: "alice", Category: "alerts", Channel: "email", Frequency: "hourly"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := p.Set(tc.pref); err == nil {
				t.Fatalf("Set(%s) = nil, want a validation error", tc.name)
			}
		})
	}
	if stored := p.List("alice"); len(stored) != 0 {
		t.Errorf("List() after rejected sets = %d entries, want 0", len(stored))
	}
}

// TestPreferenceSilenceFloor: the §7 floor — must-receive categories
// (system, alerts) refuse silence on any channel, but may still be
// folded into a digest; opt-out categories accept FreqNone. Unknown
// categories are user-sovereign.
func TestPreferenceSilenceFloor(t *testing.T) {
	p := NewPreferenceRegistry()

	for _, category := range []string{"system", "alerts"} {
		fold := Preference{AudienceID: "alice", Category: category, Channel: "email", Frequency: FreqWeekly}
		if err := p.Set(fold); err != nil {
			t.Errorf("Set(digest %s) error = %v, want the floor to allow batching", category, err)
		}
		mute := Preference{AudienceID: "alice", Category: category, Channel: "email", Frequency: FreqNone}
		if err := p.Set(mute); !errors.Is(err, ErrPreferenceSilenceFloor) {
			t.Errorf("Set(silence %s) = %v, want ErrPreferenceSilenceFloor", category, err)
		}
	}
	for _, category := range []string{"bills", "domains", "notices", "marketing", "seasonal"} {
		opt := Preference{AudienceID: "alice", Category: category, Channel: "email", Frequency: FreqNone}
		if err := p.Set(opt); err != nil {
			t.Errorf("Set(silence %s) error = %v, want opt-out lawful", category, err)
		}
	}
}

// TestPreferenceDefaults: the §7 default policy table is what an
// untouched audience gets — system/alerts realtime and unmuted,
// marketing weekly, unknown categories realtime with opt-out — and a
// stored preference overrides it per slot.
func TestPreferenceDefaults(t *testing.T) {
	cases := []struct {
		category  string
		frequency Frequency
		silence   bool
	}{
		{"system", FreqRealtime, false},
		{"alerts", FreqRealtime, false},
		{"bills", FreqRealtime, true},
		{"domains", FreqRealtime, true},
		{"notices", FreqRealtime, true},
		{"marketing", FreqWeekly, true},
		{"seasonal", FreqRealtime, true}, // fallback: user-sovereign
	}
	for _, tc := range cases {
		got := PolicyFor(tc.category)
		if got.Frequency != tc.frequency || got.Silence != tc.silence {
			t.Errorf("PolicyFor(%s) = %+v, want {Frequency: %s Silence: %t}", tc.category, got, tc.frequency, tc.silence)
		}
	}

	p := NewPreferenceRegistry()
	if got := p.Effective("alice", "marketing", "email"); got != FreqWeekly {
		t.Errorf("Effective(unset marketing) = %s, want the weekly default", got)
	}
	if err := p.Set(Preference{AudienceID: "alice", Category: "marketing", Channel: "email", Frequency: FreqRealtime}); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	if got := p.Effective("alice", "marketing", "email"); got != FreqRealtime {
		t.Errorf("Effective(set marketing) = %s, want the stored override", got)
	}
	if got := p.Effective("alice", "marketing", "telegram"); got != FreqWeekly {
		t.Errorf("Effective(other channel) = %s, want the default on the unset slot", got)
	}
}

// TestPreferenceReplaceAndClear: the latest word on a slot wins;
// clearing returns the slot to its category default and aiming at air
// is reported, not swallowed.
func TestPreferenceReplaceAndClear(t *testing.T) {
	p := NewPreferenceRegistry()
	slot := Preference{AudienceID: "alice", Category: "bills", Channel: "email"}

	if err := p.Set(Preference{AudienceID: slot.AudienceID, Category: slot.Category, Channel: slot.Channel, Frequency: FreqDaily}); err != nil {
		t.Fatal(err)
	}
	if err := p.Set(Preference{AudienceID: slot.AudienceID, Category: slot.Category, Channel: slot.Channel, Frequency: FreqNone}); err != nil {
		t.Fatal(err)
	}
	if got, ok := p.Get(slot.AudienceID, slot.Category, slot.Channel); !ok || got != FreqNone {
		t.Errorf("Get() = %s/%v, want the latest word FreqNone", got, ok)
	}

	if err := p.Clear(slot.AudienceID, slot.Category, slot.Channel); err != nil {
		t.Fatalf("Clear() error = %v", err)
	}
	if _, ok := p.Get(slot.AudienceID, slot.Category, slot.Channel); ok {
		t.Error("Get() after Clear = present, want the slot back on default")
	}
	if got := p.Effective(slot.AudienceID, slot.Category, slot.Channel); got != FreqRealtime {
		t.Errorf("Effective() after Clear = %s, want the category default", got)
	}
	if err := p.Clear(slot.AudienceID, slot.Category, slot.Channel); !errors.Is(err, ErrPreferenceNotFound) {
		t.Errorf("Clear(already gone) = %v, want ErrPreferenceNotFound", err)
	}
}

// TestPreferenceListIsolationAndOrder: one audience's list carries only
// that audience's slots, sorted by category then channel so the view is
// stable across runs; an unknown audience gets an empty slice.
func TestPreferenceListIsolationAndOrder(t *testing.T) {
	p := NewPreferenceRegistry()
	seed := []Preference{
		{AudienceID: "alice", Category: "alerts", Channel: "telegram", Frequency: FreqRealtime},
		{AudienceID: "alice", Category: "alerts", Channel: "email", Frequency: FreqDaily},
		{AudienceID: "alice", Category: "bills", Channel: "email", Frequency: FreqWeekly},
		{AudienceID: "bob", Category: "marketing", Channel: "email", Frequency: FreqNone},
	}
	for _, pref := range seed {
		if err := p.Set(pref); err != nil {
			t.Fatal(err)
		}
	}

	got := p.List("alice")
	want := []Preference{
		{AudienceID: "alice", Category: "alerts", Channel: "email", Frequency: FreqDaily},
		{AudienceID: "alice", Category: "alerts", Channel: "telegram", Frequency: FreqRealtime},
		{AudienceID: "alice", Category: "bills", Channel: "email", Frequency: FreqWeekly},
	}
	if len(got) != len(want) {
		t.Fatalf("List(alice) = %d entries, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("List(alice)[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
	if ghosts := p.List("ghost"); len(ghosts) != 0 {
		t.Errorf("List(unknown) = %d entries, want 0", len(ghosts))
	}
}
