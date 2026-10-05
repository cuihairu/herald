package audience

import (
	"strings"
	"testing"
)

func TestRefAndIsUserRef(t *testing.T) {
	if got := Ref("alice"); got != "user:alice" {
		t.Errorf("Ref(alice) = %q, want user:alice", got)
	}
	for _, tc := range []struct {
		ref  string
		want bool
	}{
		{"user:alice", true},
		{"user:", true},
		{"alice", false},
		{"group:ops", false},
		{"", false},
	} {
		if got := IsUserRef(tc.ref); got != tc.want {
			t.Errorf("IsUserRef(%q) = %v, want %v", tc.ref, got, tc.want)
		}
	}
}

// validTables returns a legal pair of tables: an audience aggregating two
// recipients that share one provider plus a direct recipient.
func validTables() (map[string]Audience, map[string]Recipient) {
	audiences := map[string]Audience{
		"ops":    {Recipients: []string{"alice", "bob"}},
		"oncall": {Recipients: []string{"alice"}},
	}
	recipients := map[string]Recipient{
		"alice": {Endpoints: []Endpoint{
			{Type: "feishu", Target: "@alice"},
			{Type: "email", Target: "alice@example.com"},
		}},
		"bob": {Endpoints: []Endpoint{
			{Type: "feishu", Target: "@bob"},
			{Type: "sms-duty", Target: "13800000000"},
		}},
		"carol": {Endpoints: []Endpoint{
			{Type: "plain", Target: "carol"},
		}},
	}
	return audiences, recipients
}

func TestNewManagerValid(t *testing.T) {
	auds, recs := validTables()
	m, err := NewManager(auds, recs)
	if err != nil {
		t.Fatalf("NewManager(valid) = %v", err)
	}
	if m == nil {
		t.Fatal("expected non-nil manager")
	}

	// Empty tables are legal: a deployment without the user model resolves
	// nothing but starts fine.
	empty, err := NewManager(nil, nil)
	if err != nil {
		t.Fatalf("NewManager(nil, nil) = %v", err)
	}
	if eps, ok := empty.ExpandUser("anyone"); ok || len(eps) != 0 {
		t.Errorf("empty manager must resolve nothing, got %v, %v", eps, ok)
	}
}

func TestNewManagerRejects(t *testing.T) {
	_, recs := validTables()

	for _, tc := range []struct {
		name   string
		auds   map[string]Audience
		recs   map[string]Recipient
		wantIn string
	}{
		{"bad audience id", map[string]Audience{"bad id!": {Recipients: []string{"alice"}}}, recs, "invalid audience id"},
		{"audience without recipients", map[string]Audience{"ops": {}}, recs, "must list at least one id"},
		{"audience too large", map[string]Audience{"ops": {Recipients: make([]string, maxAudienceRecipients+1)}}, recs, "max is 64"},
		{"unknown recipient referenced", map[string]Audience{"ops": {Recipients: []string{"ghost"}}}, recs, "unknown recipient"},
		{"bad recipient id", nil, map[string]Recipient{"bad id!": {Endpoints: []Endpoint{{Type: "email", Target: "a@b.c"}}}}, "invalid recipient id"},
		{"recipient without endpoints", nil, map[string]Recipient{"alice": {}}, "must list at least one entry"},
		{"too many endpoints", nil, map[string]Recipient{"alice": {Endpoints: make([]Endpoint, maxEndpoints+1)}}, "max is 64"},
		{"empty endpoint type", nil, map[string]Recipient{"alice": {Endpoints: []Endpoint{{Type: "  ", Target: "x"}}}}, "endpoint 0 type"},
		{"oversized endpoint type", nil, map[string]Recipient{"alice": {Endpoints: []Endpoint{{Type: strings.Repeat("t", maxNameChars+1), Target: "x"}}}}, "endpoint 0 type"},
		{"empty endpoint target", nil, map[string]Recipient{"alice": {Endpoints: []Endpoint{{Type: "email", Target: " "}}}}, "endpoint 0 target"},
		{"oversized endpoint target", nil, map[string]Recipient{"alice": {Endpoints: []Endpoint{{Type: "email", Target: strings.Repeat("t", maxNameChars+1)}}}}, "endpoint 0 target"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewManager(tc.auds, tc.recs)
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.Contains(err.Error(), tc.wantIn) {
				t.Errorf("error %q does not contain %q", err.Error(), tc.wantIn)
			}
		})
	}
}

func TestExpandUser(t *testing.T) {
	auds, recs := validTables()
	m, err := NewManager(auds, recs)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	t.Run("audience aggregates recipient endpoints", func(t *testing.T) {
		eps, ok := m.ExpandUser("ops")
		if !ok {
			t.Fatal("expected the audience to resolve")
		}
		if len(eps) != 4 {
			t.Fatalf("expected 4 endpoints (alice 2 + bob 2), got %v", eps)
		}
	})

	t.Run("direct recipient resolves", func(t *testing.T) {
		eps, ok := m.ExpandUser("carol")
		if !ok {
			t.Fatal("expected the recipient to resolve")
		}
		if len(eps) != 1 || eps[0].Type != "plain" || eps[0].Target != "carol" {
			t.Errorf("got %v", eps)
		}
	})

	t.Run("unknown user does not resolve", func(t *testing.T) {
		if eps, ok := m.ExpandUser("ghost"); ok || len(eps) != 0 {
			t.Errorf("unknown user must not resolve, got %v, %v", eps, ok)
		}
	})
}

func TestExpandUserAudiencePrecedence(t *testing.T) {
	// The same id in both tables: the audience wins, mirroring the
	// documented "audiences first, then recipients" resolution.
	auds := map[string]Audience{
		"shared": {Recipients: []string{"alice"}},
	}
	recs := map[string]Recipient{
		"shared": {Endpoints: []Endpoint{{Type: "plain", Target: "direct"}}},
		"alice":  {Endpoints: []Endpoint{{Type: "email", Target: "a@b.c"}}},
	}
	m, err := NewManager(auds, recs)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	eps, ok := m.ExpandUser("shared")
	if !ok {
		t.Fatal("expected the id to resolve")
	}
	if len(eps) != 1 || eps[0].Target != "a@b.c" {
		t.Errorf("audience must take precedence, got %v", eps)
	}
}
