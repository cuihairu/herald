package audience

import (
	"errors"
	"fmt"
	"sync"

	"github.com/cuihairu/herald/core/audit"
)

// RelationType marks how an audience came to receive a category: actively
// opted in (subscription) or passively included (enrollment). The two are
// the same row shape in the registry — never the same semantics: they
// carry different unsubscribe rights (§4 of docs/design-audience-relations)
// and are audited separately by type.
type RelationType string

const (
	// RelationSubscription: the audience chose this itself (品类×渠道×频率,
	// fully autonomous, always unsubscribable). The audience's role in the
	// relation is the 订阅者 (subscriber).
	RelationSubscription RelationType = "subscription"
	// RelationEnrollment: an admin/operator/system put the audience in
	// (group broadcast, ops outreach, system notices). The audience's role
	// is the 接收者 (recipient role); the bottom lines of §4 bound what
	// enrollment may do.
	RelationEnrollment RelationType = "enrollment"
)

// Known relation sources — the entry adapter a relation change came
// through (§8). App sources are free-form "app:<name>" strings.
const (
	SourceBot              = "bot"
	SourceWeChatMP         = "wechat_mp"
	SourcePreferenceCenter = "preference_center"
	SourceAdmin            = "admin"
)

// Policy carries the relation-type-scoped rights. The bits are validated
// against the relation type on write: subscriptions are always
// unsubscribable and never must-deliver (the contract forces it);
// enrollments must be exactly one of must-deliver (marked, not
// unsubscribable) or unsubscribable (marketing/ops) — a relation the
// audience can neither leave nor relies on being delivered has no lawful
// shape.
type Policy struct {
	// AllowUnsubscribe: the audience may terminate this relation itself.
	AllowUnsubscribe bool
	// MustDeliver marks a system must-deliver relation: shown to the
	// audience as "系统通知，不可退订" and excluded from any unsubscribe.
	MustDeliver bool
}

// Relation is one audience×category×channel delivery entitlement: the
// registry's answer to "may this audience receive this category on this
// channel, on whose word, and with which rights". Category and channel use
// the same vocabulary as the rest of the design (告警/账单/…; telegram/
// email/rss/…).
type Relation struct {
	AudienceID string
	Category   string
	Channel    string
	Type       RelationType
	Source     string
	Policy     Policy
}

// ErrRelationNotFound reports a terminate/lookup on a relation the
// registry does not hold.
var ErrRelationNotFound = errors.New("audience: relation not found")

// relationKey identifies one relation slot: one audience × category ×
// channel holds exactly one relation; a later write on the same key
// replaces it (latest word wins — the audit trail keeps the history).
type relationKey struct {
	audienceID string
	category   string
	channel    string
}

func (r Relation) key() relationKey {
	return relationKey{r.AudienceID, r.Category, r.Channel}
}

// Registry is the authoritative store of audience relations (the 受众注册
// 表). In-memory for now; the shape is the API surface, not the storage —
// a redis-backed store can slot in behind the same methods.
type Registry struct {
	mu        sync.RWMutex
	relations map[relationKey]Relation
	recorder  audit.Recorder
	// unsubscribeHook observes every successful TerminateFor (the
	// §13.5 退订回流: heraldd wires the app callback emitter here).
	// Fired outside the registry lock; the hook must not re-enter.
	unsubscribeHook func(Relation, string)
}

// SetUnsubscribeHook wires the callback face's unsubscribe backflow.
// Nil clears it.
func (g *Registry) SetUnsubscribeHook(hook func(Relation, string)) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.unsubscribeHook = hook
}

// SetRecorder wires the audit trail: every accepted Subscribe/Enroll/
// Terminate lands one event there. Nil clears it.
func (g *Registry) SetRecorder(r audit.Recorder) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.recorder = r
}

// NewRegistry creates an empty relation registry.
func NewRegistry() *Registry {
	return &Registry{relations: make(map[relationKey]Relation)}
}

// Subscribe records an active subscription. The subscription contract is
// enforced, not trusted: the relation is always unsubscribable and never
// must-deliver, whatever the caller passed. Calling it with an enrollment
// relation is a caller bug — Enroll is the explicit counterpart. The §5
// channel×relation matrix needs no check here: the subscription row
// allows every channel family.
func (g *Registry) Subscribe(rel Relation) error {
	if rel.Type != RelationSubscription {
		return fmt.Errorf("audience: Subscribe requires a %q relation, got %q (use Enroll for passive assignment)", RelationSubscription, rel.Type)
	}
	rel.Policy = Policy{AllowUnsubscribe: true, MustDeliver: false}
	if err := validateRelation(rel); err != nil {
		return err
	}
	g.store(rel)
	g.emit(audit.RelationSubscribe, rel, "")
	return nil
}

