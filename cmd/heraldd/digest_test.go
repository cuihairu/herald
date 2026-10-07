package main

import (
	"errors"
	"testing"

	"github.com/cuihairu/herald/core/digest"
)

type fakeDigestFlusher struct {
	err   error
	calls []digest.Batch
}

func (f *fakeDigestFlusher) FlushDigestBatch(b digest.Batch) error {
	f.calls = append(f.calls, b)
	return f.err
}

// TestFlushDigestCallbackForwardsAndLogs covers both paths of the flip
// loop's flush callback: a successful flush rides through, a failed one
// is logged (not panicked on) so the loop keeps flipping later windows.
func TestFlushDigestCallbackForwardsAndLogs(t *testing.T) {
	f := &fakeDigestFlusher{}
	flush := flushDigest(f)

	batch := digest.Batch{Key: digest.Key{AudienceID: "alice", Category: "bills"}, Mode: digest.Daily}
	flush(batch)
	f.err = errors.New("no route")
	flush(digest.Batch{Key: digest.Key{AudienceID: "bob", Category: "notices"}, Mode: digest.Weekly})

	if len(f.calls) != 2 {
		t.Fatalf("flusher saw %d batches, want 2", len(f.calls))
	}
	if f.calls[0].Key != (digest.Key{AudienceID: "alice", Category: "bills"}) ||
		f.calls[1].Key != (digest.Key{AudienceID: "bob", Category: "notices"}) {
		t.Errorf("batches arrived out of shape: %+v", f.calls)
	}
}
