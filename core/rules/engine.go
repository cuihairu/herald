package rules

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/ast"
	"github.com/expr-lang/expr/parser"
	"github.com/expr-lang/expr/vm"
)

// Env is the typed evaluation environment exposed to rule expressions.
// Field names follow the expr tags so rules read like the design doc:
// `level == "error" && params.fail_rate > 0.05`. The json tags make the
// environment canonically serializable for ForGroupKey.
type Env struct {
	Type   string         `expr:"type" json:"type"`
	Level  string         `expr:"level" json:"level"`
	Title  string         `expr:"title" json:"title"`
	Body   string         `expr:"body" json:"body"`
	Params map[string]any `expr:"params" json:"params"`
}

// NewEnv builds an evaluation environment from the channel-agnostic view
// of a notification.
func NewEnv(notificationType, level, title, body string, params map[string]any) Env {
	if params == nil {
		params = map[string]any{}
	}
	return Env{
		Type:   notificationType,
		Level:  level,
		Title:  title,
		Body:   body,
		Params: params,
	}
}

const (
	// MaxExpressionLen caps the raw expression size accepted at save time.
	MaxExpressionLen = 2048
	// MaxASTNodes caps compiled expression complexity (expr.MaxNodes).
	MaxASTNodes = 256
	// evalTimeout bounds a single evaluation. With builtins disabled, the
	// range operator rejected and AST nodes capped, expressions are bounded
	// pure comparisons that finish in microseconds; the timeout is a last
	// resort so the caller never waits on a runaway program (the evaluating
	// goroutine itself cannot be interrupted by expr and finishes on its own).
	evalTimeout = 50 * time.Millisecond
)

// safetyGuard rejects AST constructs that expr's compile options cannot:
// the range operator `..` allocates eagerly and would let a short
// expression like `1..99999999 != []` exhaust memory despite node limits.
type safetyGuard struct {
	violation string
}

func (g *safetyGuard) Visit(node *ast.Node) {
	if g.violation != "" {
		return
	}
	if b, ok := (*node).(*ast.BinaryNode); ok && b.Operator == ".." {
		g.violation = "range operator (..) is not allowed"
	}
}

// compileExpr is the only path from expression text to a runnable program;
// every rule expression goes through it at save time (never at eval time).
func compileExpr(code string) (*vm.Program, error) {
	if strings.TrimSpace(code) == "" {
		return nil, fmt.Errorf("rules: expression is empty")
	}
	if len(code) > MaxExpressionLen {
		return nil, fmt.Errorf("rules: expression is %d chars, max is %d", len(code), MaxExpressionLen)
	}
	tree, err := parser.Parse(code)
	if err != nil {
		return nil, fmt.Errorf("rules: %w", err)
	}
	guard := &safetyGuard{}
	ast.Walk(&tree.Node, guard)
	if guard.violation != "" {
		return nil, fmt.Errorf("rules: %s", guard.violation)
	}
	// expr.Env pins the typed environment (unknown root fields and type
	// mismatches fail at compile time), AsBool forces a boolean result,
	// MaxNodes bounds complexity, DisableAllBuiltins removes every callable
	// so expressions stay pure comparisons/logic over the environment.
	program, err := expr.Compile(code,
		expr.Env(Env{}),
		expr.AsBool(),
		expr.MaxNodes(MaxASTNodes),
		expr.DisableAllBuiltins(),
	)
	if err != nil {
		return nil, fmt.Errorf("rules: %w", err)
	}
	return program, nil
}

type compiledStep struct {
	match    *vm.Program // nil means the step matches unconditionally
	channels []string
}

