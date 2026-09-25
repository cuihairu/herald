package groups

import (
	"strings"
	"testing"
)

func validGroup() Group {
	return Group{
		ID:      "ops-oncall",
		Members: []Member{{Channel: "feishu-oncall"}, {Channel: "sms-duty", Recipients: []string{"13800000000"}}},
	}
}

func TestRefHelpers(t *testing.T) {
	if Ref("ops") != "group:ops" {
		t.Errorf("Ref mismatch: %q", Ref("ops"))
	}
	if !IsRef("group:ops") || IsRef("feishu-oncall") || IsRef("group:") == false {
		// "group:" alone is a (malformed) reference: IsRef is a prefix
		// check, the name's validity is the resolver's business.
		t.Errorf("IsRef misbehaves")
	}
}

func TestGroupValidate(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		if err := validGroup().Validate(); err != nil {
			t.Fatalf("expected acceptance, got %v", err)
		}
	})

	t.Run("bad ids", func(t *testing.T) {
		for _, id := range []string{"", " lead", "has space", "has/slash", strings.Repeat("x", 65)} {
			g := validGroup()
			g.ID = id
			if err := g.Validate(); err == nil {
				t.Errorf("id %q: expected rejection", id)
			}
		}
		for _, id := range []string{"a", "A9._-", strings.Repeat("x", 64)} {
			g := validGroup()
			g.ID = id
			if err := g.Validate(); err != nil {
				t.Errorf("id %q: expected acceptance, got %v", id, err)
			}
		}
	})

	t.Run("description bound", func(t *testing.T) {
		g := validGroup()
		g.Description = strings.Repeat("d", maxDescriptionChars+1)
		if err := g.Validate(); err == nil || !strings.Contains(err.Error(), "description") {
			t.Fatalf("expected description bound, got %v", err)
		}
	})

	t.Run("members required", func(t *testing.T) {
		g := validGroup()
		g.Members = nil
		if err := g.Validate(); err == nil || !strings.Contains(err.Error(), "at least one") {
			t.Fatalf("expected empty-members rejection, got %v", err)
		}
	})

	t.Run("members bound", func(t *testing.T) {
		g := validGroup()
		g.Members = make([]Member, maxMembers+1)
		for i := range g.Members {
			g.Members[i] = Member{Channel: "ch-" + strings.Repeat("x", 2)}
		}
		if err := g.Validate(); err == nil || !strings.Contains(err.Error(), "max") {
			t.Fatalf("expected member-count bound, got %v", err)
		}
	})

	t.Run("member channel hygiene", func(t *testing.T) {
		g := validGroup()
		g.Members = append(g.Members, Member{Channel: "  "})
		if err := g.Validate(); err == nil || !strings.Contains(err.Error(), "member 2 channel") {
			t.Fatalf("expected blank-channel rejection, got %v", err)
		}

		g = validGroup()
		g.Members = append(g.Members, Member{Channel: strings.Repeat("c", maxNameChars+1)})
		if err := g.Validate(); err == nil || !strings.Contains(err.Error(), "member 2 channel") {
			t.Fatalf("expected long-channel rejection, got %v", err)
		}
	})

	t.Run("nested group rejected", func(t *testing.T) {
		g := validGroup()
		g.Members = append(g.Members, Member{Channel: Ref("inner")})
		if err := g.Validate(); err == nil || !strings.Contains(err.Error(), "do not nest") {
			t.Fatalf("expected nesting rejection, got %v", err)
		}
	})

	t.Run("duplicate member channel rejected", func(t *testing.T) {
		g := validGroup()
		g.Members = append(g.Members, Member{Channel: "sms-duty"})
		if err := g.Validate(); err == nil || !strings.Contains(err.Error(), "duplicate member channel") {
			t.Fatalf("expected duplicate rejection, got %v", err)
		}
	})

	t.Run("recipients hygiene", func(t *testing.T) {
		g := validGroup()
		g.Members = []Member{{Channel: "sms", Recipients: make([]string, maxRecipients+1)}}
		if err := g.Validate(); err == nil || !strings.Contains(err.Error(), "recipients, max") {
			t.Fatalf("expected recipients bound, got %v", err)
		}

		g.Members = []Member{{Channel: "sms", Recipients: []string{"138", "  "}}}
		if err := g.Validate(); err == nil || !strings.Contains(err.Error(), "recipient 1 is empty") {
			t.Fatalf("expected blank-recipient rejection, got %v", err)
		}
	})
}

func TestGroupNormalize(t *testing.T) {
	g := Group{ID: "  ops  "}
	g.Normalize()
	if g.ID != "ops" {
		t.Errorf("expected trimmed id, got %q", g.ID)
	}
}
