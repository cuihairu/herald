package apps

import (
	"errors"
	"testing"
	"time"

	"github.com/cuihairu/herald/core/audience"
	"github.com/cuihairu/herald/core/dedup"
)

func TestPoliciesUpdateAndReadBack(t *testing.T) {
	r, err := NewRegistry([]SeedApp{{Name: "demo-app", Tokens: []SeedToken{
		{Secret: "s", Scopes: []string{"config"}},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	err = r.UpdatePolicies("demo-app", func(p *AppPolicies) {
		p.ChannelIntensity = map[string]audience.Intensity{"telegram": audience.IntensityRSS}
		p.ModeByCategory = map[string]audience.Mode{"alerts": audience.ModeEscalation}
		p.AckTimeout = 3 * time.Minute
		p.DedupTiers = map[string]dedup.Tier{"alerts": dedup.TierAlways}
		p.DedupWindows = map[string]time.Duration{"alerts": 30 * time.Minute}
	})
	if err != nil {
		t.Fatal(err)
	}

	got, ok := r.Policies("demo-app")
	if !ok {
		t.Fatal("Policies = not ok")
	}
	if got.ChannelIntensity["telegram"] != audience.IntensityRSS {
		t.Errorf("intensity = %v, want L0 override", got.ChannelIntensity)
	}
	if got.ModeByCategory["alerts"] != audience.ModeEscalation {
		t.Errorf("mode = %v, want escalation", got.ModeByCategory)
	}
	if got.AckTimeout != 3*time.Minute {
		t.Errorf("ack timeout = %v, want 3m", got.AckTimeout)
	}
	if got.DedupTiers["alerts"] != dedup.TierAlways {
		t.Errorf("tier = %v, want always", got.DedupTiers)
	}
	if got.DedupWindows["alerts"] != 30*time.Minute {
		t.Errorf("window = %v, want 30m", got.DedupWindows)
	}

	// The read-back is a deep copy: mutating it must not leak in.
	got.ChannelIntensity["telegram"] = audience.IntensityPhone
	again, _ := r.Policies("demo-app")
	if again.ChannelIntensity["telegram"] != audience.IntensityRSS {
		t.Error("mutating the read-back changed the stored table")
	}

	// Family replacement clears absent keys (PUT semantics).
	err = r.UpdatePolicies("demo-app", func(p *AppPolicies) {
		p.ChannelIntensity = map[string]audience.Intensity{"sms": audience.IntensitySMS}
	})
	if err != nil {
		t.Fatal(err)
	}
	again, _ = r.Policies("demo-app")
	if _, ok := again.ChannelIntensity["telegram"]; ok {
		t.Error("replaced family kept an old key")
	}
	if again.ModeByCategory["alerts"] != audience.ModeEscalation {
		t.Error("untouched family was disturbed by the intensity PUT")
	}

	if err := r.UpdatePolicies("ghost", func(*AppPolicies) {}); !errors.Is(err, ErrUnknownApp) {
		t.Errorf("UpdatePolicies(ghost) = %v, want ErrUnknownApp", err)
	}
	if _, ok := r.Policies("ghost"); ok {
		t.Error("unknown app policies resolved")
	}
}
