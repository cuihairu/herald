package audience

import (
	"errors"
	"strings"
	"testing"
)

// TestSubscribeEnforcesContract: the subscription contract is forced, not
// trusted — the stored relation is always unsubscribable and never
// must-deliver, and Enroll-typed relations are refused at the Subscribe
// door.
func TestSubscribeEnforcesContract(t *testing.T) {
	g := NewRegistry()

	rel := Relation{
		AudienceID: "alice",
		Category:   "alerts",
		Channel:    "telegram",
		Type:       RelationSubscription,
		Source:     SourcePreferenceCenter,
		Policy:     Policy{AllowUnsubscribe: false, MustDeliver: true}, // caller noise, ignored
	}
	if err := g.Subscribe(rel); err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}

	got, ok := g.Lookup("alice", "alerts", "telegram")
	if !ok {
		t.Fatal("Lookup() = missing, want the stored subscription")
	}
	if !got.Policy.AllowUnsubscribe || got.Policy.MustDeliver {
		t.Errorf("policy = %+v, want forced {AllowUnsubscribe: true, MustDeliver: false}", got.Policy)
	}
	if got.Type != RelationSubscription {
		t.Errorf("type = %q, want subscription", got.Type)
	}

	// An enrollment at the Subscribe door is a caller bug.
	enroll := Relation{AudienceID: "alice", Category: "alerts", Channel: "email", Type: RelationEnrollment, Source: SourceAdmin}
	if err := g.Subscribe(enroll); err == nil {
		t.Fatal("Subscribe(enrollment) = nil, want a routing error pointing at Enroll")
	}
	if _, ok := g.Lookup("alice", "alerts", "email"); ok {
		t.Error("rejected enrollment was stored anyway")
	}
}

// TestEnrollBottomLines: the §4 bottom lines are enforced — must-deliver
// and unsubscribable are mutually exclusive, and an enrollment that is
// neither must-deliver nor unsubscribable has no lawful shape.
func TestEnrollBottomLines(t *testing.T) {
	g := NewRegistry()

	cases := []struct {
		name    string
		policy  Policy
		wantErr bool
	}{
		{"must-deliver: marked, not unsubscribable", Policy{AllowUnsubscribe: false, MustDeliver: true}, false},
		{"marketing: unsubscribable, not must-deliver", Policy{AllowUnsubscribe: true, MustDeliver: false}, false},
		{"both flags: no lawful shape", Policy{AllowUnsubscribe: true, MustDeliver: true}, true},
		{"neither flag: no lawful shape", Policy{AllowUnsubscribe: false, MustDeliver: false}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rel := Relation{
				AudienceID: "oncall",
				Category:   "domains",
				Channel:    "email",
				Type:       RelationEnrollment,
				Source:     SourceAdmin,
				Policy:     tc.policy,
			}
			err := g.Enroll(rel)
			if tc.wantErr && err == nil {
				t.Fatal("Enroll() = nil, want a bottom-line rejection")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("Enroll() error = %v", err)
			}
		})
	}

	// A subscription at the Enroll door is a caller bug.
	sub := Relation{AudienceID: "oncall", Category: "alerts", Channel: "sms", Type: RelationSubscription, Source: SourceBot}
	if err := g.Enroll(sub); err == nil {
		t.Fatal("Enroll(subscription) = nil, want a routing error pointing at Subscribe")
	}
}

// TestRelationFieldValidation: ids and bounded fields are checked the same
// way the static tables are.
func TestRelationFieldValidation(t *testing.T) {
	g := NewRegistry()

	cases := []struct {
		name string
		rel  Relation
	}{
		{"bad audience id", Relation{AudienceID: "-bad", Category: "alerts", Channel: "email", Type: RelationSubscription, Source: SourceBot}},
		{"empty category", Relation{AudienceID: "alice", Category: "", Channel: "email", Type: RelationSubscription, Source: SourceBot}},
		{"empty channel", Relation{AudienceID: "alice", Category: "alerts", Channel: "", Type: RelationSubscription, Source: SourceBot}},
		{"empty source", Relation{AudienceID: "alice", Category: "alerts", Channel: "email", Type: RelationSubscription, Source: ""}},
		{"oversized source", Relation{AudienceID: "alice", Category: "alerts", Channel: "email", Type: RelationSubscription, Source: strings.Repeat("x", 65)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := g.Subscribe(tc.rel); err == nil {
				t.Fatalf("Subscribe(%s) = nil, want a validation error", tc.name)
			}
		})
	}
}

