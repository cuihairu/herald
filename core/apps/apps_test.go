package apps

import (
	"errors"
	"net"
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
		{Name: "demo-app", Tokens: []SeedToken{
			{Secret: "demo-app-config", Scopes: []string{"config", "trigger", "query"}},
			{Secret: "demo-app-query", Scopes: []string{"query"}},
		}},
		{Name: "app-b", Tokens: []SeedToken{
			{Secret: "sino-trigger", Scopes: []string{"trigger"}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !r.Exists("demo-app") || !r.Exists("app-b") || r.Exists("ghost") {
		t.Fatal("Exists disagrees with the seeds")
	}
	scopes, ok := r.Authenticate("demo-app", "demo-app-config")
	if !ok || !scopes.Allows(ScopeTrigger) {
		t.Fatalf("Authenticate(demo-app full token) = %v/%v, want full scopes", scopes, ok)
	}
	// A scoped token holds only its own classes.
	scopes, ok = r.Authenticate("demo-app", "demo-app-query")
	if !ok || scopes.Allows(ScopeTrigger) || !scopes.Allows(ScopeQuery) {
		t.Errorf("Authenticate(demo-app query token) = %v/%v, want query only", scopes, ok)
	}
	// Wrong secret and unknown namespace answer the same, so the caller
	// can return one uniform 401.
	if _, ok := r.Authenticate("demo-app", "wrong"); ok {
		t.Error("wrong secret authenticated")
	}
	if _, ok := r.Authenticate("ghost", "demo-app-config"); ok {
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
			{Name: "demo-app", Tokens: []SeedToken{{Secret: "a", Scopes: []string{"query"}}}},
			{Name: "demo-app", Tokens: []SeedToken{{Secret: "b", Scopes: []string{"query"}}}},
		}, "duplicate app"},
		{"no tokens", []SeedApp{{Name: "demo-app"}}, "without tokens"},
		{"empty secret", []SeedApp{{Name: "demo-app", Tokens: []SeedToken{{Secret: "", Scopes: []string{"query"}}}}}, "empty token secret"},
		{"secret reused within app", []SeedApp{{Name: "demo-app", Tokens: []SeedToken{
			{Secret: "twin", Scopes: []string{"query"}},
			{Secret: "twin", Scopes: []string{"config"}},
		}}}, "reused"},
		{"secret reused across apps", []SeedApp{
			{Name: "demo-app", Tokens: []SeedToken{{Secret: "twin", Scopes: []string{"query"}}}},
			{Name: "app-b", Tokens: []SeedToken{{Secret: "twin", Scopes: []string{"query"}}}},
		}, "reused"},
		{"unparseable scope", []SeedApp{{Name: "demo-app", Tokens: []SeedToken{{Secret: "s", Scopes: []string{"sudo"}}}}}, "invalid app scope"},
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
	if r.Exists("demo-app") {
		t.Error("empty registry holds an app")
	}
	if _, ok := r.Authenticate("demo-app", "x"); ok {
		t.Error("empty registry authenticated")
	}
}

func TestRegisterCategory(t *testing.T) {
	r, err := NewRegistry([]SeedApp{{Name: "demo-app", Tokens: []SeedToken{
		{Secret: "s", Scopes: []string{"config"}},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.RegisterCategory("demo-app", "alerts", "urgent"); err != nil {
		t.Fatalf("RegisterCategory = %v, want nil", err)
	}
	// The §6 vocabulary's Chinese aliases store in canonical English.
	if err := r.RegisterCategory("demo-app", "marketing", "例行"); err != nil {
		t.Fatalf("RegisterCategory(例行) = %v, want nil", err)
	}
	// Identical re-registration is idempotent.
	if err := r.RegisterCategory("demo-app", "alerts", "urgent"); err != nil {
		t.Fatalf("idempotent re-register = %v, want nil", err)
	}
	// A conflicting default refuses.
	if err := r.RegisterCategory("demo-app", "alerts", "critical"); !errors.Is(err, ErrCategoryConflict) {
		t.Errorf("conflicting re-register = %v, want ErrCategoryConflict", err)
	}
	if err := r.RegisterCategory("ghost", "alerts", "urgent"); !errors.Is(err, ErrUnknownApp) {
		t.Errorf("unknown app = %v, want ErrUnknownApp", err)
	}
	if err := r.RegisterCategory("demo-app", "bad name!", "urgent"); err == nil || !strings.Contains(err.Error(), "invalid category name") {
		t.Errorf("bad name = %v, want name refusal", err)
	}
	err = r.RegisterCategory("demo-app", "alerts", "hourly")
	if err == nil || !errors.Is(err, audience.ErrInvalidUrgency) || !strings.Contains(err.Error(), "alerts") {
		t.Errorf("bad urgency = %v, want ErrInvalidUrgency naming the category", err)
	}
	// critical round-trips through the canonical vocabulary too, and a
	// normal category keeps the whole §6 vocabulary exercised here.
	if err := r.RegisterCategory("demo-app", "system", "关键"); err != nil {
		t.Fatalf("RegisterCategory(关键) = %v, want nil", err)
	}
	if err := r.RegisterCategory("demo-app", "notices", "一般"); err != nil {
		t.Fatalf("RegisterCategory(一般) = %v, want nil", err)
	}
	if u, _ := r.CategoryUrgency("demo-app", "notices"); u != "normal" {
		t.Errorf("notices urgency = %q, want canonical normal", u)
	}
	if u, _ := r.CategoryUrgency("demo-app", "system"); u != "critical" {
		t.Errorf("system urgency = %q, want canonical critical", u)
	}
	if _, ok := r.CategoryUrgency("ghost", "alerts"); ok {
		t.Error("unknown app resolved a category urgency")
	}

	cats, ok := r.Categories("demo-app")
	if !ok || len(cats) != 4 || cats[0].Name != "alerts" || cats[1].Name != "marketing" || cats[2].Name != "notices" || cats[3].Name != "system" {
		t.Fatalf("Categories = %v/%v, want four categories sorted", cats, ok)
	}
	if cats[1].DefaultUrgency != "routine" {
		t.Errorf("marketing urgency = %q, want canonical routine", cats[1].DefaultUrgency)
	}
	if u, ok := r.CategoryUrgency("demo-app", "alerts"); !ok || u != "urgent" {
		t.Errorf("CategoryUrgency(alerts) = %q/%v, want urgent", u, ok)
	}
	if _, ok := r.CategoryUrgency("demo-app", "ghost"); ok {
		t.Error("unknown category resolved")
	}
	if _, ok := r.Categories("ghost"); ok {
		t.Error("unknown app listed")
	}
}

// TestAddCallbackChangeHookDoesNotOverwrite: two subscribers on one
// registry must both receive callback-change notifications; a second
// registration must not silently disable the first (the single-slot
// overwrite bug).
func TestAddCallbackChangeHookDoesNotOverwrite(t *testing.T) {
	reg, err := NewRegistry([]SeedApp{{
		Name:   "demo-app",
		Tokens: []SeedToken{{Secret: "tok", Scopes: []string{"config"}}},
	}})
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	reg.SetCallbackPolicy(CallbackPolicy{AllowPrivate: true})
	reg.LookupIP = func(host string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("127.0.0.1")}, nil
	}

	var first, second []string
	unreg1 := reg.AddCallbackChangeHook(func(url string, present bool) {
		first = append(first, url)
	})
	unreg2 := reg.AddCallbackChangeHook(func(url string, present bool) {
		second = append(second, url)
	})
	defer unreg1()
	defer unreg2()

	if err := reg.SetCallback("demo-app", "http://app.example.com/hook", strings.Repeat("s", 16)); err != nil {
		t.Fatalf("set callback: %v", err)
	}
	if len(first) != 0 || len(second) != 0 {
		t.Fatalf("unexpected notify on first set (url already present): %v %v", first, second)
	}
	if err := reg.SetCallback("demo-app", "http://app.example.com/hook2", strings.Repeat("s", 16)); err != nil {
		t.Fatalf("replace callback: %v", err)
	}
	if len(first) != 1 || first[0] != "http://app.example.com/hook" {
		t.Fatalf("first hook notifications = %v, want old URL", first)
	}
	if len(second) != 1 || second[0] != "http://app.example.com/hook" {
		t.Fatalf("second hook notifications = %v, want old URL", second)
	}
}

// TestAddCallbackChangeHookUnregister: an unregistered subscriber stops
// receiving notifications, while a remaining subscriber keeps them.
func TestAddCallbackChangeHookUnregister(t *testing.T) {
	reg, err := NewRegistry([]SeedApp{{
		Name:   "demo-app",
		Tokens: []SeedToken{{Secret: "tok", Scopes: []string{"config"}}},
	}})
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	reg.SetCallbackPolicy(CallbackPolicy{AllowPrivate: true})
	reg.LookupIP = func(host string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("127.0.0.1")}, nil
	}
	var kept, dropped []string
	unreg1 := reg.AddCallbackChangeHook(func(url string, present bool) {
		kept = append(kept, url)
	})
	unreg2 := reg.AddCallbackChangeHook(func(url string, present bool) {
		dropped = append(dropped, url)
	})
	unreg2()
	defer unreg1()

	if err := reg.SetCallback("demo-app", "http://app.example.com/hook", strings.Repeat("s", 16)); err != nil {
		t.Fatalf("set callback: %v", err)
	}
	if err := reg.ClearCallback("demo-app"); err != nil {
		t.Fatalf("clear callback: %v", err)
	}
	if len(kept) != 1 || kept[0] != "http://app.example.com/hook" {
		t.Fatalf("kept hook notifications = %v, want the cleared URL", kept)
	}
	if len(dropped) != 0 {
		t.Fatalf("dropped hook still notified: %v", dropped)
	}
}

// TestAddCallbackChangeHookNilFailsFast: a nil hook must panic at
// registration, not be stored and panic later at the first notify. The old
// single-slot setter treated nil as "clear the hook", so a caller migrating
// that habit must fail promptly rather than at an arbitrary Set/Clear call.
func TestAddCallbackChangeHookNilFailsFast(t *testing.T) {
	reg, err := NewRegistry(nil)
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("AddCallbackChangeHook(nil) did not panic")
		}
	}()
	reg.AddCallbackChangeHook(nil)
}
