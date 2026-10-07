package dedup

import "testing"

func TestParseTier(t *testing.T) {
	for _, s := range []string{"once", "throttle", "always"} {
		got, err := ParseTier(s)
		if err != nil || got != Tier(s) {
			t.Errorf("ParseTier(%q) = %v, %v; want %v, nil", s, got, err, Tier(s))
		}
	}
	if _, err := ParseTier("hourly"); err == nil {
		t.Error("unknown tier accepted; must refuse, never clamp")
	}
}

func TestDefaultTier(t *testing.T) {
	if DefaultTier("system") != TierOnce {
		t.Error("system should default to once (一次性事件)")
	}
	if DefaultTier("alerts") != TierThrottle {
		t.Error("alerts should default to throttle (窗口节流)")
	}
	if DefaultTier("bills") != TierThrottle {
		t.Error("unlisted categories keep the historical throttle default")
	}
}

func TestNarrowOnlyNarrows(t *testing.T) {
	cases := []struct {
		base, request, want Tier
		note                string
	}{
		{TierThrottle, "", TierThrottle, "no preference keeps the default"},
		{TierAlways, TierOnce, TierOnce, "user narrows always to once"},
		{TierThrottle, TierOnce, TierOnce, "user narrows throttle to once"},
		{TierAlways, TierThrottle, TierThrottle, "user narrows always to throttle"},
		{TierOnce, TierAlways, TierOnce, "widening is refused — base stands"},
		{TierThrottle, TierAlways, TierThrottle, "widening to always is refused"},
		{TierOnce, TierThrottle, TierOnce, "widening to throttle is refused"},
		{TierThrottle, Tier("hourly"), TierThrottle, "unknown requests are ignored"},
	}
	for _, c := range cases {
		if got := Narrow(c.base, c.request); got != c.want {
			t.Errorf("Narrow(%v, %v) = %v, want %v (%s)", c.base, c.request, got, c.want, c.note)
		}
	}
}
