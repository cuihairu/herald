package apps

import (
	"errors"
	"net"
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
		{"short secret", "https://93.184.216.34/hook", "short-secret"},
		{"long secret", "https://93.184.216.34/hook", strings.Repeat("s", 129)},
	}
	for _, tc := range cases {
		if err := r.SetCallback("demo-app", tc.url, tc.secret); err == nil {
			t.Fatalf("%s: want refusal, got acceptance", tc.name)
		}
	}

	// The boundaries hold: exactly 16 and exactly 128 chars are lawful.
	for _, n := range []int{16, 128} {
		if err := r.SetCallback("demo-app", "https://93.184.216.34/hook", strings.Repeat("s", n)); err != nil {
			t.Fatalf("secret of %d chars: %v", n, err)
		}
	}

	if err := r.SetCallback("ghost", "https://93.184.216.34/hook", strings.Repeat("s", 32)); err != ErrUnknownApp {
		t.Fatalf("unknown app: want ErrUnknownApp, got %v", err)
	}
}

func TestSetCallbackRejectsPrivateNetworks(t *testing.T) {
	r, _ := NewRegistry([]SeedApp{{Name: "demo-app", Tokens: []SeedToken{{Secret: "s", Scopes: []string{"config"}}}}})
	secret := strings.Repeat("s", 32)
	for _, host := range []string{
		"127.0.0.1", "localhost", "10.1.2.3", "172.16.4.5", "192.168.7.8",
		"169.254.1.2", "[::1]", "[fe80::1]", "[fc00::1]",
		"100.64.1.2", "192.0.0.1", "198.18.0.1", "240.0.0.1",
	} {
		if err := r.SetCallback("demo-app", "http://"+host+"/hook", secret); err == nil {
			t.Fatalf("host %s: want private-network refusal", host)
		}
	}

	// Explicit deployment-level exemption allows localhost.
	r.SetCallbackPolicy(CallbackPolicy{AllowPrivate: true})
	if err := r.SetCallback("demo-app", "http://localhost/hook", secret); err != nil {
		t.Fatalf("allow localhost policy: %v", err)
	}
}

// TestDefaultDenyIsAbsoluteHardStop: the default deny list is the SSRF
// hard stop. A host on a default-deny network is refused no matter what;
// only AllowPrivate can re-admit it.
func TestDefaultDenyIsAbsoluteHardStop(t *testing.T) {
	r, _ := NewRegistry([]SeedApp{{Name: "demo-app", Tokens: []SeedToken{{Secret: "s", Scopes: []string{"config"}}}}})
	secret := strings.Repeat("s", 32)

	for _, host := range []string{
		"127.0.0.1", "10.1.2.3", "169.254.1.2", "[::1]",
		"100.64.1.2", "198.18.0.1",
	} {
		if err := r.SetCallback("demo-app", "http://"+host+"/hook", secret); err == nil {
			t.Fatalf("host %s: default deny must refuse", host)
		}
	}

	// AllowPrivate is the only exemption.
	r.SetCallbackPolicy(CallbackPolicy{AllowPrivate: true})
	for _, host := range []string{
		"127.0.0.1", "10.1.2.3", "169.254.1.2", "[::1]",
		"100.64.1.2", "198.18.0.1",
	} {
		if err := r.SetCallback("demo-app", "http://"+host+"/hook", secret); err != nil {
			t.Fatalf("host %s with AllowPrivate: want acceptance, got %v", host, err)
		}
	}
}

