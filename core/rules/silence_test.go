package rules

import "testing"

func TestParseSilenceWindow(t *testing.T) {
	t.Run("ok", func(t *testing.T) {
		w, err := ParseSilenceWindow("22:30", "06:00")
		if err != nil {
			t.Fatalf("ParseSilenceWindow: %v", err)
		}
		if w.StartMin != 22*60+30 || w.EndMin != 6*60 {
			t.Fatalf("unexpected window: %+v", w)
		}
	})

	t.Run("bad end", func(t *testing.T) {
		if _, err := ParseSilenceWindow("22:30", "not-a-time"); err == nil {
			t.Fatal("a malformed end time must be rejected")
		}
	})

	t.Run("bad start", func(t *testing.T) {
		if _, err := ParseSilenceWindow("nope", "06:00"); err == nil {
			t.Fatal("a malformed start time must be rejected")
		}
	})

	t.Run("zero length", func(t *testing.T) {
		if _, err := ParseSilenceWindow("08:00", "08:00"); err == nil {
			t.Fatal("a zero-length window must be rejected")
		}
	})
}
