package audience

import (
	"errors"
	"strings"
	"testing"

	"github.com/cuihairu/herald/core/audit"
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

// TestRegistryAuditTrail: accepted writes emit typed events; rejected
// ones emit nothing. Terminate reports the removed relation's kind.
func TestRegistryAuditTrail(t *testing.T) {
	g := NewRegistry()
	st := audit.New(0)
	g.SetRecorder(st)

	sub := Relation{AudienceID: "alice", Category: "alerts", Channel: "telegram", Type: RelationSubscription, Source: SourceBot}
	if err := g.Subscribe(sub); err != nil {
		t.Fatal(err)
	}
	enr := Relation{AudienceID: "alice", Category: "system", Channel: "email", Type: RelationEnrollment, Source: SourceAdmin, Policy: Policy{MustDeliver: true}}
	if err := g.Enroll(enr); err != nil {
		t.Fatal(err)
	}
	if err := g.Subscribe(Relation{AudienceID: "alice", Category: "x", Channel: "email", Type: RelationEnrollment, Source: SourceBot}); err == nil {
		t.Fatal("want door error")
	}
	if _, err := g.Terminate("alice", "alerts", "telegram"); err != nil {
		t.Fatal(err)
	}

	if got := st.ListByRelationType("subscription"); len(got) != 2 || got[0].Kind != audit.RelationSubscribe || got[1].Kind != audit.RelationTerminate {
		t.Errorf("subscription trail = %+v, want subscribe then terminate", got)
	}
	if got := st.ListByRelationType("enrollment"); len(got) != 1 || got[0].Kind != audit.RelationEnroll {
		t.Errorf("enrollment trail = %+v, want exactly one enroll", got)
	}
	if got := st.List(); len(got) != 3 {
		t.Errorf("total events = %d, want 3 (rejected write recorded nothing)", len(got))
	}

	g.SetRecorder(nil)
	if err := g.Subscribe(Relation{AudienceID: "bob", Category: "a", Channel: "email", Type: RelationSubscription, Source: SourceBot}); err != nil {
		t.Fatal(err)
	}
	if got := st.List(); len(got) != 3 {
		t.Errorf("events after recorder cleared = %d, want still 3", len(got))
	}
}

// TestTerminateUnsubscribeHook: the §13.5 backflow hook sees exactly the
// successful terminations, with the removed relation and the acting
// entry — refusals and unknown relations report nothing.
func TestTerminateUnsubscribeHook(t *testing.T) {
	g := NewRegistry()
	sub := Relation{AudienceID: "alice", Category: "alerts", Channel: "telegram", Type: RelationSubscription, Source: "app:ferry"}
	must := Relation{AudienceID: "alice", Category: "system", Channel: "sms", Type: RelationEnrollment, Source: SourceAdmin, Policy: Policy{MustDeliver: true}}
	if err := g.Subscribe(sub); err != nil {
		t.Fatal(err)
	}
	if err := g.Enroll(must); err != nil {
		t.Fatal(err)
	}

	var seen []Relation
	var actors []string
	g.SetUnsubscribeHook(func(rel Relation, actor string) {
		seen = append(seen, rel)
		actors = append(actors, actor)
	})

	if _, err := g.TerminateFor("alice", "alerts", "telegram", "preference_center"); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 1 || seen[0].AudienceID != "alice" || seen[0].Source != "app:ferry" || actors[0] != "preference_center" {
		t.Fatalf("hook = %v/%v, want the removed app:ferry relation via preference_center", seen, actors)
	}

	// A must-deliver refusal and an unknown slot fire nothing.
	if _, err := g.TerminateFor("alice", "system", "sms", "preference_center"); err == nil {
		t.Fatal("must-deliver terminate = nil, want refusal")
	}
	if _, err := g.TerminateFor("ghost", "alerts", "telegram", "x"); err == nil {
		t.Fatal("unknown terminate = nil, want ErrRelationNotFound")
	}
	if len(seen) != 1 {
		t.Fatalf("refusals must not reach the hook, got %v", seen)
	}

	// Clearing the hook detaches it.
	g.SetUnsubscribeHook(nil)
	if err := g.Subscribe(sub); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Terminate("alice", "alerts", "telegram"); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 1 {
		t.Fatalf("cleared hook still firing, got %v", seen)
	}
}
