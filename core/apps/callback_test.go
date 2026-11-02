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

func TestSetCallbackRejectsPrivateNetworks(t *testing.T) {
	r, _ := NewRegistry([]SeedApp{{Name: "demo-app", Tokens: []SeedToken{{Secret: "s", Scopes: []string{"config"}}}}})
	secret := strings.Repeat("s", 32)
	for _, host := range []string{
		"127.0.0.1", "localhost", "10.1.2.3", "172.16.4.5", "192.168.7.8",
		"169.254.1.2", "[::1]", "[fe80::1]", "[fc00::1]",
	} {
		if err := r.SetCallback("demo-app", "http://"+host+"/hook", secret); err == nil {
			t.Fatalf("host %s: want private-network refusal", host)
		}
	}

	// Specific allow list: only 10.0.0.0/8 is exempted, and other ranges
	// remain refused.
	r.SetCallbackPolicy(CallbackPolicy{AllowCIDRs: []string{"10.0.0.0/8"}})
	if err := r.SetCallback("demo-app", "http://10.1.2.3/hook", secret); err != nil {
		t.Fatalf("allow list 10/8: %v", err)
	}
	if err := r.SetCallback("demo-app", "http://127.0.0.1/hook", secret); err == nil {
		t.Fatal("loopback is not exempted by the 10/8 allow list")
	}

	// Explicit deployment-level exemption allows localhost.
	r.SetCallbackPolicy(CallbackPolicy{AllowPrivate: true})
	if err := r.SetCallback("demo-app", "http://localhost/hook", secret); err != nil {
		t.Fatalf("allow localhost policy: %v", err)
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
	if err := r.SetCallback("demo-app", "http://example.com/cb", strings.Repeat("n", 16)); err != nil {
		t.Fatalf("replace: %v", err)
	}
	cb, _ = r.Callback("demo-app")
	if cb.URL != "http://example.com/cb" {
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