// Enroll records a passive assignment. The §4 bottom lines are enforced
// here: a must-deliver relation may not claim unsubscribability, and any
// enrollment that is not must-deliver must be unsubscribable — there is no
// lawful enrollment the audience can neither leave nor counts on. The §5
// channel×relation matrix is enforced at the same door: a marketing
// enrollment may only land on a low-disturbance family (email/app), a
// must-deliver enrollment on anything but RSS (subscriptions allow every
// family, so Subscribe has nothing to check here).
func (g *Registry) Enroll(rel Relation) error {
	if rel.Type != RelationEnrollment {
		return fmt.Errorf("audience: Enroll requires an %q relation, got %q (use Subscribe for active opt-in)", RelationEnrollment, rel.Type)
	}
	if rel.Policy.MustDeliver && rel.Policy.AllowUnsubscribe {
		return fmt.Errorf("audience: relation %s/%s/%s: a must-deliver enrollment cannot be unsubscribable", rel.AudienceID, rel.Category, rel.Channel)
	}
	if !rel.Policy.MustDeliver && !rel.Policy.AllowUnsubscribe {
		return fmt.Errorf("audience: relation %s/%s/%s: an enrollment that is not must-deliver must be unsubscribable", rel.AudienceID, rel.Category, rel.Channel)
	}
	if err := validateRelation(rel); err != nil {
		return err
	}
	class := ClassifyChannel(rel.Channel)
	if !MatrixAllows(RelationEnrollment, rel.Policy, class) {
		return fmt.Errorf("audience: relation %s/%s/%s: channel class %q is not permitted for this enrollment by the channel×relation matrix", rel.AudienceID, rel.Category, rel.Channel, class)
	}
	g.store(rel)
	g.emit(audit.RelationEnroll, rel, "")
	return nil
}

// validateRelation checks the fields every relation carries, with the
// same bounds as the other audience tables.
func validateRelation(rel Relation) error {
	if !idPattern.MatchString(rel.AudienceID) {
		return fmt.Errorf("audience: invalid audience id %q (want 1-64 chars of letters, digits, dot, dash, underscore)", rel.AudienceID)
	}
	for label, v := range map[string]string{"category": rel.Category, "channel": rel.Channel, "source": rel.Source} {
		if len(v) == 0 || len(v) > maxNameChars {
			return fmt.Errorf("audience: relation %s: %s must be 1-%d chars", rel.AudienceID, label, maxNameChars)
		}
	}
	return nil
}

// store writes the relation, replacing any previous relation on the same
// audience×category×channel slot. Callers validate first.
func (g *Registry) store(rel Relation) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.relations[rel.key()] = rel
}

// Terminate removes one relation at the audience's word. Only relations
// whose policy allows it can go — a must-deliver enrollment refuses
// (the bottom line), and an unknown relation is ErrRelationNotFound. The
// removed relation is returned so the caller can audit what ended.
func (g *Registry) Terminate(audienceID, category, channel string) (Relation, error) {
	return g.TerminateFor(audienceID, category, channel, "")
}

// TerminateFor is Terminate with the acting entry adapter recorded: the
// event keeps the relation's own entry source (§4 snapshot contract) and
// names the actor in Detail, so the trail answers both「这条订阅从哪来」
// and「这次退订是谁操作」(§8 审计记入口).
func (g *Registry) TerminateFor(audienceID, category, channel, actor string) (Relation, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	key := relationKey{audienceID, category, channel}
	rel, ok := g.relations[key]
	if !ok {
		return Relation{}, ErrRelationNotFound
	}
	if !rel.Policy.AllowUnsubscribe {
		return Relation{}, fmt.Errorf("audience: relation %s/%s/%s is must-deliver and cannot be unsubscribed", audienceID, category, channel)
	}
	delete(g.relations, key)
	if g.recorder != nil {
		detail := ""
		if actor != "" {
			detail = "unsubscribe via " + actor
		}
		g.recorder.Record(audit.Event{
			Kind: audit.RelationTerminate, AudienceID: rel.AudienceID, Category: rel.Category,
			Channel: rel.Channel, RelationType: string(rel.Type), Source: rel.Source, Detail: detail,
		})
	}
	hook := g.unsubscribeHook
	g.mu.Unlock()
	if hook != nil {
		hook(rel, actor)
	}
	g.mu.Lock()
	return rel, nil
}

// emit reports one accepted write to the audit trail, if one is wired.
func (g *Registry) emit(kind audit.EventKind, rel Relation, detail string) {
	g.mu.RLock()
	r := g.recorder
	g.mu.RUnlock()
	if r == nil {
		return
	}
	r.Record(audit.Event{
		Kind: kind, AudienceID: rel.AudienceID, Category: rel.Category,
		Channel: rel.Channel, RelationType: string(rel.Type), Source: rel.Source, Detail: detail,
	})
}

// Lookup returns the relation on one audience×category×channel slot.
func (g *Registry) Lookup(audienceID, category, channel string) (Relation, bool) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	rel, ok := g.relations[relationKey{audienceID, category, channel}]
	return rel, ok
}

// Relations lists every relation held for one audience, subscription and
// enrollment together; callers that need the kinds apart filter with
// RelationsByType.
func (g *Registry) Relations(audienceID string) []Relation {
	return g.filter(func(rel Relation) bool { return rel.AudienceID == audienceID })
}

// RelationsByType lists one audience's relations of exactly one kind —
// the by-type view audits and policy checks go through (the two kinds are
// never mixed in reporting).
func (g *Registry) RelationsByType(audienceID string, t RelationType) []Relation {
	return g.filter(func(rel Relation) bool {
		return rel.AudienceID == audienceID && rel.Type == t
	})
}

func (g *Registry) filter(keep func(Relation) bool) []Relation {
	g.mu.RLock()
	defer g.mu.RUnlock()

	out := make([]Relation, 0, len(g.relations))
	for _, rel := range g.relations {
		if keep(rel) {
			out = append(out, rel)
		}
	}
	return out
}
