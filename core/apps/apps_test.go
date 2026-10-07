package apps

import (
	"errors"
	"strings"
	"testing"

	"github.com/cuihairu/herald/core/audience"
)

func TestParseScope(t *testing.T) {
	for _, in := range []string{"config", "trigger", "query"} {
		if _, err := ParseScope(in); err != nil {
			t.Errorf("ParseScope(%q) = %v, want nil", in, err)
		}
	}
	if _, err := ParseScope("admin"); !errors.Is(err, ErrInvalidScope) {
		t.Errorf("ParseScope(admin) = %v, want ErrInvalidScope", err)
	}
}

func TestScopes(t *testing.T) {
	s, err := NewScopes([]string{"query", "config", "trigger"})
	if err != nil {
		t.Fatal(err)
	}
	for _, scope := range []Scope{ScopeConfig, ScopeTrigger, ScopeQuery} {
		if !s.Allows(scope) {
			t.Errorf("Allows(%v) = false, want true", scope)
		}
	}
	// Contract order for stable API output.
	if got := s.Strings(); len(got) != 3 || got[0] != "config" || got[1] != "trigger" || got[2] != "query" {
		t.Errorf("Strings() = %v, want [config trigger query]", got)
	}
	empty, err := NewScopes(nil)
	if err != nil {
		t.Fatal(err)
	}
	if empty.Allows(ScopeQuery) {
		t.Error("empty set grants query")
	}
	if got := empty.Strings(); len(got) != 0 {
		t.Errorf("empty Strings() = %v, want empty", got)
	}
	if _, err := NewScopes([]string{"root"}); !errors.Is(err, ErrInvalidScope) {
		t.Errorf("NewScopes(root) = %v, want ErrInvalidScope", err)
	}
}

