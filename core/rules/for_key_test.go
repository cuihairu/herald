package rules

import "testing"

// ForGroupKey must degrade to a stable sentinel rather than panic when the
// environment carries a value encoding/json cannot represent.
func TestForGroupKeyUnmarshalableParams(t *testing.T) {
	if key := ForGroupKey(NewEnv("t", "info", "x", "y", map[string]any{"f": func() {}})); key != "unmarshalable" {
		t.Fatalf("expected the unmarshalable sentinel, got %q", key)
	}
}