type compiledRule struct {
	rule   Rule
	match  *vm.Program
	steps  []compiledStep
	forDur time.Duration // 0 = rule has no "for" window
	// groupBy lists the group aggregation fields; empty = no aggregation.
	groupBy []string
	// groupInterval is the quiet period that closes a group round.
	groupInterval time.Duration
	// inhibit is the parsed suppression spec; nil = rule is not suppressed.
	inhibit *InhibitSpec
	// inhibitTTL is the parsed inhibit.ttl (DefaultInhibitTTL when unset).
	inhibitTTL time.Duration
	// silence is the parsed daily quiet window; nil = rule is never silenced.
	silence *compiledSilence
	// escalation is the parsed ack-gated upgrade plan; nil = no escalation.
	escalation *EscalationPlan
}

// compiledSilence pairs the parsed daily window with the optional
// expression limiting which in-window events are silenced.
type compiledSilence struct {
	window *SilenceWindow
	match  *vm.Program // nil = everything in the window is silenced
}

// EvalError is one rule's evaluation failure, kept structured so callers
// can attribute observations to rules individually.
type EvalError struct {
	RuleID string
	Err    error
}

// Decision is the outcome of evaluating the rule table for one notification.
// nil return from Engine.Evaluate means no rule matched and static routing
// applies unchanged.
type Decision struct {
	// RuleID and Mode describe the governing rule: an active rule when one
	// matched, otherwise the first shadow hit (ModeShadow).
	RuleID string
	Mode   Mode
	// Channels are the resolved channels of the governing active rule;
	// empty unless Mode == ModeActive.
	Channels []string
	// Shadow lists every shadow-mode rule that matched this notification,
	// with the channels each would have used — the dry-run evidence.
	Shadow []ShadowHit
	// ForPending reports that the governing active rule matched but its
	// "for" duration has not elapsed yet: the event is suppressed (not
	// routed, not queued). Callers must treat this like a dedup hit.
	ForPending bool
	// Folded reports that the governing active rule matched and its group
	// aggregation window is open: the event has been counted into the
	// group but must not be delivered (like ForPending).
	Folded bool
	// Summary carries the folded events of a finished group round; the
	// caller delivers it as a summary notification together with this
	// event. It is only set when the event itself is being routed.
	Summary *GroupSummary
	// Inhibited reports that the governing active rule matched but a
	// root-cause rule (its inhibit.source) is currently present for the
	// same equal-field values: the event is suppressed like ForPending,
	// not discarded forever — the presence entry expires with its TTL.
	Inhibited bool
	// Silenced reports that the governing active rule matched inside its
	// daily silence window: the event is withheld for as long as the
	// window lasts (pure schedule-driven, no state involved).
	Silenced bool
	// Escalation is the governing active rule's ack-gated upgrade plan;
	// non-nil only when the event itself is routed and the rule declares
	// an escalation spec. The caller schedules the upgrade after delivery
	// and cancels it when an ack for the alert arrives in time.
	Escalation *EscalationPlan
	// GroupKey is the engine's identity of the alert group this event
	// belongs to (content hash, or the group_by field values). The ledger
	// and the recovery summary use it to correlate an alert episode with
	// its later recovery.
	GroupKey string
	// EvalErrors lists rules whose expressions failed to evaluate; these
	// rules were skipped and never contribute a match.
	EvalErrors []EvalError
}

// ShadowHit records one shadow rule match for dry-run observation.
type ShadowHit struct {
	RuleID   string
	Channels []string
}

// Engine owns the compiled rule table. Rules are compiled once at save
// time; evaluation only executes compiled programs. Stateful semantics
// (for-windows, group aggregation, inhibit presence) go through trackers
// backed by a StateStore — in-memory by default, Redis via SetStateStore.
type Engine struct {
	mu           sync.RWMutex
	store        Store
	rules        []*compiledRule
	forState     *ForTracker
	groupState   *GroupTracker
	inhibitState *InhibitTracker
	stateStore   StateStore
	// now is the clock for schedule-driven judgements (silence windows);
	// swapped in tests.
	now func() time.Time
	// onResolved, when set, fires when a delivered alert recovers: a
	// fired "for" group whose match stopped holding. Called outside the
	// engine lock; the receiver owns delivery of the recovery summary.
	onResolved func(ResolvedEvent)
	// inhibitIndex maps a source rule id to the compiled rules that
	// declare inhibit.source = that id. Rebuilt under mu whenever the
	// rule table changes; read under RLock.
	inhibitIndex map[string][]*compiledRule
}

