package api

import (
	"testing"
	"time"

	"github.com/cuihairu/herald/core/audience"
	"github.com/cuihairu/herald/core/digest"
)

// TestDigestWiringAndFlush: a config carrying a digest aggregator wires
// it into the notification service at construction, and the server's
// flush passthrough forwards batches to it (an empty batch is a no-op).
func TestDigestWiringAndFlush(t *testing.T) {
	daily, err := digest.ParseDaily("09:00", time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	weekly, err := digest.ParseWeekly("Mon 09:00", time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	env := newTestEnv(t, func(c *Config) {
		c.Digest = digest.NewAggregator(daily, weekly)
		c.DigestPrefs = audience.NewPreferenceRegistry()
	})
	if err := env.server.FlushDigestBatch(digest.Batch{}); err != nil {
		t.Fatalf("empty batch flush = %v, want nil", err)
	}
}
