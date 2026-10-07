package audience

import (
	"errors"
	"fmt"
)

// SourceAdapter is the §8 订阅入口: it normalizes the platform-side
// actions (公众号关注/取关、bot /start /stop、应用内勾选) into relation
// and surface changes on the two registries. Herald stays the 权威登记处
// — adapters never open a second book; every accepted change lands in
// the audit trail attributed to the entry adapter it came through.
//
// The adapter deliberately knows no platform wire formats: HTTP handlers
// translate TG updates and MP events into these calls, so the action
// semantics are testable without either platform.
type SourceAdapter struct {
	surfaces  *SurfaceRegistry
	relations *Registry
}

// NewSourceAdapter wires the adapter to the two registries it mutates.
// Both must be non-nil — an adapter without its authoritative store has
// nothing to converge on.
func NewSourceAdapter(surfaces *SurfaceRegistry, relations *Registry) *SourceAdapter {
	if surfaces == nil || relations == nil {
		panic("audience: source adapter needs both registries")
	}
	return &SourceAdapter{surfaces: surfaces, relations: relations}
}

// ErrSurfaceConflict reports a follow whose platform handle contradicts
// an active surface (different target on a live slot). The adapter does
// not arbitrate — §8 rule 3 leaves contradictions to 对账, which probes
// the platform and corrects from evidence instead of from an event that
// may race a rebind.
var ErrSurfaceConflict = errors.New("audience: active surface holds a different target")

// FailedDefault reports one default subscription that could not land on
// a follow. Follow still succeeds: the surface is registered either way,
// and a default that the registry refuses (bad category, matrix block)
// is reported, not swallowed.
type FailedDefault struct {
	Category string
	Error    string
}

// FollowResult reports what one follow action did.
type FollowResult struct {
	// Surface is the contact surface after the action.
	Surface ContactSurface
	// BindChanged is true when the surface state moved (first bind,
	// re-activation of an invalid slot); a refresh of an already-active
	// same-target surface is a no-op and reports false.
	BindChanged bool
	// Subscribed lists the default relations that landed.
	Subscribed []Relation
	// Failed lists the defaults the registry refused, with reasons.
	Failed []FailedDefault
}

// Follow handles a platform follow event: register the platform-vouched
// handle as the audience's contact surface and lay down the default
// subscription group (§8 动作归一化: follow = 注册联系面 + 默认订阅组).
// The caller resolves the audience from the platform identity first —
// an event for an identity the registry cannot map to an audience is not
// this method's business (no binding was ever made; there is nothing to
// follow through).
//
// Surface rules mirror the binding flow: an empty or invalid slot
// activates outright (an invalid slot has nothing left to hijack); an
// already-active same-target surface is a refresh no-op; a live slot
// holding a different target refuses with ErrSurfaceConflict — the
// rebind guard of §3.1 applies to platform events too, and the
// contradiction is 对账's to resolve.
//
// Defaults are subscription-type relations (the audience followed of its
// own will): the policy is forced to the unsubscribable shape regardless
// of what the caller passes, and each refusal is reported per category.
func (a *SourceAdapter) Follow(audienceID, channel, target, source string, defaultCategories []string) (FollowResult, error) {
	if err := a.surfaces.checkIDAndChannel(audienceID, channel); err != nil {
		return FollowResult{}, err
	}
	if len(source) == 0 || len(source) > maxNameChars {
		return FollowResult{}, fmt.Errorf("audience: source must be 1-%d chars", maxNameChars)
	}

	changed, err := a.surfaces.Activate(audienceID, channel, target, fmt.Sprintf("follow via %s", source))
	if err != nil {
		return FollowResult{}, err
	}
	res := FollowResult{BindChanged: changed}
	res.Surface, _ = a.surfaces.Surface(audienceID, channel)

	for _, category := range defaultCategories {
		rel := Relation{
			AudienceID: audienceID, Category: category, Channel: channel,
			Type: RelationSubscription, Source: source,
			Policy: Policy{AllowUnsubscribe: true, MustDeliver: false},
		}
		if err := a.relations.Subscribe(rel); err != nil {
			res.Failed = append(res.Failed, FailedDefault{Category: category, Error: err.Error()})
			continue
		}
		res.Subscribed = append(res.Subscribed, rel)
	}
	return res, nil
}

// UnfollowResult reports what one unfollow action stopped.
type UnfollowResult struct {
	// SurfaceInvalidated is true when a live (active or pending) surface
	// was marked invalid by this action.
	SurfaceInvalidated bool
	// Terminated lists the subscription relations ended on this channel.
	Terminated []Relation
}

// Unfollow handles a platform unfollow event — the 取关回流, the one
// action §8 makes absolute: the contact surface goes invalid (nothing
// delivers on a dead handle) and every subscription the audience holds
// on this channel terminates. Enrollment relations are deliberately left
// in the registry: their must-deliver bottom line is not the audience's
// to undo, and deliveries stop anyway through the invalid surface —
// re-following revives the slot without re-enrolling.
//
// A missing surface is an error only for the caller's bookkeeping: the
// handler answers the platform 200 regardless (retrying an unfollow for
// an unknown follower changes nothing).
func (a *SourceAdapter) Unfollow(audienceID, channel, source string) (UnfollowResult, error) {
	if err := a.surfaces.checkIDAndChannel(audienceID, channel); err != nil {
		return UnfollowResult{}, err
	}
	if len(source) == 0 || len(source) > maxNameChars {
		return UnfollowResult{}, fmt.Errorf("audience: source must be 1-%d chars", maxNameChars)
	}

	res := UnfollowResult{}
	if surface, ok := a.surfaces.Surface(audienceID, channel); ok && surface.Status != SurfaceInvalid {
		if err := a.surfaces.InvalidateFor(audienceID, channel, fmt.Sprintf("unfollow via %s", source)); err != nil {
			return UnfollowResult{}, err
		}
		res.SurfaceInvalidated = true
	}
	for _, rel := range a.relations.RelationsByType(audienceID, RelationSubscription) {
		if rel.Channel != channel {
			continue
		}
		ended, err := a.relations.TerminateFor(audienceID, rel.Category, channel, source)
		if err != nil {
			// Refusals here need a must-deliver relation (Subscribe never
			// produces one) or a concurrent delete landing in the snapshot
			// window — guard and keep the sweep going either way.
			continue
		}
		res.Terminated = append(res.Terminated, ended)
	}
	return res, nil
}

// Toggle applies one in-app preference-centre checkbox (§8 check 动作):
// on lays a subscription down, off terminates the one on the slot. The
// acting source records as preference_center (or whatever the deployment
// calls its app) — the registry audit answers「这条订阅从哪来」for these
// relations too.
func (a *SourceAdapter) Toggle(audienceID, category, channel, source string, on bool) (Relation, error) {
	if len(source) == 0 || len(source) > maxNameChars {
		return Relation{}, fmt.Errorf("audience: source must be 1-%d chars", maxNameChars)
	}
	if on {
		rel := Relation{
			AudienceID: audienceID, Category: category, Channel: channel,
			Type: RelationSubscription, Source: source,
		}
		return rel, a.relations.Subscribe(rel)
	}
	return a.relations.TerminateFor(audienceID, category, channel, source)
}