func TestNewRegistry(t *testing.T) {
	r, err := NewRegistry([]SeedApp{
		{Name: "ferry", Tokens: []SeedToken{
			{Secret: "ferry-config", Scopes: []string{"config", "trigger", "query"}},
			{Secret: "ferry-query", Scopes: []string{"query"}},
		}},
		{Name: "sinomed", Tokens: []SeedToken{
			{Secret: "sino-trigger", Scopes: []string{"trigger"}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !r.Exists("ferry") || !r.Exists("sinomed") || r.Exists("ghost") {
		t.Fatal("Exists disagrees with the seeds")
	}
	scopes, ok := r.Authenticate("ferry", "ferry-config")
	if !ok || !scopes.Allows(ScopeTrigger) {
		t.Fatalf("Authenticate(ferry full token) = %v/%v, want full scopes", scopes, ok)
	}
	// A scoped token holds only its own classes.
	scopes, ok = r.Authenticate("ferry", "ferry-query")
	if !ok || scopes.Allows(ScopeTrigger) || !scopes.Allows(ScopeQuery) {
		t.Errorf("Authenticate(ferry query token) = %v/%v, want query only", scopes, ok)
	}
	// Wrong secret and unknown namespace answer the same, so the caller
	// can return one uniform 401.
	if _, ok := r.Authenticate("ferry", "wrong"); ok {
		t.Error("wrong secret authenticated")
	}
	if _, ok := r.Authenticate("ghost", "ferry-config"); ok {
		t.Error("unknown app authenticated (secret from another app)")
	}
}

func TestNewRegistryRefuses(t *testing.T) {
	cases := []struct {
		name  string
		seeds []SeedApp
		want  string
	}{
		{"empty name", []SeedApp{{Name: "", Tokens: []SeedToken{{Secret: "s", Scopes: []string{"query"}}}}}, "invalid app name"},
		{"bad charset", []SeedApp{{Name: "bad name!", Tokens: []SeedToken{{Secret: "s", Scopes: []string{"query"}}}}}, "invalid app name"},
		{"over 64 chars", []SeedApp{{Name: string(make([]byte, 65)), Tokens: []SeedToken{{Secret: "s", Scopes: []string{"query"}}}}}, "invalid app name"},
		{"duplicate app", []SeedApp{
			{Name: "ferry", Tokens: []SeedToken{{Secret: "a", Scopes: []string{"query"}}}},
			{Name: "ferry", Tokens: []SeedToken{{Secret: "b", Scopes: []string{"query"}}}},
		}, "duplicate app"},
		{"no tokens", []SeedApp{{Name: "ferry"}}, "without tokens"},
		{"empty secret", []SeedApp{{Name: "ferry", Tokens: []SeedToken{{Secret: "", Scopes: []string{"query"}}}}}, "empty token secret"},
		{"secret reused within app", []SeedApp{{Name: "ferry", Tokens: []SeedToken{
			{Secret: "twin", Scopes: []string{"query"}},
			{Secret: "twin", Scopes: []string{"config"}},
		}}}, "reused"},
		{"secret reused across apps", []SeedApp{
			{Name: "ferry", Tokens: []SeedToken{{Secret: "twin", Scopes: []string{"query"}}}},
			{Name: "sinomed", Tokens: []SeedToken{{Secret: "twin", Scopes: []string{"query"}}}},
		}, "reused"},
		{"unparseable scope", []SeedApp{{Name: "ferry", Tokens: []SeedToken{{Secret: "s", Scopes: []string{"sudo"}}}}}, "invalid app scope"},
	}
	for _, tc := range cases {
		if _, err := NewRegistry(tc.seeds); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want refusal containing %q", tc.name, err, tc.want)
		}
	}
}

func TestEmptyRegistry(t *testing.T) {
	r, err := NewRegistry(nil)
	if err != nil {
		t.Fatal(err)
	}
	if r.Exists("ferry") {
		t.Error("empty registry holds an app")
	}
	if _, ok := r.Authenticate("ferry", "x"); ok {
		t.Error("empty registry authenticated")
	}
}

func TestRegisterCategory(t *testing.T) {
	r, err := NewRegistry([]SeedApp{{Name: "ferry", Tokens: []SeedToken{
		{Secret: "s", Scopes: []string{"config"}},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.RegisterCategory("ferry", "alerts", "urgent"); err != nil {
		t.Fatalf("RegisterCategory = %v, want nil", err)
	}
	// The §6 vocabulary's Chinese aliases store in canonical English.
	if err := r.RegisterCategory("ferry", "marketing", "例行"); err != nil {
		t.Fatalf("RegisterCategory(例行) = %v, want nil", err)
	}
	// Identical re-registration is idempotent.
	if err := r.RegisterCategory("ferry", "alerts", "urgent"); err != nil {
		t.Fatalf("idempotent re-register = %v, want nil", err)
	}
	// A conflicting default refuses.
	if err := r.RegisterCategory("ferry", "alerts", "critical"); !errors.Is(err, ErrCategoryConflict) {
		t.Errorf("conflicting re-register = %v, want ErrCategoryConflict", err)
	}
	if err := r.RegisterCategory("ghost", "alerts", "urgent"); !errors.Is(err, ErrUnknownApp) {
		t.Errorf("unknown app = %v, want ErrUnknownApp", err)
	}
	if err := r.RegisterCategory("ferry", "bad name!", "urgent"); err == nil || !strings.Contains(err.Error(), "invalid category name") {
		t.Errorf("bad name = %v, want name refusal", err)
	}
	err = r.RegisterCategory("ferry", "alerts", "hourly")
	if err == nil || !errors.Is(err, audience.ErrInvalidUrgency) || !strings.Contains(err.Error(), "alerts") {
		t.Errorf("bad urgency = %v, want ErrInvalidUrgency naming the category", err)
	}
	// critical round-trips through the canonical vocabulary too, and a
	// normal category keeps the whole §6 vocabulary exercised here.
	if err := r.RegisterCategory("ferry", "system", "关键"); err != nil {
		t.Fatalf("RegisterCategory(关键) = %v, want nil", err)
	}
	if err := r.RegisterCategory("ferry", "notices", "一般"); err != nil {
		t.Fatalf("RegisterCategory(一般) = %v, want nil", err)
	}
	if u, _ := r.CategoryUrgency("ferry", "notices"); u != "normal" {
		t.Errorf("notices urgency = %q, want canonical normal", u)
	}
	if u, _ := r.CategoryUrgency("ferry", "system"); u != "critical" {
		t.Errorf("system urgency = %q, want canonical critical", u)
	}
	if _, ok := r.CategoryUrgency("ghost", "alerts"); ok {
		t.Error("unknown app resolved a category urgency")
	}

	cats, ok := r.Categories("ferry")
	if !ok || len(cats) != 4 || cats[0].Name != "alerts" || cats[1].Name != "marketing" || cats[2].Name != "notices" || cats[3].Name != "system" {
		t.Fatalf("Categories = %v/%v, want four categories sorted", cats, ok)
	}
	if cats[1].DefaultUrgency != "routine" {
		t.Errorf("marketing urgency = %q, want canonical routine", cats[1].DefaultUrgency)
	}
	if u, ok := r.CategoryUrgency("ferry", "alerts"); !ok || u != "urgent" {
		t.Errorf("CategoryUrgency(alerts) = %q/%v, want urgent", u, ok)
	}
	if _, ok := r.CategoryUrgency("ferry", "ghost"); ok {
		t.Error("unknown category resolved")
	}
	if _, ok := r.Categories("ghost"); ok {
		t.Error("unknown app listed")
	}
}
