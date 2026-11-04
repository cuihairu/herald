// Package apps holds the 集成者接入面 (关系详设 §13.1): one namespace
// per integrating application, token auth with 分级权限. App state is
// seeded from config and mutated through the app config API in later
// increments; the registry itself only answers authentication and
// namespace questions.
package apps

import (
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"net"
	"regexp"
	"sort"
	"sync"

	"github.com/cuihairu/herald/core/audience"
	"github.com/cuihairu/herald/core/template"
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

// ErrUnknownApp is returned when an action names a namespace that is
// not registered.
var ErrUnknownApp = errors.New("unknown app")

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
	Name       string
	tokens     []Token
	categories map[string]Category
	policies   AppPolicies
	templates  *template.Manager
	// callback is the §13.5 webhook callback face configuration; nil
	// means the app receives no callbacks.
	callback *Callback
}

// ScopesFor resolves a bearer secret to the permission set it holds in
// this app. The first matching token wins; seeds refuse duplicate
// secrets, so there is exactly one.
func (a *App) ScopesFor(secret string) (Scopes, bool) {
	for _, t := range a.tokens {
		if constantTimeEqual(t.Secret, secret) {
			return t.Scopes, true
		}
	}
	return nil, false
}

// constantTimeEqual compares two app bearer secrets in constant time: both
// sides are hashed to a fixed 32 bytes before the compare, so a length or
// prefix difference cannot be timed apart.
func constantTimeEqual(a, b string) bool {
	ha := sha256.Sum256([]byte(a))
	hb := sha256.Sum256([]byte(b))
	return subtle.ConstantTimeCompare(ha[:], hb[:]) == 1
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
	mu             sync.RWMutex
	apps           map[string]*App
	callbackPolicy CallbackPolicy
	// LookupIP is the callback hostname resolver seam. Defaults to the
	// system resolver; tests may substitute a deterministic answer. It is
	// copied under r.mu before use so concurrent deliveries are safe.
	LookupIP func(string) ([]net.IP, error)
	// callbackHooks are invoked after a callback face is attached,
	// replaced or cleared. Delivery-side caches keyed by callback URL
	// (e.g. pinned transports) hook here to drop entries the registry
	// no longer references, instead of holding them forever. Multiple
	// subscribers (several providers or components) may register, so the
	// slot is a set rather than a single overwrite. Hooks run outside
	// r.mu so handlers may take other locks.
	callbackHooks map[int]func(url string, present bool)
	nextHookID    int
}

// NewRegistry builds the registry from config seeds. Validation refuses
// the whole registry on any bad seed — a half-loaded integration face
// is worse than a refused start.
func NewRegistry(seeds []SeedApp) (*Registry, error) {
	r := &Registry{apps: make(map[string]*App), LookupIP: net.LookupIP}
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

// AddCallbackChangeHook registers a callback-face change notifier and
// returns a handle that unregisters it. Unlike a single-slot setter, it
// never overwrites another subscriber's hook, so a second Provider on
// the same registry does not silently disable the first. The hook
// receives the URL and whether a callback is now present (false on
// clear), and is called outside r.mu so it may safely call back into
// the registry.
func (r *Registry) AddCallbackChangeHook(hook func(url string, present bool)) (unregister func()) {
	if hook == nil {
		// A nil hook would panic on the first notify instead of failing
		// here. The old single-slot setter treated nil as "clear the
		// hook", so a caller migrating that habit must be told promptly
		// rather than fail late at an arbitrary Set/Clear callback call.
		panic("apps: AddCallbackChangeHook called with nil hook")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.callbackHooks == nil {
		r.callbackHooks = make(map[int]func(url string, present bool))
	}
	r.nextHookID++
	id := r.nextHookID
	r.callbackHooks[id] = hook
	return func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		delete(r.callbackHooks, id)
	}
}

func (r *Registry) notifyCallbackChange(url string, present bool) {
	r.mu.RLock()
	hooks := make([]func(url string, present bool), 0, len(r.callbackHooks))
	for _, h := range r.callbackHooks {
		hooks = append(hooks, h)
	}
	r.mu.RUnlock()
	for _, h := range hooks {
		h(url, present)
	}
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

// Category is one app-scoped message category (§13.2 品类注册): the
// dispatch face's taxonomy key plus its default urgency — the §6.2
// 品类→紧急度 mapping's source of truth inside the namespace. Two apps
// registering the same name stay strangers: isolation is the point of
// the namespace.
type Category struct {
	Name           string `json:"name"`
	DefaultUrgency string `json:"default_urgency"`
}

// ErrCategoryConflict is returned when a category re-registers with a
// different default urgency; an identical re-registration is idempotent.
var ErrCategoryConflict = errors.New("category already registered with a different default urgency")

// RegisterCategory validates and stores one category. The name shares
// the app-name charset (it rides in URLs and table keys), the urgency
// must parse in the §6 vocabulary; both refuse, never clamp.
func (r *Registry) RegisterCategory(app, name, defaultUrgency string) error {
	if !appPattern.MatchString(name) {
		return errors.New("invalid category name: " + name)
	}
	urgency, err := audience.ParseUrgency(defaultUrgency)
	if err != nil {
		return fmt.Errorf("category %s: %w", name, err)
	}
	canonical := categoryUrgencyName(urgency)

	r.mu.Lock()
	defer r.mu.Unlock()
	a, ok := r.apps[app]
	if !ok {
		return ErrUnknownApp
	}
	if a.categories == nil {
		a.categories = make(map[string]Category)
	}
	if prev, ok := a.categories[name]; ok {
		if prev.DefaultUrgency != canonical {
			return ErrCategoryConflict
		}
		return nil
	}
	a.categories[name] = Category{Name: name, DefaultUrgency: canonical}
	return nil
}

// Categories lists the namespace's categories in stable name order.
// Unknown apps answer false.
func (r *Registry) Categories(app string) ([]Category, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	a, ok := r.apps[app]
	if !ok {
		return nil, false
	}
	out := make([]Category, 0, len(a.categories))
	for _, c := range a.categories {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, true
}

// CategoryUrgency resolves a namespace category's default urgency in
// canonical English vocabulary; unknown categories answer false (the
// dispatch face decides its own fallback).
func (r *Registry) CategoryUrgency(app, name string) (string, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	a, ok := r.apps[app]
	if !ok {
		return "", false
	}
	c, ok := a.categories[name]
	return c.DefaultUrgency, ok
}

func categoryUrgencyName(u audience.Urgency) string {
	switch u {
	case audience.UrgencyRoutine:
		return "routine"
	case audience.UrgencyNormal:
		return "normal"
	case audience.UrgencyUrgent:
		return "urgent"
	default:
		return "critical"
	}
}