func TestCallbackLifecycle(t *testing.T) {
	r, _ := NewRegistry([]SeedApp{{Name: "demo-app", Tokens: []SeedToken{{Secret: "s", Scopes: []string{"config"}}}}})

	if _, ok := r.Callback("demo-app"); ok {
		t.Fatalf("fresh app: want no callback")
	}

	if err := r.SetCallback("demo-app", "https://93.184.216.34/hook?x=1", strings.Repeat("s", 32)); err != nil {
		t.Fatalf("set: %v", err)
	}
	cb, ok := r.Callback("demo-app")
	if !ok || cb.URL != "https://93.184.216.34/hook?x=1" || cb.Secret != strings.Repeat("s", 32) {
		t.Fatalf("get: got %v ok=%v", cb, ok)
	}

	// A replace wins wholesale.
	if err := r.SetCallback("demo-app", "http://93.184.216.34/cb", strings.Repeat("n", 16)); err != nil {
		t.Fatalf("replace: %v", err)
	}
	cb, _ = r.Callback("demo-app")
	if cb.URL != "http://93.184.216.34/cb" {
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

func TestValidateCallbackURLEdgeCases(t *testing.T) {
	r, _ := NewRegistry([]SeedApp{{Name: "demo-app", Tokens: []SeedToken{{Secret: "s", Scopes: []string{"config"}}}}})

	// A non-http scheme, a hostless URL and an unparseable URL all refuse at
	// the absolute-url gate.
	for _, u := range []string{"ftp://example.com/hook", "https://", "not a url"} {
		if err := r.ValidateCallbackURL(u); err == nil {
			t.Errorf("ValidateCallbackURL(%q) = nil, want refusal", u)
		}
	}

	// A URL whose host parses to an empty hostname (port only) passes the
	// absolute-url gate but is refused by the destination check.
	if err := r.ValidateCallbackURL("http://:8080/"); err == nil {
		t.Error("ValidateCallbackURL(http://:8080/) = nil, want host refusal")
	}
}

func TestValidateCallbackURLUnresolvableHost(t *testing.T) {
	r, _ := NewRegistry([]SeedApp{{Name: "demo-app", Tokens: []SeedToken{{Secret: "s", Scopes: []string{"config"}}}}})

	// The per-registry resolver seam makes the host unresolvable, so the
	// destination check has no IPs to validate and must refuse.
	r.LookupIP = func(string) ([]net.IP, error) { return nil, errors.New("dns down") }

	if err := r.ValidateCallbackURL("http://unresolvable.invalid/"); err == nil {
		t.Error("ValidateCallbackURL(unresolvable) = nil, want resolution refusal")
	}
}

// TestResolveCallbackURLReturnsValidatedIPs: the resolver must hand back
// exactly the addresses it validated, so the transport can pin them and
// skip a second DNS resolution.
func TestResolveCallbackURLReturnsValidatedIPs(t *testing.T) {
	r, _ := NewRegistry([]SeedApp{{Name: "demo-app", Tokens: []SeedToken{{Secret: "s", Scopes: []string{"config"}}}}})
	r.LookupIP = func(string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("93.184.216.34")}, nil
	}

	ips, err := r.ResolveCallbackURL("http://app.example.com/hook")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(ips) != 1 || !ips[0].Equal(net.ParseIP("93.184.216.34")) {
		t.Fatalf("ips = %v, want [93.184.216.34]", ips)
	}
}

// TestResolveCallbackURLRefusesDeniedHostname: a hostname whose resolver
// returns a denied network must be refused, and the refusal surfaces the
// private/loopback error rather than a resolution error.
func TestResolveCallbackURLRefusesDeniedHostname(t *testing.T) {
	r, _ := NewRegistry([]SeedApp{{Name: "demo-app", Tokens: []SeedToken{{Secret: "s", Scopes: []string{"config"}}}}})
	r.LookupIP = func(string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("10.1.2.3")}, nil
	}

	_, err := r.ResolveCallbackURL("http://app.example.com/hook")
	if err == nil || !strings.Contains(err.Error(), "private/loopback") {
		t.Fatalf("resolve denied hostname: want private refusal, got %v", err)
	}
}

// TestResolveCallbackURLCanRunParallel: the per-registry resolver seam is
// instance state, so parallel tests can stub it without a global race.
func TestResolveCallbackURLCanRunParallel(t *testing.T) {
	r, _ := NewRegistry([]SeedApp{{Name: "demo-app", Tokens: []SeedToken{{Secret: "s", Scopes: []string{"config"}}}}})
	r.LookupIP = func(string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("93.184.216.34")}, nil
	}

	t.Run("parallel stub", func(t *testing.T) {
		t.Parallel()
		if _, err := r.ResolveCallbackURL("http://app.example.com/hook"); err != nil {
			t.Fatalf("resolve: %v", err)
		}
	})
	t.Run("parallel stub 2", func(t *testing.T) {
		t.Parallel()
		if _, err := r.ResolveCallbackURL("http://app.example.com/hook"); err != nil {
			t.Fatalf("resolve: %v", err)
		}
	})
}
