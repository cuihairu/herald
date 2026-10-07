// Package digest folds an audience's low-frequency events into one
// summary per 品类×时间窗 (总纲 §10): events collected while a window is
// open are emitted as a single batch when the window flips. The clock
// and both flip schedules are injected, so window flips are fully
// deterministic under test; the flip loop (FlipLoop) and the multi-
// instance leader lease (LeaderLock) live here too so a heraldd process
// only wires them together.
package digest

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cuihairu/herald/core"
	"github.com/cuihairu/herald/core/audience"
)

// Mode is the window cadence a folding preference asks for.
type Mode string

const (
	// Daily folds into the daily window: one flip a day at the
	// configured time.
	Daily Mode = "daily"
	// Weekly folds into the weekly window: one flip a week at the
	// configured weekday and time.
	Weekly Mode = "weekly"
)

// Key identifies one audience×category slot (关系详设 §10 聚合键).
type Key struct {
	AudienceID string
	Category   string
}

// Batch is one flipped window: everything collected for a key since the
// last flip, ordered by CreatedAt.
type Batch struct {
	Key    Key
	Mode   Mode
	Events []*core.Notification
}

// Spec is one cadence's flip schedule in a concrete location.
type Spec struct {
	Hour, Minute int
	Weekday      time.Weekday // weekly only
	Loc          *time.Location
}

// ParseDaily parses a daily flip time "HH:MM" in loc.
func ParseDaily(s string, loc *time.Location) (Spec, error) {
	h, m, err := parseClock(s)
	if err != nil {
		return Spec{}, err
	}
	return Spec{Hour: h, Minute: m, Loc: loc}, nil
}

// ParseWeekly parses a weekly flip time "Weekday HH:MM" (Mon..Sun, or a
// full weekday name) in loc.
func ParseWeekly(s string, loc *time.Location) (Spec, error) {
	day, rest, err := parseWeekday(s)
	if err != nil {
		return Spec{}, err
	}
	h, m, err := parseClock(rest)
	if err != nil {
		return Spec{}, err
	}
	return Spec{Hour: h, Minute: m, Weekday: day, Loc: loc}, nil
}

func parseClock(s string) (int, int, error) {
	h, m, err := parseHM(s)
	if err != nil {
		return 0, 0, err
	}
	if h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, 0, fmt.Errorf("digest: flip time out of range, want HH:MM (00:00-23:59), got %q", s)
	}
	return h, m, nil
}

func parseHM(s string) (int, int, error) {
	parts := strings.SplitN(s, ":", 2)
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("digest: want flip time HH:MM, got %q", s)
	}
	h, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil {
		return 0, 0, fmt.Errorf("digest: want flip time HH:MM, got %q", s)
	}
	m, err := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil {
		return 0, 0, fmt.Errorf("digest: want flip time HH:MM, got %q", s)
	}
	return h, m, nil
}

func parseWeekday(s string) (time.Weekday, string, error) {
	parts := strings.SplitN(strings.TrimSpace(s), " ", 2)
	if len(parts) != 2 {
		return time.Sunday, "", fmt.Errorf("digest: want weekly flip time \"Weekday HH:MM\", got %q", s)
	}
	days := map[string]time.Weekday{
		"sun": time.Sunday, "sunday": time.Sunday,
		"mon": time.Monday, "monday": time.Monday,
		"tue": time.Tuesday, "tuesday": time.Tuesday,
		"wed": time.Wednesday, "wednesday": time.Wednesday,
		"thu": time.Thursday, "thursday": time.Thursday,
		"fri": time.Friday, "friday": time.Friday,
		"sat": time.Saturday, "saturday": time.Saturday,
	}
	day, ok := days[strings.ToLower(strings.TrimSpace(parts[0]))]
	if !ok {
		return time.Sunday, "", fmt.Errorf("digest: unknown weekday %q", parts[0])
	}
	return day, parts[1], nil
}

// Aggregator collects events per audience×category×mode and reports the
// batches whose window has flipped.
type Aggregator struct {
	mu      sync.Mutex
	windows map[windowKey]*window
	clock   func() time.Time
	daily   Spec
	weekly  Spec
}

type windowKey struct {
	k Key
	m Mode
}

type window struct {
	events []*core.Notification
	flipAt time.Time
}

// NewAggregator builds an aggregator over the two flip schedules.
func NewAggregator(daily, weekly Spec) *Aggregator {
	return &Aggregator{
		windows: make(map[windowKey]*window),
		clock:   time.Now,
		daily:   daily,
		weekly:  weekly,
	}
}

