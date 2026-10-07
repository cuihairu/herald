// Package apps holds the 集成者接入面 (关系详设 §13.1): one namespace
// per integrating application, token auth with 分级权限. App state is
// seeded from config and mutated through the app config API in later
// increments; the registry itself only answers authentication and
// namespace questions.
package apps

import (
	"errors"
	"regexp"
	"sort"
	"sync"
)

// Scope is one app-token permission class (§13.1: 配置权/触发权/查询权).
// The vocabulary is the API contract — no aliases.
type Scope string

const (
	ScopeConfig  Scope = "config"
	ScopeTrigger Scope = "trigger"
	ScopeQuery   Scope = "query"
)

// ErrInvalidScope is returned for any token scope outside the three
// permission classes. Refuse, never clamp.
var ErrInvalidScope = errors.New("invalid app scope")

// ParseScope parses a scope string; unparseable values refuse (the
// caller turns that into a config or request rejection).
func ParseScope(s string) (Scope, error) {
	switch s {
	case "config":
		return ScopeConfig, nil
	case "trigger":
		return ScopeTrigger, nil
	case "query":
		return ScopeQuery, nil
	default:
		return "", ErrInvalidScope
	}
}

var scopeRank = map[Scope]int{
	ScopeConfig:  0,
	ScopeTrigger: 1,
	ScopeQuery:   2,
}

// Scopes is one token's permission set.
type Scopes map[Scope]bool

// NewScopes builds the set, refusing unknown scope strings.
func NewScopes(raw []string) (Scopes, error) {
	s := make(Scopes, len(raw))
	for _, r := range raw {
		scope, err := ParseScope(r)
		if err != nil {
			return nil, err
		}
		s[scope] = true
	}
	return s, nil
}

// Allows reports whether the set grants one scope.
func (s Scopes) Allows(scope Scope) bool {
	return s[scope]
}

// Strings renders the set in contract order (config, trigger, query) so
// API responses and logs are stable.
func (s Scopes) Strings() []string {
	out := make([]Scope, 0, len(s))
	for scope := range s {
		out = append(out, scope)
	}
	sort.Slice(out, func(i, j int) bool { return scopeRank[out[i]] < scopeRank[out[j]] })
	res := make([]string, len(out))
	for i, scope := range out {
		res[i] = string(scope)
	}
	return res
}

// Token is one app credential: the bearer secret as configured plus the
// permission set it carries. Secrets stay plaintext in config, the same
// precedent as auth.api_keys.
type Token struct {
	Secret string
	Scopes Scopes
}

// App is one integration namespace: its name bounds everything the app
// registers (categories, templates, policies — later increments), and
// its tokens are the only keys that act inside it.
type App struct {
	Name   string
	tokens []Token
}

// ScopesFor resolves a bearer secret to the permission set it holds in
// this app. The first matching token wins; seeds refuse duplicate
// secrets, so there is exactly one.
func (a *App) ScopesFor(secret string) (Scopes, bool) {
	for _, t := range a.tokens {
		if t.Secret == secret {
			return t.Scopes, true
		}
	}
	return nil, false
}

// SeedApp and SeedToken are the bootstrap input: an app with at least
// one token, all validated at construction. There is no runtime way to
// mint a first token yet, so a token-less app seed is refused.
type SeedApp struct {
	Name   string
	Tokens []SeedToken
}

type SeedToken struct {
	Secret string
	Scopes []string
}

// appPattern bounds app names to the same charset as feed URL slugs.
var appPattern = regexp.MustCompile(`^[a-zA-Z0-9._-]{1,64}$`)

// Registry holds every integration namespace. Read-mostly: seeds load
// once at startup, later mutation arrives through the config API.
type Registry struct {
	mu   sync.RWMutex
	apps map[string]*App
}

// NewRegistry builds the registry from config seeds. Validation refuses
// the whole registry on any bad seed — a half-loaded integration face
// is worse than a refused start.
func NewRegistry(seeds []SeedApp) (*Registry, error) {
	r := &Registry{apps: make(map[string]*App)}
	seen := make(map[string]string) // secret -> app name, across all apps
	for _, seed := range seeds {
		if !appPattern.MatchString(seed.Name) {
			return nil, errors.New("invalid app name: " + seed.Name)
		}
		if _, ok := r.apps[seed.Name]; ok {
			return nil, errors.New("duplicate app: " + seed.Name)
		}
		if len(seed.Tokens) == 0 {
			return nil, errors.New("app without tokens: " + seed.Name)
		}
		app := &App{Name: seed.Name}
		for _, st := range seed.Tokens {
			if st.Secret == "" {
				return nil, errors.New("empty token secret for app " + seed.Name)
			}
			if other, ok := seen[st.Secret]; ok {
				return nil, errors.New("token secret reused across " + other + " and " + seed.Name)
			}
			seen[st.Secret] = seed.Name
			scopes, err := NewScopes(st.Scopes)
			if err != nil {
				return nil, errors.New("app " + seed.Name + ": " + err.Error())
			}
			app.tokens = append(app.tokens, Token{Secret: st.Secret, Scopes: scopes})
		}
		r.apps[seed.Name] = app
	}
	return r, nil
}

// Exists reports whether a namespace is registered.
func (r *Registry) Exists(app string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.apps[app]
	return ok
}

// Authenticate resolves an app+secret pair to its granted scopes.
// Unknown app and wrong secret answer the same — callers return one
// uniform 401 so the endpoint never leaks which namespaces exist.
func (r *Registry) Authenticate(app, secret string) (Scopes, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	a, ok := r.apps[app]
	if !ok {
		return nil, false
	}
	return a.ScopesFor(secret)
}