// ResolvedEvent reports the recovery of an alert a rule had delivered: the
// group's window fired (the alert went out) and the rule's match has now
// stopped holding.
type ResolvedEvent struct {
	RuleID   string
	GroupKey string
	// State is the removed window state: Count events over
	// FirstSeen..LastSeen.
	State *RuleState
}

// SetResolvedFunc attaches the recovery callback (nil disables reporting).
func (e *Engine) SetResolvedFunc(fn func(ResolvedEvent)) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.onResolved = fn
}

// NewEngine creates an Engine backed by store. Call Reload once after
// creation to load and compile the stored rules. Rule state (for windows,
// group rounds, inhibit presence) is kept in memory by default; see
// SetStateStore.
func NewEngine(store Store) *Engine {
	ss := NewMemoryStateStore()
	return &Engine{
		store:        store,
		forState:     NewForTracker(ss),
		groupState:   NewGroupTracker(ss),
		inhibitState: NewInhibitTracker(ss),
		stateStore:   ss,
		now:          time.Now,
	}
}

// SetStateStore moves rule state (for windows, group rounds, inhibit
// presence) into the given store, e.g. RedisStateStore for multi-instance
// deployments. Call it during setup, before the engine serves traffic;
// in-flight state does not migrate.
func (e *Engine) SetStateStore(ss StateStore) {
	if ss == nil {
		ss = NewMemoryStateStore()
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.stateStore = ss
	e.forState = NewForTracker(ss)
	e.groupState = NewGroupTracker(ss)
	e.inhibitState = NewInhibitTracker(ss)
}

// Validate normalizes and checks r, then compiles every expression in it —
// the full save-time contract. It does not touch the store or live table.
func (e *Engine) Validate(r *Rule) error {
	r.Normalize()
	if err := r.Validate(); err != nil {
		return err
	}
	if _, err := compileExpr(r.Match); err != nil {
		return err
	}
	for i, step := range r.Route {
		if step.Match == "" {
			continue
		}
		if _, err := compileExpr(step.Match); err != nil {
			return fmt.Errorf("rules: rule %q: route step %d: %w", r.ID, i, err)
		}
	}
	if r.Silence != nil && r.Silence.Match != nil && *r.Silence.Match != "" {
		if _, err := compileExpr(*r.Silence.Match); err != nil {
			return fmt.Errorf("rules: rule %q: silence match: %w", r.ID, err)
		}
	}
	return nil
}

// Put validates then persists a rule and refreshes the live table in place
// (existing id keeps its priority position, new ids go last).
func (e *Engine) Put(ctx context.Context, r *Rule) error {
	if err := e.Validate(r); err != nil {
		return err
	}
	if err := e.store.Put(ctx, *r); err != nil {
		return fmt.Errorf("rules: store put: %w", err)
	}
	compiled, err := compileRule(*r)
	if err != nil {
		// Defensive: Validate just compiled the same expressions, so this
		// cannot trigger today; kept so a future refactor of Validate fails
		// loudly instead of silently skipping the live-table update.
		return err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	for i, existing := range e.rules {
		if existing.rule.ID == r.ID {
			e.rules[i] = compiled
			// The old rule's in-flight for windows and group rounds no longer
			// mean anything under the new definition; drop them so groups
			// start fresh.
			_ = e.forState.ResetRule(ctx, r.ID)
			_ = e.groupState.ResetRule(ctx, r.ID)
			e.rebuildInhibitIndexLocked()
			return nil
		}
	}
	e.rules = append(e.rules, compiled)
	e.rebuildInhibitIndexLocked()
	return nil
}

// Delete removes a rule from the store and the live table.
func (e *Engine) Delete(ctx context.Context, id string) error {
	if err := e.store.Delete(ctx, id); err != nil {
		return err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	for i, existing := range e.rules {
		if existing.rule.ID == id {
			e.rules = append(e.rules[:i], e.rules[i+1:]...)
			_ = e.forState.ResetRule(ctx, id)
			_ = e.groupState.ResetRule(ctx, id)
			e.rebuildInhibitIndexLocked()
			return nil
		}
	}
	return nil
}

// List returns the stored rules in priority order.
func (e *Engine) List(ctx context.Context) ([]Rule, error) {
	return e.store.List(ctx)
}

// Get returns a stored rule by id.
func (e *Engine) Get(ctx context.Context, id string) (Rule, error) {
	return e.store.Get(ctx, id)
}

// Reload re-reads every rule from the store and rebuilds the live table.
// It is atomic: if any rule fails to validate or compile, the previous
// table keeps serving and the error names the offending rule.
func (e *Engine) Reload(ctx context.Context) error {
	stored, err := e.store.List(ctx)
	if err != nil {
		return fmt.Errorf("rules: store list: %w", err)
	}
	compiled := make([]*compiledRule, 0, len(stored))
	for _, r := range stored {
		c, err := compileRule(r)
		if err != nil {
			return fmt.Errorf("rules: reload stopped at rule %q: %w", r.ID, err)
		}
		compiled = append(compiled, c)
	}
	e.mu.Lock()
	e.rules = compiled
	e.rebuildInhibitIndexLocked()
	e.mu.Unlock()
	return nil
}

// rebuildInhibitIndexLocked maps each inhibit source rule id to the
// compiled rules that reference it. Callers must hold e.mu for writing.
// Forward references are allowed: a target may name a source that does not
// exist (yet) — the index entry simply stays inert until such a rule is
// added, and presence is only ever written by an existing source rule.
func (e *Engine) rebuildInhibitIndexLocked() {
	index := make(map[string][]*compiledRule)
	for _, cr := range e.rules {
		if cr.inhibit == nil {
			continue
		}
		index[cr.inhibit.Source] = append(index[cr.inhibit.Source], cr)
	}
	e.inhibitIndex = index
}

func compileRule(r Rule) (*compiledRule, error) {
	r.Normalize()
	match, err := compileExpr(r.Match)
	if err != nil {
		return nil, fmt.Errorf("rules: rule %q: %w", r.ID, err)
	}
	steps := make([]compiledStep, len(r.Route))
	for i, step := range r.Route {
		c := compiledStep{channels: step.Channels}
		if step.Match != "" {
			c.match, err = compileExpr(step.Match)
			if err != nil {
				return nil, fmt.Errorf("rules: rule %q: route step %d: %w", r.ID, i, err)
			}
		}
		steps[i] = c
	}
	forDur := time.Duration(0)
	if r.For != nil {
		forDur, err = ParseFor(*r.For)
		if err != nil {
			return nil, fmt.Errorf("rules: rule %q: %w", r.ID, err)
		}
	}
	groupBy := r.GroupBy
	groupInterval := time.Duration(0)
	if len(groupBy) > 0 {
		groupInterval = DefaultGroupInterval
		if r.GroupInterval != nil {
			groupInterval, err = ParseGroupInterval(*r.GroupInterval)
			if err != nil {
				return nil, fmt.Errorf("rules: rule %q: %w", r.ID, err)
			}
		}
	}
	var inhibit *InhibitSpec
	inhibitTTL := time.Duration(0)
	if r.Inhibit != nil {
		inhibit = r.Inhibit
		inhibitTTL = DefaultInhibitTTL
		if r.Inhibit.TTL != nil {
			inhibitTTL, err = parseDurationField("inhibit ttl", *r.Inhibit.TTL)
			if err != nil {
				return nil, fmt.Errorf("rules: rule %q: %w", r.ID, err)
			}
		}
	}
	var silence *compiledSilence
	if r.Silence != nil {
		window, err := ParseSilenceWindow(r.Silence.Start, r.Silence.End)
		if err != nil {
			return nil, fmt.Errorf("rules: rule %q: %w", r.ID, err)
		}
		silence = &compiledSilence{window: window}
		if r.Silence.Match != nil && *r.Silence.Match != "" {
			silence.match, err = compileExpr(*r.Silence.Match)
			if err != nil {
				return nil, fmt.Errorf("rules: rule %q: silence match: %w", r.ID, err)
			}
		}
	}
	var escalation *EscalationPlan
	if r.Escalation != nil {
		timeout := DefaultAckTimeout
		if r.Escalation.AckTimeout != "" {
			timeout, err = parseDurationField("ack_timeout", r.Escalation.AckTimeout)
			if err != nil {
				return nil, fmt.Errorf("rules: rule %q: %w", r.ID, err)
			}
		}
		escalation = &EscalationPlan{Timeout: timeout, To: r.Escalation.To}
	}
	return &compiledRule{rule: r, match: match, steps: steps, forDur: forDur, groupBy: groupBy, groupInterval: groupInterval, inhibit: inhibit, inhibitTTL: inhibitTTL, silence: silence, escalation: escalation}, nil
}

// Evaluate runs the notification environment against the rule table in
// priority order and returns the resulting Decision (nil when nothing
// matched). Evaluation failures (e.g. expressions referencing missing
// params) skip the offending rule and are reported both structurally in
// Decision.EvalErrors and joined into the returned error — a broken rule
// must not break routing for the rest of the table.
//
// Rules with a "for" window are judged through the ForTracker: a hit whose
// window has not elapsed yet yields ForPending for active rules (the event
// is suppressed) and no shadow record for shadow rules; a match miss resets
// the window, because the duration counts CONTINUOUS holding.
func (e *Engine) Evaluate(ctx context.Context, env Env) (*Decision, error) {
	e.mu.RLock()
	rules := make([]*compiledRule, len(e.rules))
	copy(rules, e.rules)
	e.mu.RUnlock()

	var decision *Decision
	var evalErrs []EvalError
	for _, cr := range rules {
		if cr.rule.Mode == ModeOff {
			continue
		}
		matched, err := e.runProgram(ctx, cr.match, env)
		if err != nil {
			evalErrs = append(evalErrs, EvalError{RuleID: cr.rule.ID, Err: err})
			continue
		}
		if !matched {
			// Condition stopped holding: drop the in-progress window so the
			// next hit starts the duration from scratch. Reset failures are
			// ignored — the state TTL is the backstop, and a miss must not
			// turn into an evaluation error.
			if cr.forDur > 0 {
				groupKey, _ := RuleGroupKey(cr.groupBy, env)
				if state, err := e.forState.Take(ctx, cr.rule.ID, groupKey); err == nil && state != nil && state.Fired {
					// The alert this rule delivered has recovered.
					e.mu.RLock()
					fn := e.onResolved
					e.mu.RUnlock()
					if fn != nil {
						fn(ResolvedEvent{RuleID: cr.rule.ID, GroupKey: groupKey, State: state})
					}
				}
			}
			continue
		}

		// for and group aggregation share the group key: with group_by the
		// identity is the field values, without it the content hash.
		groupKey, groupLabel := RuleGroupKey(cr.groupBy, env)

		// Step -1: the daily silence window. Schedule-driven and stateless:
		// inside the window (and, when the silence match is set, for
		// matching events only) the rule is frozen — the event is withheld
		// and no for/group state advances. Only active rules check: shadow
		// observation is about condition hits.
		if cr.silence != nil && cr.rule.Mode == ModeActive && cr.silence.window.Contains(e.now()) {
			if cr.silence.match == nil {
				governing := &Decision{RuleID: cr.rule.ID, Mode: ModeActive, Silenced: true}
				governing.EvalErrors = evalErrs
				return governing, joinEvalErrors(evalErrs)
			}
			inWindow, err := e.runProgram(ctx, cr.silence.match, env)
			if err != nil {
				evalErrs = append(evalErrs, EvalError{RuleID: cr.rule.ID, Err: err})
				continue
			}
			if inWindow {
				governing := &Decision{RuleID: cr.rule.ID, Mode: ModeActive, Silenced: true}
				governing.EvalErrors = evalErrs
				return governing, joinEvalErrors(evalErrs)
			}
		}

		// Step 0: inhibition. While the root-cause rule (inhibit.source)
		// is delivering for the same equal-field values, this rule's
		// events are withheld — before any for/group state is touched, a
		// suppressed event must not open windows or rounds. Only active
		// rules check: shadow observation is about condition hits.
		if cr.inhibit != nil && cr.rule.Mode == ModeActive {
			hash := EqualFieldsHash(cr.inhibit.Equal, env)
			present, err := e.inhibitState.Present(ctx, cr.rule.ID, hash)
			if err != nil {
				// State failure fails open: the rule is skipped like any
				// other evaluation error and routing survives.
				evalErrs = append(evalErrs, EvalError{RuleID: cr.rule.ID, Err: err})
				continue
			}
			if present {
				governing := &Decision{RuleID: cr.rule.ID, Mode: ModeActive, Inhibited: true}
				governing.EvalErrors = evalErrs
				return governing, joinEvalErrors(evalErrs)
			}
		}

		// Step 1: the "for" window gates everything else. While it runs
		// the event is suppressed; after it fired once, further hits stay
		// silent — and for rules with group_by they fall through to the
		// aggregation below so folded events are still counted.
		if cr.forDur > 0 {
			outcome, err := e.forState.Observe(ctx, cr.rule.ID, groupKey, cr.forDur)
			if err != nil {
				// State failure fails open: the rule is skipped like any
				// other evaluation error and routing survives.
				evalErrs = append(evalErrs, EvalError{RuleID: cr.rule.ID, Err: err})
				continue
			}
			suppress := outcome == ForPending ||
				(outcome == ForSilent && len(cr.groupBy) == 0)
			if suppress {
				switch cr.rule.Mode {
				case ModeActive:
					// The rule owns this group: suppress the event, skip
					// the remaining table (a later rule must not route
					// what this rule holds).
					governing := &Decision{RuleID: cr.rule.ID, Mode: ModeActive, ForPending: true}
					governing.EvalErrors = evalErrs
					return governing, joinEvalErrors(evalErrs)
				default: // ModeShadow
					// A shadow rule only records what WOULD fire; a
					// pending or already-silent window would not.
					continue
				}
			}
		}

		// Step 2: group aggregation. Only active rules simulate folding —
		// shadow observation is about condition hits, and its records stay
		// unsuppressed (the delivery behavior is not what's being previewed).
		var summary *GroupSummary
		if len(cr.groupBy) > 0 && cr.rule.Mode == ModeActive {
			folded, gs, err := e.groupState.Observe(ctx, cr.rule.ID, groupKey, groupLabel, cr.groupInterval)
			if err != nil {
				evalErrs = append(evalErrs, EvalError{RuleID: cr.rule.ID, Err: err})
				continue
			}
			if folded {
				governing := &Decision{RuleID: cr.rule.ID, Mode: ModeActive, Folded: true}
				governing.EvalErrors = evalErrs
				return governing, joinEvalErrors(evalErrs)
			}
			summary = gs
		}
		channels, err := e.resolveSteps(ctx, cr, env)
		if err != nil {
			evalErrs = append(evalErrs, EvalError{RuleID: cr.rule.ID, Err: err})
			continue
		}
		switch cr.rule.Mode {
		case ModeActive:
			if len(channels) == 0 {
				// The rule matched but no route step did; keep looking.
				continue
			}
			// First matching active rule governs; shadow observations ride along.
			governing := &Decision{RuleID: cr.rule.ID, Mode: ModeActive, Channels: channels, Summary: summary, Escalation: cr.escalation, GroupKey: groupKey}
			if decision != nil {
				governing.Shadow = decision.Shadow
			}
			// The delivery is happening: mark presence for every rule that
			// inhibits on this one, so their equal-matching events are
			// withheld while the root cause keeps firing. A write failure
			// must not block the delivery (fail open toward delivery);
			// it surfaces as an eval error for visibility.
			e.mu.RLock()
			targets := e.inhibitIndex[cr.rule.ID]
			targetsCopy := make([]*compiledRule, len(targets))
			copy(targetsCopy, targets)
			e.mu.RUnlock()
			for _, target := range targetsCopy {
				hash := EqualFieldsHash(target.inhibit.Equal, env)
				if err := e.inhibitState.Record(ctx, target.rule.ID, hash, target.inhibitTTL); err != nil {
					evalErrs = append(evalErrs, EvalError{RuleID: target.rule.ID, Err: err})
				}
			}
			governing.EvalErrors = evalErrs
			return governing, joinEvalErrors(evalErrs)
		case ModeShadow:
			if decision == nil {
				decision = &Decision{RuleID: cr.rule.ID, Mode: ModeShadow}
			}
			decision.Shadow = append(decision.Shadow, ShadowHit{RuleID: cr.rule.ID, Channels: channels})
		}
	}
	if decision != nil {
		decision.EvalErrors = evalErrs
	}
	return decision, joinEvalErrors(evalErrs)
}

func joinEvalErrors(evalErrs []EvalError) error {
	if len(evalErrs) == 0 {
		return nil
	}
	errs := make([]error, len(evalErrs))
	for i, ee := range evalErrs {
		errs[i] = fmt.Errorf("rule %q: %w", ee.RuleID, ee.Err)
	}
	return errors.Join(errs...)
}

// resolveSteps returns the channels of the first step whose match holds;
// an empty step match always matches. A rule whose steps all miss yields
// no channels (empty slice), which the caller treats as fall-through.
func (e *Engine) resolveSteps(ctx context.Context, cr *compiledRule, env Env) ([]string, error) {
	for _, step := range cr.steps {
		if step.match == nil {
			return step.channels, nil
		}
		matched, err := e.runProgram(ctx, step.match, env)
		if err != nil {
			return nil, err
		}
		if matched {
			return step.channels, nil
		}
	}
	return nil, nil
}

// runProgram executes a compiled expression under the eval timeout.
// program is never nil here: resolveSteps filters catch-all steps before
// calling, and compileExpr only returns non-nil programs.
func (e *Engine) runProgram(ctx context.Context, program *vm.Program, env Env) (bool, error) {
	runCtx, cancel := context.WithTimeout(ctx, evalTimeout)
	defer cancel()

	type evalResult struct {
		out any
		err error
	}
	done := make(chan evalResult, 1)
	go func() {
		out, err := expr.Run(program, env)
		done <- evalResult{out, err}
	}()
	select {
	case r := <-done:
		if r.err != nil {
			return false, r.err
		}
		matched, ok := r.out.(bool)
		if !ok {
			// Defensive: AsBool rejects non-boolean expressions at compile
			// time; kept so an unexpected expr runtime change fails loudly.
			return false, fmt.Errorf("expression returned %T, want bool", r.out)
		}
		return matched, nil
	case <-runCtx.Done():
		return false, fmt.Errorf("evaluation exceeded %v (or context canceled): %w", evalTimeout, runCtx.Err())
	}
}
