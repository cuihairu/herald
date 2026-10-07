package api

import (
	"testing"

	"github.com/cuihairu/herald/core/service"
)

func TestNotifyIdempotencyStore(t *testing.T) {
	key := func(prefix string, i int) string { return prefix + string(rune('0'+i)) }
	res := func(i int) service.ProcessResult {
		return service.ProcessResult{NotificationID: "n" + key("", i)}
	}

	t.Run("unknown key misses", func(t *testing.T) {
		s := newNotifyIdempotency(0)
		if _, ok := s.get("nope"); ok {
			t.Error("expected miss for unknown key")
		}
	})

	t.Run("roundtrip and replay record", func(t *testing.T) {
		s := newNotifyIdempotency(0)
		s.put("k1", res(1))
		got, ok := s.get("k1")
		if !ok {
			t.Fatal("expected hit for recorded key")
		}
		if got.NotificationID != "n1" {
			t.Errorf("expected n1, got %q", got.NotificationID)
		}
	})

	t.Run("re-put keeps first record", func(t *testing.T) {
		s := newNotifyIdempotency(0)
		s.put("k1", res(1))
		s.put("k1", res(2))
		got, ok := s.get("k1")
		if !ok {
			t.Fatal("expected hit after re-put")
		}
		if got.NotificationID != "n1" {
			t.Errorf("expected first record n1, got %q", got.NotificationID)
		}
		if len(s.keys) != 1 {
			t.Errorf("expected single entry in eviction order, got %v", s.keys)
		}
	})

	t.Run("evicts oldest beyond capacity", func(t *testing.T) {
		s := newNotifyIdempotency(2)
		s.put("k1", res(1))
		s.put("k2", res(2))
		s.put("k3", res(3))
		if _, ok := s.get("k1"); ok {
			t.Error("expected k1 evicted")
		}
		for _, k := range []string{"k2", "k3"} {
			if _, ok := s.get(k); !ok {
				t.Errorf("expected %s retained", k)
			}
		}
		if len(s.keys) != 2 {
			t.Errorf("expected 2 entries in eviction order, got %v", s.keys)
		}
	})

	t.Run("nonpositive capacity falls back to default", func(t *testing.T) {
		if s := newNotifyIdempotency(0); s.cap != defaultNotifyIdempotencyCap {
			t.Errorf("expected default cap %d, got %d", defaultNotifyIdempotencyCap, s.cap)
		}
		if s := newNotifyIdempotency(-1); s.cap != defaultNotifyIdempotencyCap {
			t.Errorf("expected default cap %d for negative input, got %d", defaultNotifyIdempotencyCap, s.cap)
		}
	})

	t.Run("custom capacity respected", func(t *testing.T) {
		s := newNotifyIdempotency(1)
		s.put("k1", res(1))
		s.put("k2", res(2))
		if _, ok := s.get("k1"); ok {
			t.Error("expected k1 evicted at cap 1")
		}
		if _, ok := s.get("k2"); !ok {
			t.Error("expected k2 retained")
		}
	})
}
