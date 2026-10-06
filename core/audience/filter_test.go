package audience

import "testing"

// seedSurfaces plants surfaces directly (the public binding flow can
// only store active surfaces — pending is a seeded lifecycle state the
// filter must still refuse).
func seedSurfaces(s *SurfaceRegistry, surfaces ...ContactSurface) {
	for _, surf := range surfaces {
		s.mu.Lock()
		s.slot(surf.AudienceID)[surf.Channel] = surf
		s.mu.Unlock()
	}
}

// TestFilterPassesLegacyAudiences: §14 compatibility — an audience with
// no contact surfaces at all is pre-model traffic and passes untouched,
// relation or not.
func TestFilterPassesLegacyAudiences(t *testing.T) {
	f := NewFilter(NewRegistry(), NewSurfaceRegistry())
	if !f.Allow("legacy", "alerts", "telegram") {
		t.Error("Allow() = false for a surface-less audience, want the §14 compat passthrough")
	}
}

// TestFilterIntersection: for a surface-bearing audience the target must
// clear binding (active surface on that exact channel) AND relation (a
// slot holding a permitting relation) — every partial combination
// refuses, the full intersection passes.
func TestFilterIntersection(t *testing.T) {
	relations := NewRegistry()
	surfaces := NewSurfaceRegistry()
	seedSurfaces(surfaces,
		ContactSurface{AudienceID: "alice", Channel: "telegram", Target: "@a", Status: SurfaceActive},
		ContactSurface{AudienceID: "alice", Channel: "email", Target: "a@x", Status: SurfacePending},
		ContactSurface{AudienceID: "alice", Channel: "sms", Target: "139", Status: SurfaceInvalid},
		ContactSurface{AudienceID: "alice", Channel: "webhook", Target: "https://x", Status: SurfaceActive},
	)
	if err := relations.Subscribe(Relation{
		AudienceID: "alice", Category: "alerts", Channel: "telegram",
		Type: RelationSubscription, Source: SourcePreferenceCenter,
	}); err != nil {
		t.Fatal(err)
	}
	f := NewFilter(relations, surfaces)

	cases := []struct {
		name     string
		category string
		channel  string
		want     bool
	}{
		{"active surface × subscription relation", "alerts", "telegram", true},
		{"pending surface refuses", "alerts", "email", false},
		{"invalid surface refuses", "alerts", "sms", false},
		{"active surface but no relation refuses", "alerts", "webhook", false},
		{"no surface on the channel refuses", "alerts", "rss", false},
		{"missing category fails closed", "", "telegram", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := f.Allow("alice", tc.category, tc.channel); got != tc.want {
				t.Errorf("Allow() = %t, want %t", got, tc.want)
			}
		})
	}
}

// TestFilterRechecksMatrixAtSendTime: §5 sends re-validate the matrix —
// a relation seeded outside the Enroll door (registry loaded from an
// external store) that the matrix would refuse is still dropped at
// delivery, while its lawful twin passes.
func TestFilterRechecksMatrixAtSendTime(t *testing.T) {
	relations := NewRegistry()
	surfaces := NewSurfaceRegistry()
	seedSurfaces(surfaces,
		ContactSurface{AudienceID: "bob", Channel: "telegram", Target: "@b", Status: SurfaceActive},
		ContactSurface{AudienceID: "bob", Channel: "webhook", Target: "https://b", Status: SurfaceActive},
		ContactSurface{AudienceID: "bob", Channel: "rss", Target: "", Status: SurfaceActive},
	)
	// Planted the way an external seeder would write — no Enroll door in
	// between, so the matrix was never consulted at write time.
	marketing := Relation{
		AudienceID: "bob", Category: "marketing", Channel: "telegram",
		Type: RelationEnrollment, Source: "app:seed", Policy: Policy{AllowUnsubscribe: true},
	}
	relations.store(marketing)
	mustFeed := Relation{
		AudienceID: "bob", Category: "system", Channel: "rss",
		Type: RelationEnrollment, Source: "app:seed", Policy: Policy{MustDeliver: true},
	}
	relations.store(mustFeed)
	f := NewFilter(relations, surfaces)

	if f.Allow("bob", "marketing", "telegram") {
		t.Error("Allow() = true for a seeded marketing×instant relation, want the send-time matrix refusal")
	}
	if f.Allow("bob", "system", "rss") {
		t.Error("Allow() = true for a seeded must-deliver×rss relation, want the send-time matrix refusal")
	}

	// The lawful twin of the seeded marketing relation passes.
	lawful := marketing
	lawful.Channel = "webhook"
	relations.store(lawful)
	if !f.Allow("bob", "marketing", "webhook") {
		t.Error("Allow() = false for a seeded marketing×app relation, want the matrix to pass it")
	}
}
