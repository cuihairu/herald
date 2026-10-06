package audience

import (
	"errors"
	"fmt"
	"sort"
	"sync"
)

// Frequency is the delivery cadence a preference asks for (§7): right
// now, folded into a daily or weekly digest, or not at all.
type Frequency string

const (
	// FreqRealtime: deliver as events happen.
	FreqRealtime Frequency = "realtime"
	// FreqDaily: fold into the daily digest window.
	FreqDaily Frequency = "daily"
	// FreqWeekly: fold into the weekly digest window.
	FreqWeekly Frequency = "weekly"
	// FreqNone: silence the category on this channel. Lawful only where
	// the category's policy allows opting out.
	FreqNone Frequency = "none"
)

// DefaultPolicy is a category's zero-operation behaviour (§7 默认策略
// 表): the frequency a fresh audience gets with no preference set, and
// whether silence (FreqNone) is a lawful choice at all. Silence=false is
// the 必收/不可静默 floor — system and alerts cannot be muted; they may
// still be folded into a digest (content is not lost, only batched; the
// §6.4 critical fallback covers the urgency floor separately).
type DefaultPolicy struct {
	Frequency Frequency
	Silence   bool
}

// defaultPolicies is the §7 default policy table by category. Unknown
// categories fall back to realtime with opt-out allowed — for a category
// nobody configured the table for, the user is sovereign.
var defaultPolicies = map[string]DefaultPolicy{
	"system":    {Frequency: FreqRealtime, Silence: false},
	"alerts":    {Frequency: FreqRealtime, Silence: false},
	"bills":     {Frequency: FreqRealtime, Silence: true},
	"domains":   {Frequency: FreqRealtime, Silence: true},
	"notices":   {Frequency: FreqRealtime, Silence: true},
	"marketing": {Frequency: FreqWeekly, Silence: true},
}

// PolicyFor returns the default policy for a category — table hit or
// the permissive fallback.
func PolicyFor(category string) DefaultPolicy {
	if policy, ok := defaultPolicies[category]; ok {
		return policy
	}
	return DefaultPolicy{Frequency: FreqRealtime, Silence: true}
}

// Preference is one 品类×渠道×频率 choice a subscriber makes in the
// preference center — the Preference term of §1, living on subscription
// relations. The channel names one of the audience's contact surfaces;
// which surfaces exist is the SurfaceRegistry's business, and whether a
// relation permits the pair is the delivery filter's (batch 4) business.
// This registry owns the choice and its floor validation only.
type Preference struct {
	AudienceID string
	Category   string
	Channel    string
	Frequency  Frequency
}

// Preference errors. ErrPreferenceSilenceFloor is the §7 bottom line:
// the user may narrow how a must-receive category arrives, never mute
// it.
var (
	ErrPreferenceNotFound     = errors.New("audience: preference not found")
	ErrPreferenceSilenceFloor = errors.New("audience: must-receive category cannot be silenced on any channel")
)

// preferenceKey is the 品类×渠道 slot under one audience.
type preferenceKey struct {
	audienceID string
	category   string
	channel    string
}

// PreferenceRegistry stores subscriber preferences — in-memory like the
// other registries; the shape is the API, not the storage.
type PreferenceRegistry struct {
	mu    sync.RWMutex
	prefs map[preferenceKey]Frequency
}

// NewPreferenceRegistry creates an empty preference registry.
func NewPreferenceRegistry() *PreferenceRegistry {
	return &PreferenceRegistry{prefs: make(map[preferenceKey]Frequency)}
}

// Set stores one preference, replacing any earlier word on the same
// audience×category×channel slot (latest wins, as with relations).
// Unset channels keep their category default; FreqNone is an explicit
// opt-out and is refused wherever the category floor forbids silence.
func (p *PreferenceRegistry) Set(pref Preference) error {
	if !idPattern.MatchString(pref.AudienceID) {
		return fmt.Errorf("audience: invalid audience id %q (want 1-64 chars of letters, digits, dot, dash, underscore)", pref.AudienceID)
	}
	if len(pref.Category) == 0 || len(pref.Category) > maxNameChars {
		return fmt.Errorf("audience: category must be 1-%d chars", maxNameChars)
	}
	if len(pref.Channel) == 0 || len(pref.Channel) > maxNameChars {
		return fmt.Errorf("audience: channel must be 1-%d chars", maxNameChars)
	}
	switch pref.Frequency {
	case FreqRealtime, FreqDaily, FreqWeekly, FreqNone:
	default:
		return fmt.Errorf("audience: unknown frequency %q (want realtime|daily|weekly|none)", pref.Frequency)
	}
	if pref.Frequency == FreqNone && !PolicyFor(pref.Category).Silence {
		return ErrPreferenceSilenceFloor
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	p.prefs[preferenceKey{audienceID: pref.AudienceID, category: pref.Category, channel: pref.Channel}] = pref.Frequency
	return nil
}

// Get returns one stored preference (not the default).
func (p *PreferenceRegistry) Get(audienceID, category, channel string) (Frequency, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	freq, ok := p.prefs[preferenceKey{audienceID: audienceID, category: category, channel: channel}]
	return freq, ok
}

// Effective resolves what actually applies on a slot: the stored
// preference when there is one, the category default otherwise.
func (p *PreferenceRegistry) Effective(audienceID, category, channel string) Frequency {
	if freq, ok := p.Get(audienceID, category, channel); ok {
		return freq
	}
	return PolicyFor(category).Frequency
}

// List returns every preference stored for one audience, ordered by
// category then channel so callers and tests see a stable view.
func (p *PreferenceRegistry) List(audienceID string) []Preference {
	p.mu.RLock()
	defer p.mu.RUnlock()

	out := make([]Preference, 0, len(p.prefs))
	for key, freq := range p.prefs {
		if key.audienceID != audienceID {
			continue
		}
		out = append(out, Preference{AudienceID: key.audienceID, Category: key.category, Channel: key.channel, Frequency: freq})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Category != out[j].Category {
			return out[i].Category < out[j].Category
		}
		return out[i].Channel < out[j].Channel
	})
	return out
}

// Clear removes a stored preference so the slot falls back to its
// category default. Clearing nothing is a caller error, not a no-op —
// callers should know they were aiming at air.
func (p *PreferenceRegistry) Clear(audienceID, category, channel string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	key := preferenceKey{audienceID: audienceID, category: category, channel: channel}
	if _, ok := p.prefs[key]; !ok {
		return ErrPreferenceNotFound
	}
	delete(p.prefs, key)
	return nil
}
