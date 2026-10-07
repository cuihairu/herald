package audience

// Overlay is one namespace's typed overrides (§13.2 策略覆盖) applied on
// top of the operator's global §6 policy for a dispatch. All values are
// already parsed — the app config API refused anything unparseable at
// write time — so composing never fails and never re-validates.
type Overlay struct {
	// UrgencyByCategory pins a category's default urgency (the app's
	// registered category taxonomy) over the global category_urgency
	// table. Payload-supplied urgency wins at the dispatch face before
	// any table is consulted.
	UrgencyByCategory map[string]Urgency
	// ChannelIntensity re-grades channels inside the namespace.
	ChannelIntensity map[string]Intensity
	// ModeByCategory fixes the §6.3 delivery mode per category.
	ModeByCategory map[string]Mode
}

// Overlay returns a copy of the policy with the overrides applied.
// Precedence per key: override > base > taxonomy default. The phone
// gate is NOT overridable — §6.2 双重同意 is a safety rule, not a
// namespace preference, so allow_phone always comes from the base.
func (d *DeliveryPolicy) Overlay(ov Overlay) *DeliveryPolicy {
	out := &DeliveryPolicy{
		urgencyByCategory:  copyTable(d.urgencyByCategory, ov.UrgencyByCategory),
		intensityByChannel: copyTable(d.intensityByChannel, ov.ChannelIntensity),
		modeByCategory:     copyTable(d.modeByCategory, ov.ModeByCategory),
		phoneEnabled:       d.phoneEnabled,
	}
	return out
}

func copyTable[K comparable, V any](base, ov map[K]V) map[K]V {
	out := make(map[K]V, len(base)+len(ov))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range ov {
		out[k] = v
	}
	return out
}
