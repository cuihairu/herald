package apps

import (
	"strings"
	"testing"
)

func TestSetCallbackValidation(t *testing.T) {
	r, _ := NewRegistry([]SeedApp{{Name: "demo-app", Tokens: []SeedToken{{Secret: "s", Scopes: []string{"config"}}}}})

	cases := []struct {
		name, url, secret string
	}{
		{"relative url", "/hook", strings.Repeat("s", 32)},
		{"ftp url", "ftp://example.com/hook", strings.Repeat("s", 32)},
		{"hostless url", "https://", strings.Repeat("s", 32)},
		{"short secret", "https://example.com/hook", "short-secret"},
		{"long secret", "https://example.com/hook", strings.Repeat("s", 129)},
	}
	for _, tc := range cases {
		if err := r.SetCallback("demo-app", tc.url, tc.secret); err == nil {
			t.Fatalf("%s: want refusal, got acceptance", tc.name)
		}
	}

	// The boundaries hold: exactly 16 and exactly 128 chars are lawful.
	for _, n := range []int{16, 128} {
		if err := r.SetCallback("demo-app", "https://example.com/hook", strings.Repeat("s", n)); err != nil {
			t.Fatalf("secret of %d chars: %v", n, err)
		}
	}

	if err := r.SetCallback("ghost", "https://example.com/hook", strings.Repeat("s", 32)); err != ErrUnknownApp {
		t.Fatalf("unknown app: want ErrUnknownApp, got %v", err)
	}
}

func TestCallbackLifecycle(t *testing.T) {
	r, _ := NewRegistry([]SeedApp{{Name: "demo-app", Tokens: []SeedToken{{Secret: "s", Scopes: []string{"config"}}}}})

	if _, ok := r.Callback("demo-app"); ok {
		t.Fatalf("fresh app: want no callback")
	}

	if err := r.SetCallback("demo-app", "https://example.com/hook?x=1", strings.Repeat("s", 32)); err != nil {
		t.Fatalf("set: %v", err)
	}
	cb, ok := r.Callback("demo-app")
	if !ok || cb.URL != "https://example.com/hook?x=1" || cb.Secret != strings.Repeat("s", 32) {
		t.Fatalf("get: got %v ok=%v", cb, ok)
	}

	// A replace wins wholesale.
	if err := r.SetCallback("demo-app", "http://other.example.com/cb", strings.Repeat("n", 16)); err != nil {
		t.Fatalf("replace: %v", err)
	}
	cb, _ = r.Callback("demo-app")
	if cb.URL != "http://other.example.com/cb" {
		t.Fatalf("replace: want the new url, got %q", cb.URL)
	}

	if err := r.ClearCallback("demo-app"); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if _, ok := r.Callback("demo-app"); ok {
		t.Fatalf("after clear: want no callback")
	}
	if err := r.ClearCallback("demo-app"); err != nil {
		t.Fatalf("clear is idempotent: %v", err)
	}
	if err := r.ClearCallback("ghost"); err != ErrUnknownApp {
		t.Fatalf("clear unknown: want ErrUnknownApp, got %v", err)
	}
}
