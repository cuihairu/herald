package audience

import (
	"errors"
	"testing"
)

func TestIntensityString(t *testing.T) {
	cases := map[Intensity]string{
		IntensityRSS:   "L0",
		IntensityMail:  "L1",
		IntensityInbox: "L2",
		IntensityIM:    "L3",
		IntensitySMS:   "L4",
		IntensityPhone: "L5",
		Intensity(42):  "L?", // outside the ladder renders the placeholder
	}
	for i, want := range cases {
		if got := i.String(); got != want {
			t.Errorf("Intensity(%d).String() = %q, want %q", i, got, want)
		}
	}
}

func TestParseIntensity(t *testing.T) {
	valid := map[string]Intensity{
		"L0": IntensityRSS, "RSS": IntensityRSS,
		"L1": IntensityMail, "Mail": IntensityMail, "Email": IntensityMail,
		"L2": IntensityInbox, "Inbox": IntensityInbox, "Webhook": IntensityInbox,
		"L3": IntensityIM, "IM": IntensityIM,
		"L4": IntensitySMS, "SMS": IntensitySMS,
		"L5": IntensityPhone, "Phone": IntensityPhone, "Call": IntensityPhone,
	}
	for in, want := range valid {
		got, err := ParseIntensity(in)
		if err != nil || got != want {
			t.Errorf("ParseIntensity(%q) = %v/%v, want %v/nil", in, got, err, want)
		}
	}
	if _, err := ParseIntensity("L9"); !errors.Is(err, ErrInvalidIntensity) {
		t.Errorf("ParseIntensity(L9) error = %v, want ErrInvalidIntensity", err)
	}
}

