package audience

import (
	"errors"
	"sort"
	"strings"
)

// Intensity is the intrusion level of a channel (L0–L5).
type Intensity int

const (
	IntensityRSS   Intensity = iota // L0: pull-based, no push
	IntensityMail                   // L1: async, hours
	IntensityInbox                  // L2: passive, next open
	IntensityIM                     // L3: push, seconds
	IntensitySMS                    // L4: push, seconds, high intrusiveness
	IntensityPhone                  // L5: strong interrupt, immediate
)

// String implements fmt.Stringer.
func (i Intensity) String() string {
	switch i {
	case IntensityRSS:
		return "L0"
	case IntensityMail:
		return "L1"
	case IntensityInbox:
		return "L2"
	case IntensityIM:
		return "L3"
	case IntensitySMS:
		return "L4"
	case IntensityPhone:
		return "L5"
	default:
		return "L?"
	}
}

var (
	ErrInvalidIntensity = errors.New("invalid intensity")
	ErrInvalidUrgency   = errors.New("invalid urgency")
	ErrInvalidMode      = errors.New("invalid mode")
)

// ParseIntensity parses an intensity string (L0–L5).
func ParseIntensity(s string) (Intensity, error) {
	switch s {
	case "L0", "RSS":
		return IntensityRSS, nil
	case "L1", "Mail", "Email":
		return IntensityMail, nil
	case "L2", "Inbox", "Webhook":
		return IntensityInbox, nil
	case "L3", "IM":
		return IntensityIM, nil
	case "L4", "SMS":
		return IntensitySMS, nil
	case "L5", "Phone", "Call":
		return IntensityPhone, nil
	default:
		return 0, ErrInvalidIntensity
	}
}

// ChannelIntensity returns the default intensity for a channel name.
func ChannelIntensity(name string) Intensity {
	switch name {
	case "rss", "feed":
		return IntensityRSS
	case "email", "smtp", "mail":
		return IntensityMail
	case "webhook", "inbox":
		return IntensityInbox
	case "sms", "sms-duty", "smsduty":
		return IntensitySMS
	case "phone", "call", "phone-call":
		return IntensityPhone
	default:
		return IntensityIM // default to IM for unknown
	}
}

// Urgency is the message urgency level.
type Urgency int

const (
	UrgencyRoutine  Urgency = iota // L0–L1
	UrgencyNormal                  // L0–L2
	UrgencyUrgent                  // L0–L4
	UrgencyCritical                // L0–L5
)

func ParseUrgency(s string) (Urgency, error) {
	switch s {
	case "routine", "例行":
		return UrgencyRoutine, nil
	case "normal", "一般":
		return UrgencyNormal, nil
	case "urgent", "紧急":
		return UrgencyUrgent, nil
	case "critical", "关键":
		return UrgencyCritical, nil
	default:
		return 0, ErrInvalidUrgency
	}
}

func (u Urgency) MaxIntensity() Intensity {
	switch u {
	case UrgencyRoutine:
		return IntensityMail
	case UrgencyNormal:
		return IntensityInbox
	case UrgencyUrgent:
		return IntensitySMS
	case UrgencyCritical:
		return IntensityPhone
	default:
		return IntensityMail
	}
}

func (u Urgency) Allows(i Intensity) bool {
	return i <= u.MaxIntensity()
}

// DefaultUrgency returns the default urgency for a category.
func DefaultUrgency(category string) Urgency {
	switch category {
	case "alerts", "alert", "告警":
		return UrgencyUrgent
	case "domains", "domain", "域名":
		return UrgencyUrgent
	case "system", "系统":
		return UrgencyCritical
	case "marketing", "营销":
		return UrgencyRoutine
	default:
		return UrgencyNormal
	}
}

// Mode is the delivery mode.
type Mode int

const (
	ModeFixed Mode = iota
	ModeEscalation
	ModeParallel
)

func ParseMode(s string) (Mode, error) {
	switch s {
	case "fixed", "固定":
		return ModeFixed, nil
	case "escalation", "升级链":
		return ModeEscalation, nil
	case "parallel", "并行":
		return ModeParallel, nil
	default:
		return 0, ErrInvalidMode
	}
}

// String implements fmt.Stringer (contract vocabulary).
func (m Mode) String() string {
	switch m {
	case ModeFixed:
		return "fixed"
	case ModeEscalation:
		return "escalation"
	case ModeParallel:
		return "parallel"
	default:
		return "mode?"
	}
}