// SetClock injects the window clock (deterministic tests).
func (a *Aggregator) SetClock(now func() time.Time) { a.clock = now }

// Add files one event into its audience×category window under the given
// mode. The first event of a window opens it: the window flips at the
// schedule's first occurrence strictly after the opening time, so an
// event arriving at the flip moment itself belongs to the next window.
func (a *Aggregator) Add(n *core.Notification, mode Mode) {
	a.mu.Lock()
	defer a.mu.Unlock()
	wk := windowKey{k: Key{AudienceID: n.AudienceID, Category: n.Type}, m: mode}
	w, ok := a.windows[wk]
	if !ok {
		w = &window{flipAt: a.nextFlip(mode, a.clock())}
		a.windows[wk] = w
	}
	w.events = append(w.events, n)
}

// Due flips every window whose flip time has passed and returns one
// batch per flipped window that actually collected events — an empty
// restarted window only advances its flip time. Flipped windows restart
// immediately: their next flip is the schedule's next occurrence after
// now, so a missed tick self-heals on the next one.
func (a *Aggregator) Due() []Batch {
	a.mu.Lock()
	defer a.mu.Unlock()
	now := a.clock()
	var out []Batch
	for wk, w := range a.windows {
		if now.Before(w.flipAt) {
			continue
		}
		if len(w.events) > 0 {
			events := make([]*core.Notification, len(w.events))
			copy(events, w.events)
			sort.Slice(events, func(i, j int) bool {
				return events[i].CreatedAt.Before(events[j].CreatedAt)
			})
			out = append(out, Batch{Key: wk.k, Mode: wk.m, Events: events})
		}
		w.events = nil
		w.flipAt = a.nextFlip(wk.m, now)
	}
	// Map iteration is random; a stable batch order keeps flush behavior
	// and test expectations deterministic.
	sort.Slice(out, func(i, j int) bool {
		if out[i].Key.AudienceID != out[j].Key.AudienceID {
			return out[i].Key.AudienceID < out[j].Key.AudienceID
		}
		if out[i].Key.Category != out[j].Key.Category {
			return out[i].Key.Category < out[j].Key.Category
		}
		return out[i].Mode < out[j].Mode
	})
	return out
}

// nextFlip returns the schedule's first occurrence strictly after t.
func (a *Aggregator) nextFlip(mode Mode, t time.Time) time.Time {
	spec := a.daily
	if mode == Weekly {
		spec = a.weekly
	}
	loc := spec.Loc
	if loc == nil {
		loc = time.Local
	}
	local := t.In(loc)
	flip := time.Date(local.Year(), local.Month(), local.Day(), spec.Hour, spec.Minute, 0, 0, loc)
	if mode == Weekly {
		days := (int(local.Weekday()) - int(spec.Weekday) + 7) % 7
		flip = flip.AddDate(0, 0, -days)
	}
	if flip.After(t) {
		return flip
	}
	if mode == Weekly {
		return flip.AddDate(0, 0, 7)
	}
	return flip.AddDate(0, 0, 1)
}

// Resolve decides an event's fate (§10 豁免) from its audience's
// preferences: a realtime-preferring channel keeps the whole event
// direct — 直投 wins over folding when channels disagree, the same
// 宁直投勿扣留 conservatism the channel matrix uses. Otherwise any
// daily/weekly preference folds the event under the tighter of the two
// windows; FreqNone channels contribute nothing (their silence is
// enforced at delivery, not by withholding the window). The category
// axis is the notification type until the integrator API (关系详设 §13.2)
// carries a separate category field. No audience reference resolves
// direct: anonymous events have no preference to fold by.
func Resolve(prefs *audience.PreferenceRegistry, audienceID, category string, channels []string) (Mode, bool) {
	if audienceID == "" || len(channels) == 0 {
		return "", false
	}
	fold := false
	for _, ch := range channels {
		switch frequency(prefs, audienceID, category, ch) {
		case audience.FreqRealtime:
			return "", false
		case audience.FreqDaily, audience.FreqWeekly:
			fold = true
		}
	}
	if !fold {
		return "", false
	}
	for _, ch := range channels {
		if frequency(prefs, audienceID, category, ch) == audience.FreqDaily {
			return Daily, true
		}
	}
	return Weekly, true
}

func frequency(prefs *audience.PreferenceRegistry, audienceID, category, channel string) audience.Frequency {
	if prefs != nil {
		return prefs.Effective(audienceID, category, channel)
	}
	return audience.PolicyFor(category).Frequency
}