func TestChannelIntensity(t *testing.T) {
	cases := map[string]Intensity{
		"rss": IntensityRSS, "feed": IntensityRSS,
		"email": IntensityMail, "smtp": IntensityMail, "mail": IntensityMail,
		"webhook": IntensityInbox, "inbox": IntensityInbox,
		"sms": IntensitySMS, "sms-duty": IntensitySMS, "smsduty": IntensitySMS,
		"phone": IntensityPhone, "call": IntensityPhone, "phone-call": IntensityPhone,
		"telegram": IntensityIM, // unknown names land on the L3 default
		"":         IntensityIM,
	}
	for name, want := range cases {
		if got := ChannelIntensity(name); got != want {
			t.Errorf("ChannelIntensity(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestParseUrgency(t *testing.T) {
	valid := map[string]Urgency{
		"routine": UrgencyRoutine, "例行": UrgencyRoutine,
		"normal": UrgencyNormal, "一般": UrgencyNormal,
		"urgent": UrgencyUrgent, "紧急": UrgencyUrgent,
		"critical": UrgencyCritical, "关键": UrgencyCritical,
	}
	for in, want := range valid {
		got, err := ParseUrgency(in)
		if err != nil || got != want {
			t.Errorf("ParseUrgency(%q) = %v/%v, want %v/nil", in, got, err, want)
		}
	}
	if _, err := ParseUrgency("hourly"); !errors.Is(err, ErrInvalidUrgency) {
		t.Errorf("ParseUrgency(hourly) error = %v, want ErrInvalidUrgency", err)
	}
}

// TestUrgencyRanges pins the §6 ladder windows: each urgency opens its
// documented intensity ceiling and refuses the level above it.
func TestUrgencyRanges(t *testing.T) {
	cases := []struct {
		u     Urgency
		max   Intensity
		allow Intensity
		deny  Intensity
	}{
		{UrgencyRoutine, IntensityMail, IntensityMail, IntensityInbox},
		{UrgencyNormal, IntensityInbox, IntensityInbox, IntensityIM},
		{UrgencyUrgent, IntensitySMS, IntensitySMS, IntensityPhone},
		{UrgencyCritical, IntensityPhone, IntensityPhone, Intensity(9)},
		{Urgency(9), IntensityMail, IntensityMail, IntensityInbox}, // default falls back to L1
	}
	for _, tc := range cases {
		if got := tc.u.MaxIntensity(); got != tc.max {
			t.Errorf("Urgency(%d).MaxIntensity() = %v, want %v", tc.u, got, tc.max)
		}
		if !tc.u.Allows(tc.allow) {
			t.Errorf("Urgency(%d).Allows(%v) = false, want true", tc.u, tc.allow)
		}
		if tc.u.Allows(tc.deny) {
			t.Errorf("Urgency(%d).Allows(%v) = true, want false", tc.u, tc.deny)
		}
	}
}

func TestDefaultUrgency(t *testing.T) {
	cases := map[string]Urgency{
		"alerts": UrgencyUrgent, "alert": UrgencyUrgent, "告警": UrgencyUrgent,
		"domains": UrgencyUrgent, "domain": UrgencyUrgent, "域名": UrgencyUrgent,
		"system": UrgencyCritical, "系统": UrgencyCritical,
		"marketing": UrgencyRoutine, "营销": UrgencyRoutine,
		"bills": UrgencyNormal, "": UrgencyNormal, // unknown → normal
	}
	for category, want := range cases {
		if got := DefaultUrgency(category); got != want {
			t.Errorf("DefaultUrgency(%q) = %v, want %v", category, got, want)
		}
	}
}

func TestParseMode(t *testing.T) {
	valid := map[string]Mode{
		"fixed": ModeFixed, "固定": ModeFixed,
		"escalation": ModeEscalation, "升级链": ModeEscalation,
		"parallel": ModeParallel, "并行": ModeParallel,
	}
	for in, want := range valid {
		got, err := ParseMode(in)
		if err != nil || got != want {
			t.Errorf("ParseMode(%q) = %v/%v, want %v/nil", in, got, err, want)
		}
	}
	if _, err := ParseMode("cascade"); !errors.Is(err, ErrInvalidMode) {
		t.Errorf("ParseMode(cascade) error = %v, want ErrInvalidMode", err)
	}
}

// TestDefaultMode: must-deliver overrides everything to parallel; else
// urgent/critical escalate and the rest stay fixed (§6.3).
func TestModeString(t *testing.T) {
	cases := map[Mode]string{
		ModeFixed:      "fixed",
		ModeEscalation: "escalation",
		ModeParallel:   "parallel",
		Mode(42):       "mode?",
	}
	for m, want := range cases {
		if got := m.String(); got != want {
			t.Errorf("Mode(%d).String() = %q, want %q", m, got, want)
		}
	}
}

func TestDefaultMode(t *testing.T) {
	for _, u := range []Urgency{UrgencyRoutine, UrgencyNormal, UrgencyUrgent, UrgencyCritical} {
		if got := DefaultMode(u, true); got != ModeParallel {
			t.Errorf("DefaultMode(%v, mustDeliver) = %v, want parallel", u, got)
		}
	}
	if got := DefaultMode(UrgencyUrgent, false); got != ModeEscalation {
		t.Errorf("DefaultMode(urgent) = %v, want escalation", got)
	}
	if got := DefaultMode(UrgencyCritical, false); got != ModeEscalation {
		t.Errorf("DefaultMode(critical) = %v, want escalation", got)
	}
	if got := DefaultMode(UrgencyRoutine, false); got != ModeFixed {
		t.Errorf("DefaultMode(routine) = %v, want fixed", got)
	}
	if got := DefaultMode(UrgencyNormal, false); got != ModeFixed {
		t.Errorf("DefaultMode(normal) = %v, want fixed", got)
	}
}

// TestNewDeliveryPolicyRefusesBadTables: an unparseable value in any of
// the three override tables refuses the build — no partial policy, no
// silent clamp.
func TestNewDeliveryPolicyRefusesBadTables(t *testing.T) {
	t.Run("bad urgency", func(t *testing.T) {
		if _, err := NewDeliveryPolicy(map[string]string{"alerts": "hourly"}, nil, nil, false); !errors.Is(err, ErrInvalidUrgency) {
			t.Errorf("error = %v, want ErrInvalidUrgency", err)
		}
	})
	t.Run("bad intensity", func(t *testing.T) {
		if _, err := NewDeliveryPolicy(nil, map[string]string{"sms": "L9"}, nil, false); !errors.Is(err, ErrInvalidIntensity) {
			t.Errorf("error = %v, want ErrInvalidIntensity", err)
		}
	})
	t.Run("bad mode", func(t *testing.T) {
		if _, err := NewDeliveryPolicy(nil, nil, map[string]string{"alerts": "cascade"}, false); !errors.Is(err, ErrInvalidMode) {
			t.Errorf("error = %v, want ErrInvalidMode", err)
		}
	})
	t.Run("empty tables build", func(t *testing.T) {
		if _, err := NewDeliveryPolicy(nil, nil, nil, true); err != nil {
			t.Errorf("NewDeliveryPolicy(nil tables) = %v, want nil", err)
		}
	})
}

// TestDeliveryPolicyOverrides: override tables win (case-insensitively),
// everything else falls back to the taxonomy defaults.
func TestDeliveryPolicyOverrides(t *testing.T) {
	d, err := NewDeliveryPolicy(
		map[string]string{"ALERTS": "critical"},
		map[string]string{"Telegram": "L0"},
		map[string]string{"alerts": "fixed"},
		false,
	)
	if err != nil {
		t.Fatal(err)
	}
	if got := d.UrgencyFor("alerts"); got != UrgencyCritical {
		t.Errorf("UrgencyFor(alerts) = %v, want the critical override", got)
	}
	if got := d.IntensityOf("telegram"); got != IntensityRSS {
		t.Errorf("IntensityOf(telegram) = %v, want the L0 override", got)
	}
	if got := d.ModeFor("alerts", false); got != ModeFixed {
		t.Errorf("ModeFor(alerts) = %v, want the fixed override (mustDeliver irrelevant)", got)
	}
	// No override: taxonomy defaults.
	if got := d.UrgencyFor("marketing"); got != UrgencyRoutine {
		t.Errorf("UrgencyFor(marketing) = %v, want routine", got)
	}
	if got := d.IntensityOf("sms"); got != IntensitySMS {
		t.Errorf("IntensityOf(sms) = %v, want L4", got)
	}
	// ModeFor falls back through DefaultMode(UrgencyFor(...), mustDeliver).
	// The mode override wins even over mustDeliver; mustDeliver reaches
	// the default chain only on categories without one.
	if got := d.ModeFor("alerts", true); got != ModeFixed {
		t.Errorf("ModeFor(alerts, mustDeliver) = %v, want the fixed override", got)
	}
	if got := d.ModeFor("system", true); got != ModeParallel {
		t.Errorf("ModeFor(system, mustDeliver) = %v, want parallel via the default chain", got)
	}
	if got := d.ModeFor("bills", false); got != ModeFixed {
		t.Errorf("ModeFor(bills) = %v, want fixed (normal urgency)", got)
	}
	if got := d.ModeFor("system", false); got != ModeEscalation {
		t.Errorf("ModeFor(system) = %v, want escalation (critical urgency)", got)
	}
}

// TestMatchChannelsThreeWayIntersection: §6 filter × phone gate ×
// urgency window, in that order — each refusal names its own reason and
// only a channel that clears all three stays.
func TestMatchChannelsThreeWayIntersection(t *testing.T) {
	relations := NewRegistry()
	surfaces := NewSurfaceRegistry()
	seedSurfaces(surfaces,
		ContactSurface{AudienceID: "alice", Channel: "email", Target: "a@x", Status: SurfaceActive},
	)
	if err := relations.Subscribe(Relation{
		AudienceID: "alice", Category: "alerts", Channel: "email",
		Type: RelationSubscription, Source: SourcePreferenceCenter,
	}); err != nil {
		t.Fatal(err)
	}
	f := NewFilter(relations, surfaces)

	// allowPhone=false: the operator has not opted in to L5.
	off, err := NewDeliveryPolicy(nil, nil, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	got := off.MatchChannels(f, "alice", "alerts", []string{"email", "telegram"})
	want := []Match{
		{Channel: "email", Kept: true},
		{Channel: "telegram", Kept: false, Reason: "filtered"}, // active surface only on email
	}
	if len(got) != len(want) {
		t.Fatalf("MatchChannels returned %d matches, want %d: %+v", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("match[%d] = %+v, want %+v", i, got[i], w)
		}
	}

	// system is critical: its window reaches L5, but the phone gate still
	// refuses until allow_phone lifts it.
	sys := off.MatchChannels(nil, "alice", "system", []string{"phone", "email"})
	if sys[0] != (Match{Channel: "phone", Kept: false, Reason: "phone_disabled"}) {
		t.Errorf("phone under critical = %+v, want phone_disabled", sys[0])
	}
	if !sys[1].Kept {
		t.Errorf("email under critical = %+v, want kept", sys[1])
	}
	// With the opt-in, critical delivers L5.
	on, err := NewDeliveryPolicy(nil, nil, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if got := on.MatchChannels(nil, "alice", "system", []string{"phone"}); !got[0].Kept {
		t.Errorf("phone with allow_phone = %+v, want kept", got[0])
	}
	// marketing is routine (L0–L1): L2 clears the phone gate (it is not
	// L5) but exceeds the urgency window.
	if got := on.MatchChannels(nil, "alice", "marketing", []string{"webhook"}); got[0] != (Match{Channel: "webhook", Kept: false, Reason: "intensity_exceeded"}) {
		t.Errorf("webhook under routine = %+v, want intensity_exceeded", got[0])
	}
	// KeptChannels is the projection of the kept matches: L4 clears the
	// urgent window, the L5 channel still waits for the phone opt-in.
	kept := off.KeptChannels(nil, "alice", "alerts", []string{"email", "rss", "sms", "phone"})
	if len(kept) != 3 || kept[0] != "email" || kept[1] != "rss" || kept[2] != "sms" {
		t.Errorf("KeptChannels = %v, want [email rss sms]", kept)
	}
}

func TestUrgencyString(t *testing.T) {
	cases := map[Urgency]string{
		UrgencyRoutine:  "routine",
		UrgencyNormal:   "normal",
		UrgencyUrgent:   "urgent",
		UrgencyCritical: "critical",
		Urgency(99):     "urgency?",
	}
	for u, want := range cases {
		if got := u.String(); got != want {
			t.Fatalf("Urgency(%d).String() = %q, want %q", u, got, want)
		}
	}
}