// TestTerminateRespectsBottomLine: unsubscribable relations come off
// freely, must-deliver relations refuse, and unknown slots report
// ErrRelationNotFound. The terminated relation comes back for the audit
// trail.
func TestTerminateRespectsBottomLine(t *testing.T) {
	g := NewRegistry()
	sub := Relation{AudienceID: "alice", Category: "alerts", Channel: "telegram", Type: RelationSubscription, Source: SourceBot}
	must := Relation{AudienceID: "alice", Category: "system", Channel: "sms", Type: RelationEnrollment, Source: SourceAdmin, Policy: Policy{MustDeliver: true}}
	if err := g.Subscribe(sub); err != nil {
		t.Fatal(err)
	}
	if err := g.Enroll(must); err != nil {
		t.Fatal(err)
	}

	gone, err := g.Terminate("alice", "alerts", "telegram")
	if err != nil {
		t.Fatalf("Terminate(subscription) error = %v", err)
	}
	if gone.Category != "alerts" || gone.Type != RelationSubscription {
		t.Errorf("terminated = %+v, want the removed subscription back", gone)
	}
	if _, ok := g.Lookup("alice", "alerts", "telegram"); ok {
		t.Error("terminated relation still registered")
	}

	if _, err := g.Terminate("alice", "system", "sms"); err == nil {
		t.Fatal("Terminate(must-deliver) = nil, want the bottom-line refusal")
	}
	if _, ok := g.Lookup("alice", "system", "sms"); !ok {
		t.Error("refused terminate removed the relation anyway")
	}

	if _, err := g.Terminate("alice", "alerts", "rss"); !errors.Is(err, ErrRelationNotFound) {
		t.Fatalf("Terminate(unknown) = %v, want ErrRelationNotFound", err)
	}
}

// TestReplaceOnSameSlot: a later word on the same audience×category×channel
// slot replaces the earlier relation — the latest registration wins.
func TestReplaceOnSameSlot(t *testing.T) {
	g := NewRegistry()
	first := Relation{AudienceID: "alice", Category: "bills", Channel: "email", Type: RelationSubscription, Source: SourceBot}
	if err := g.Subscribe(first); err != nil {
		t.Fatal(err)
	}
	second := Relation{AudienceID: "alice", Category: "bills", Channel: "email", Type: RelationEnrollment, Source: SourceAdmin, Policy: Policy{AllowUnsubscribe: true}}
	if err := g.Enroll(second); err != nil {
		t.Fatal(err)
	}

	got, ok := g.Lookup("alice", "bills", "email")
	if !ok || got.Type != RelationEnrollment || got.Source != SourceAdmin {
		t.Errorf("slot = %+v ok=%v, want the replacement enrollment", got, ok)
	}
	if subs := g.RelationsByType("alice", RelationSubscription); len(subs) != 0 {
		t.Errorf("subscriptions = %d, want 0 after the slot flipped", len(subs))
	}
}

// TestQueriesByType: the by-audience and by-type views never mix the two
// kinds, and unknown audiences come back empty rather than nil-guarded
// specials.
func TestQueriesByType(t *testing.T) {
	g := NewRegistry()
	seed := []Relation{
		{AudienceID: "alice", Category: "alerts", Channel: "telegram", Type: RelationSubscription, Source: SourceBot},
		{AudienceID: "alice", Category: "bills", Channel: "email", Type: RelationSubscription, Source: SourcePreferenceCenter},
		{AudienceID: "alice", Category: "system", Channel: "sms", Type: RelationEnrollment, Source: SourceAdmin, Policy: Policy{MustDeliver: true}},
		{AudienceID: "bob", Category: "notices", Channel: "email", Type: RelationEnrollment, Source: "app:ferry", Policy: Policy{AllowUnsubscribe: true}},
	}
	for _, rel := range seed {
		if rel.Type == RelationSubscription {
			if err := g.Subscribe(rel); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := g.Enroll(rel); err != nil {
			t.Fatal(err)
		}
	}

	if all := g.Relations("alice"); len(all) != 3 {
		t.Errorf("Relations(alice) = %d, want 3", len(all))
	}
	if subs := g.RelationsByType("alice", RelationSubscription); len(subs) != 2 {
		t.Errorf("subscriptions = %d, want 2", len(subs))
	}
	if enrolls := g.RelationsByType("alice", RelationEnrollment); len(enrolls) != 1 || enrolls[0].Category != "system" {
		t.Errorf("enrollments = %+v, want only the system relation", enrolls)
	}
	if all := g.Relations("ghost"); len(all) != 0 {
		t.Errorf("Relations(unknown) = %d, want 0", len(all))
	}
}
