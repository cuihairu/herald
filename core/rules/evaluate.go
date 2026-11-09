package rules

import "context"

// evalWalk carries the per-Evaluate state shared by the decision steps:
// the engine snapshot (roster source), the evaluation environment, the
// accumulated eval errors and shadow observations, and the per-rule
// group identity and summary handed from one step to the next.
type evalWalk struct {
	engine       *Engine
	ctx          context.Context
	env          Env
	rosterSource RosterSource
	evalErrs     []EvalError
	decision     *Decision
	groupKey     string
	groupLabel   string
	summary      *GroupSummary
}

// fail records an evaluation error and reports the rule as done: like
// every state/program failure it fails open — the rule is skipped and
// routing survives.
func (w *evalWalk) fail(ruleID string, err error) (*Decision, bool) {
	w.evalErrs = append(w.evalErrs, EvalError{RuleID: ruleID, Err: err})
	return nil, true
}

// governing finishes the walk with a decided outcome: the rule's verdict
// rides with every error accumulated so far.
func (w *evalWalk) governing(d *Decision) (*Decision, bool) {
	d.EvalErrors = w.evalErrs
	return d, true
}

// resetOnMiss drops the in-progress for-window when the condition
// stopped holding, so the next hit starts the duration from scratch.
// Reset failures are ignored — the state TTL is the backstop, and a
// miss must not turn into an evaluation error.
func (w *evalWalk) resetOnMiss(cr *compiledRule) {
	if cr.forDur <= 0 {
		return
	}
	groupKey, _ := RuleGroupKey(cr.groupBy, w.env)
	if state, err := w.engine.forState.Take(w.ctx, cr.rule.ID, groupKey); err == nil && state != nil && state.Fired {
		// The alert this rule delivered has recovered.
		w.engine.mu.RLock()
		fn := w.engine.onResolved
		w.engine.mu.RUnlock()
		if fn != nil {
			fn(ResolvedEvent{RuleID: cr.rule.ID, GroupKey: groupKey, State: state})
		}
	}
}

// stepImmediate returns the decision for non-route actions on match:
// allow keeps the caller's course (explicit channels, else static
// routing) and stops the table; suppress withholds. The route-gated
// machinery below (silence/inhibit/for/group) is route semantics —
// validation forbids combining it with allow/suppress — and shadow
// rules only observe, which the tail switch handles.
func (w *evalWalk) stepImmediate(cr *compiledRule) (*Decision, bool) {
	if cr.rule.Mode != ModeActive || cr.action == ActionRoute {
		return nil, false
	}
	governing := &Decision{RuleID: cr.rule.ID, Mode: ModeActive, Action: cr.action}
	if w.decision != nil {
		governing.Shadow = w.decision.Shadow
	}
	return w.governing(governing)
}

// stepSilence applies the Step -1 silence schedule — daily window or
// duty roster. Schedule-driven and stateless: inside the schedule (and,
// when the silence match is set, for matching events only) the rule is
// frozen — the event is withheld and no for/group state advances. Only
// active rules check: shadow observation is about condition hits.
func (w *evalWalk) stepSilence(cr *compiledRule) (*Decision, bool) {
	if cr.silence == nil || cr.rule.Mode != ModeActive {
		return nil, false
	}
	if !silenceQuiet(w.rosterSource, cr.silence, w.engine.now()) {
		return nil, false
	}
	if cr.silence.match == nil {
		return w.governing(&Decision{RuleID: cr.rule.ID, Mode: ModeActive, Silenced: true})
	}
	inSchedule, err := w.engine.runProgram(w.ctx, cr.silence.match, w.env)
	if err != nil {
		return w.fail(cr.rule.ID, err)
	}
	if inSchedule {
		return w.governing(&Decision{RuleID: cr.rule.ID, Mode: ModeActive, Silenced: true})
	}
	return nil, false
}

// stepInhibit applies Step 0 inhibition. While the root-cause rule
// (inhibit.source) is delivering for the same equal-field values, this
// rule's events are withheld — before any for/group state is touched, a
// suppressed event must not open windows or rounds. Only active rules
// check: shadow observation is about condition hits.
func (w *evalWalk) stepInhibit(cr *compiledRule) (*Decision, bool) {
	if cr.inhibit == nil || cr.rule.Mode != ModeActive {
		return nil, false
	}
	hash := EqualFieldsHash(cr.inhibit.Equal, w.env)
	present, err := w.engine.inhibitState.Present(w.ctx, cr.rule.ID, hash)
	if err != nil {
		// State failure fails open: the rule is skipped like any other
		// evaluation error and routing survives.
		return w.fail(cr.rule.ID, err)
	}
	if present {
		return w.governing(&Decision{RuleID: cr.rule.ID, Mode: ModeActive, Inhibited: true})
	}
	return nil, false
}