func DefaultMode(u Urgency, mustDeliver bool) Mode {
	if mustDeliver {
		return ModeParallel
	}
	switch u {
	case UrgencyUrgent, UrgencyCritical:
		return ModeEscalation
	default:
		return ModeFixed
	}
}

// DeliveryPolicy holds the §6 strategy config.
type DeliveryPolicy struct {
	urgencyByCategory  map[string]Urgency
	intensityByChannel map[string]Intensity
	modeByCategory     map[string]Mode
	// phoneEnabled is the §6.2 配置同意 half of the phone double-consent
	// (电话默认禁用): channels whose effective intensity is L5 are refused
	// until the operator lifts the gate. The audience half is binding a
	// phone contact surface at all (§5 filter). The gate keys on the
	// effective intensity, so a re-graded channel that lands on L5 needs
	// the flag too — deliberately grading a channel *down* out of L5 is
	// itself an explicit opt-in act in the config.
	phoneEnabled bool
}

// NewDeliveryPolicy builds the §6 policy from the three override tables
// plus the phone opt-in; an unparseable table value refuses to build (the
// caller turns that into a startup refusal), never a silent clamp.
func NewDeliveryPolicy(categoryUrgency, channelIntensity, categoryMode map[string]string, allowPhone bool) (*DeliveryPolicy, error) {
	d := &DeliveryPolicy{
		urgencyByCategory:  make(map[string]Urgency),
		intensityByChannel: make(map[string]Intensity),
		modeByCategory:     make(map[string]Mode),
		phoneEnabled:       allowPhone,
	}
	for category, raw := range categoryUrgency {
		u, err := ParseUrgency(raw)
		if err != nil {
			return nil, err
		}
		d.urgencyByCategory[strings.ToLower(category)] = u
	}
	for channel, raw := range channelIntensity {
		i, err := ParseIntensity(raw)
		if err != nil {
			return nil, err
		}
		d.intensityByChannel[strings.ToLower(channel)] = i
	}
	for category, raw := range categoryMode {
		m, err := ParseMode(raw)
		if err != nil {
			return nil, err
		}
		d.modeByCategory[strings.ToLower(category)] = m
	}
	return d, nil
}

func (d *DeliveryPolicy) UrgencyFor(category string) Urgency {
	if u, ok := d.urgencyByCategory[strings.ToLower(category)]; ok {
		return u
	}
	return DefaultUrgency(category)
}

func (d *DeliveryPolicy) IntensityOf(channel string) Intensity {
	if i, ok := d.intensityByChannel[strings.ToLower(channel)]; ok {
		return i
	}
	return ChannelIntensity(channel)
}

func (d *DeliveryPolicy) ModeFor(category string, mustDeliver bool) Mode {
	if m, ok := d.modeByCategory[strings.ToLower(category)]; ok {
		return m
	}
	return DefaultMode(d.UrgencyFor(category), mustDeliver)
}

type Match struct {
	Channel string
	Kept    bool
	Reason  string
}

func (d *DeliveryPolicy) MatchChannels(filter *Filter, audienceID, category string, channels []string) []Match {
	var out []Match
	for _, channel := range channels {
		var m Match
		m.Channel = channel
		switch {
		case filter != nil && !filter.Allow(audienceID, category, channel):
			m.Kept = false
			m.Reason = "filtered"
		case !d.phoneEnabled && d.IntensityOf(channel) == IntensityPhone:
			// §6.2: 电话默认禁用 — L5 waits for the operator's opt-in
			// even when the urgency window would reach it.
			m.Kept = false
			m.Reason = "phone_disabled"
		case !d.UrgencyFor(category).Allows(d.IntensityOf(channel)):
			m.Kept = false
			m.Reason = "intensity_exceeded"
		default:
			m.Kept = true
		}
		out = append(out, m)
	}
	return out
}

func (d *DeliveryPolicy) KeptChannels(filter *Filter, audienceID, category string, channels []string) []string {
	matches := d.MatchChannels(filter, audienceID, category, channels)
	var kept []string
	for _, m := range matches {
		if m.Kept {
			kept = append(kept, m.Channel)
		}
	}
	return kept
}

func (d *DeliveryPolicy) sortByIntensity(channels []string) []string {
	ordered := make([]string, len(channels))
	copy(ordered, channels)
	sort.SliceStable(ordered, func(i, j int) bool {
		return d.IntensityOf(ordered[i]) < d.IntensityOf(ordered[j])
	})
	return ordered
}
