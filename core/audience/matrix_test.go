package audience

import "testing"

// TestClassifyChannel: the §5 family each channel name belongs to,
// including the strict default — an unknown name classifies as instant.
func TestClassifyChannel(t *testing.T) {
	cases := []struct {
		name string
		want ChannelClass
	}{
		{"rss", ChannelRSS},
		{"feed", ChannelRSS},
		{"RSS", ChannelRSS}, // classification is case-insensitive
		{"email", ChannelEmail},
		{"smtp", ChannelEmail},
		{"mail", ChannelEmail},
		{"Email", ChannelEmail},
		{"webhook", ChannelApp},
		{"inbox", ChannelApp},
		{"telegram", ChannelInstant},
		{"sms-duty", ChannelInstant},
		{"feishu-oncall", ChannelInstant},
		{"", ChannelInstant},
	}
	for _, tc := range cases {
		if got := ClassifyChannel(tc.name); got != tc.want {
			t.Errorf("ClassifyChannel(%q) = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// TestMatrixAllows: the §5 渠道×关系矩阵 row by row — subscriptions own
// every family, must-deliver enrollments every family but RSS, marketing
// enrollments only the low-disturbance two, and an unknown relation type
// carries no channel rights at all.
func TestMatrixAllows(t *testing.T) {
	mustDeliver := Policy{MustDeliver: true}
	marketing := Policy{AllowUnsubscribe: true}

	cases := []struct {
		name    string
		relType RelationType
		policy  Policy
		class   ChannelClass
		want    bool
	}{
		{"subscription×instant", RelationSubscription, Policy{}, ChannelInstant, true},
		{"subscription×email", RelationSubscription, Policy{}, ChannelEmail, true},
		{"subscription×app", RelationSubscription, Policy{}, ChannelApp, true},
		{"subscription×rss", RelationSubscription, Policy{}, ChannelRSS, true},
		{"must-deliver×instant", RelationEnrollment, mustDeliver, ChannelInstant, true},
		{"must-deliver×email", RelationEnrollment, mustDeliver, ChannelEmail, true},
		{"must-deliver×app", RelationEnrollment, mustDeliver, ChannelApp, true},
		{"must-deliver×rss", RelationEnrollment, mustDeliver, ChannelRSS, false},
		{"marketing×instant", RelationEnrollment, marketing, ChannelInstant, false},
		{"marketing×email", RelationEnrollment, marketing, ChannelEmail, true},
		{"marketing×app", RelationEnrollment, marketing, ChannelApp, true},
		{"marketing×rss", RelationEnrollment, marketing, ChannelRSS, false},
		{"unknown type", RelationType("stranger"), Policy{}, ChannelEmail, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := MatrixAllows(tc.relType, tc.policy, tc.class); got != tc.want {
				t.Errorf("MatrixAllows() = %t, want %t", got, tc.want)
			}
		})
	}
}

// TestEnrollHonorsChannelMatrix: the §5 matrix is enforced at the Enroll
// door — a marketing enrollment never lands on an instant channel or
// RSS, a must-deliver enrollment never lands on RSS, and refused
// combinations are not stored. The lawful rows still write.
func TestEnrollHonorsChannelMatrix(t *testing.T) {
	refused := []struct {
		name    string
		policy  Policy
		channel string
	}{
		{"marketing×instant", Policy{AllowUnsubscribe: true}, "telegram"},
		{"marketing×rss", Policy{AllowUnsubscribe: true}, "rss"},
		{"must-deliver×rss", Policy{MustDeliver: true}, "feed"},
	}
	for _, tc := range refused {
		t.Run(tc.name, func(t *testing.T) {
			g := NewRegistry()
			rel := Relation{
				AudienceID: "alice",
				Category:   "marketing",
				Channel:    tc.channel,
				Type:       RelationEnrollment,
				Source:     SourceAdmin,
				Policy:     tc.policy,
			}
			if err := g.Enroll(rel); err == nil {
				t.Fatalf("Enroll(%s) = nil, want a matrix refusal", tc.name)
			}
			if _, ok := g.Lookup(rel.AudienceID, rel.Category, rel.Channel); ok {
				t.Error("refused enrollment was stored anyway")
			}
		})
	}

	// Field validation runs at the Enroll door before the matrix does.
	g := NewRegistry()
	bad := Relation{
		AudienceID: "alice",
		Category:   "",
		Channel:    "email",
		Type:       RelationEnrollment,
		Source:     SourceAdmin,
		Policy:     Policy{AllowUnsubscribe: true},
	}
	if err := g.Enroll(bad); err == nil {
		t.Fatal("Enroll(missing category) = nil, want a field validation error")
	}

	// Lawful rows still write: marketing on the app family, must-deliver
	// on an instant channel.
	lawful := []struct {
		name    string
		policy  Policy
		channel string
	}{
		{"marketing×app", Policy{AllowUnsubscribe: true}, "webhook"},
		{"must-deliver×instant", Policy{MustDeliver: true}, "sms-duty"},
	}
	for _, tc := range lawful {
		t.Run(tc.name, func(t *testing.T) {
			g := NewRegistry()
			rel := Relation{
				AudienceID: "alice",
				Category:   "ops",
				Channel:    tc.channel,
				Type:       RelationEnrollment,
				Source:     SourceAdmin,
				Policy:     tc.policy,
			}
			if err := g.Enroll(rel); err != nil {
				t.Fatalf("Enroll(%s) error = %v, want the lawful row to write", tc.name, err)
			}
			if _, ok := g.Lookup(rel.AudienceID, rel.Category, rel.Channel); !ok {
				t.Errorf("lawful enrollment %s did not store", tc.name)
			}
		})
	}
}