// stepForWindow applies Step 1, the "for" window that gates everything
// else. While it runs the event is suppressed; after it fired once,
// further hits stay silent — and for rules with group_by they fall
// through to the aggregation below so folded events are still counted.
func (w *evalWalk) stepForWindow(cr *compiledRule) (*Decision, bool) {
	if cr.forDur <= 0 {
		return nil, false
	}
	outcome, err := w.engine.forState.Observe(w.ctx, cr.rule.ID, w.groupKey, cr.forDur)
	if err != nil {
		// State failure fails open: the rule is skipped like any other
		// evaluation error and routing survives.
		return w.fail(cr.rule.ID, err)
	}
	suppress := outcome == ForPending ||
		(outcome == ForSilent && len(cr.groupBy) == 0)
	if !suppress {
		return nil, false
	}
	switch cr.rule.Mode {
	case ModeActive:
		// The rule owns this group: suppress the event, skip the
		// remaining table (a later rule must not route what this rule
		// holds).
		return w.governing(&Decision{RuleID: cr.rule.ID, Mode: ModeActive, ForPending: true})
	default: // ModeShadow
		// A shadow rule only records what WOULD fire; a pending or
		// already-silent window would not.
		return nil, true
	}
}

// stepGroupFold applies Step 2, group aggregation. Only active rules
// simulate folding — shadow observation is about condition hits, and
// its records stay unsuppressed (the delivery behavior is not what's
// being previewed).
func (w *evalWalk) stepGroupFold(cr *compiledRule) (*Decision, bool) {
	if len(cr.groupBy) == 0 || cr.rule.Mode != ModeActive {
		return nil, false
	}
	folded, gs, err := w.engine.groupState.Observe(w.ctx, cr.rule.ID, w.groupKey, w.groupLabel, cr.groupInterval)
	if err != nil {
		return w.fail(cr.rule.ID, err)
	}
	if folded {
		return w.governing(&Decision{RuleID: cr.rule.ID, Mode: ModeActive, Folded: true})
	}
	w.summary = gs
	return nil, false
}

// stepDeliver resolves the route steps and finishes the rule: the first
// matching active rule governs (and marks presence for every rule that
// inhibits on it), shadow observations accumulate on the walk.
func (w *evalWalk) stepDeliver(cr *compiledRule) (*Decision, bool) {
	channels, err := w.engine.resolveSteps(w.ctx, cr, w.env)
	if err != nil {
		return w.fail(cr.rule.ID, err)
	}
	switch cr.rule.Mode {
	case ModeActive:
		if len(channels) == 0 {
			// The rule matched but no route step did; keep looking.
			return nil, true
		}
		governing := &Decision{RuleID: cr.rule.ID, Mode: ModeActive, Action: ActionRoute, Channels: channels, Summary: w.summary, Escalation: cr.escalation, GroupKey: w.groupKey}
		if w.decision != nil {
			governing.Shadow = w.decision.Shadow
		}
		// The delivery is happening: mark presence for every rule that
		// inhibits on this one, so their equal-matching events are
		// withheld while the root cause keeps firing. A write failure
		// must not block the delivery (fail open toward delivery);
		// it surfaces as an eval error for visibility.
		w.engine.mu.RLock()
		targets := w.engine.inhibitIndex[cr.rule.ID]
		targetsCopy := make([]*compiledRule, len(targets))
		copy(targetsCopy, targets)
		w.engine.mu.RUnlock()
		for _, target := range targetsCopy {
			hash := EqualFieldsHash(target.inhibit.Equal, w.env)
			if err := w.engine.inhibitState.Record(w.ctx, target.rule.ID, hash, target.inhibitTTL); err != nil {
				w.evalErrs = append(w.evalErrs, EvalError{RuleID: target.rule.ID, Err: err})
			}
		}
		return w.governing(governing)
	default: // ModeShadow
		if w.decision == nil {
			w.decision = &Decision{RuleID: cr.rule.ID, Mode: ModeShadow}
		}
		w.decision.Shadow = append(w.decision.Shadow, ShadowHit{RuleID: cr.rule.ID, Action: cr.action, Channels: channels})
		return nil, true
	}
}
