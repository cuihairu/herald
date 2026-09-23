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
// `level == "error" && params.fail_rate > 0.05`.
type Env struct {
	Type   string         `expr:"type"`
	Level  string         `expr:"level"`
	Title  string         `expr:"title"`
	Body   string         `expr:"body"`
	Params map[string]any `expr:"params"`
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
	rule  Rule
	match *vm.Program
	steps []compiledStep
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
// time; evaluation only executes compiled programs.
type Engine struct {
	mu    sync.RWMutex
	store Store
	rules []*compiledRule
}

// NewEngine creates an Engine backed by store. Call Reload once after
// creation to load and compile the stored rules.
func NewEngine(store Store) *Engine {
	return &Engine{store: store}
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
			return nil
		}
	}
	e.rules = append(e.rules, compiled)
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
			return nil
		}
	}
	return nil
}

// List returns the stored rules in priority order.
func (e *Engine) List(ctx context.Context) ([]Rule, error) {
	return e.store.List(ctx)
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
	e.mu.Unlock()
	return nil
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
	return &compiledRule{rule: r, match: match, steps: steps}, nil
}

// Evaluate runs the notification environment against the rule table in
// priority order and returns the resulting Decision (nil when nothing
// matched). Evaluation failures (e.g. expressions referencing missing
// params) skip the offending rule and are reported both structurally in
// Decision.EvalErrors and joined into the returned error — a broken rule
// must not break routing for the rest of the table.
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
			continue
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
			governing := &Decision{RuleID: cr.rule.ID, Mode: ModeActive, Channels: channels}
			if decision != nil {
				governing.Shadow = decision.Shadow
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
