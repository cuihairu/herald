package audience

import (
	"context"
	"fmt"
	"sort"
)

// SurfaceProbe is the one question 对账 (§8 rule 3) asks the outside
// world per registered handle: does the platform still know it? A probe
// answers for exactly one channel — telegram chat validity, MP subscribe
// flag — and reports errors honestly: a probe that cannot ask (auth
// failure, network down) must error, not guess false, or reconciliation
// would unsubscribe people on its own blind spots.
type SurfaceProbe interface {
	// Channel names the channel this probe can verify (the same
	// vocabulary surfaces use).
	Channel() string
	// Valid reports whether the platform still recognizes target.
	Valid(ctx context.Context, target string) (bool, error)
}

// ReconcileReport is one RunOnce's outcome, per channel probed.
type ReconcileReport struct {
	// Checked counts active surfaces the probe actually asked.
	Checked int
	// Corrected counts surfaces marked invalid this round (each with its
	// channel subscriptions terminated).
	Corrected int
	// Skipped counts probe errors (channel-blind rounds, never guesses).
	Skipped int
	// Errors carries the per-surface probe failures, if any.
	Errors []string
}

// Reconciler is the §8 consistency half: Herald 只信自己登记的关系, but
// the platforms move underneath — followers unfollow without firing the
// webhook, chats go dead. RunOnce asks each probe about every active
// surface on its channel and corrects the registry from the answers:
// a handle the platform no longer knows goes invalid and its channel
// subscriptions terminate, exactly as an unfollow would (取关回流全停的
// 对账形态), audited under the "reconcile" actor.
//
// The loop lives with the deployment (heraldd ticks it under the digest
// leader lock, §14) — this type is one synchronous sweep.
type Reconciler struct {
	surfaces  *SurfaceRegistry
	relations *Registry
	probes    map[string]SurfaceProbe
}

// ReconcileSource is the actor recorded on reconciliation corrections.
const ReconcileSource = "reconcile"

// NewReconciler wires the sweep. Probes key by their channel; a channel
// without a probe is simply not reconciled — no probe, no questions.
func NewReconciler(surfaces *SurfaceRegistry, relations *Registry, probes ...SurfaceProbe) *Reconciler {
	if surfaces == nil || relations == nil {
		panic("audience: reconciler needs both registries")
	}
	r := &Reconciler{surfaces: surfaces, relations: relations, probes: make(map[string]SurfaceProbe, len(probes))}
	for _, p := range probes {
		r.probes[p.Channel()] = p
	}
	return r
}

// Channels lists the channels this reconciler can verify, stable order
// for tests and status pages.
func (r *Reconciler) Channels() []string {
	out := make([]string, 0, len(r.probes))
	for ch := range r.probes {
		out = append(out, ch)
	}
	sort.Strings(out)
	return out
}

// RunOnce sweeps every registered surface whose channel has a probe.
// Only active surfaces are asked — pending has nothing live to lose,
// invalid is already stopped. A "no" from the platform applies the
// unfollow correction (invalidate + terminate subscriptions); a probe
// error skips that surface and lands in the report.
func (r *Reconciler) RunOnce(ctx context.Context) ReconcileReport {
	var report ReconcileReport

	// Snapshot first: corrections mutate the maps the iteration reads.
	type target struct{ audienceID, channel, handle string }
	var targets []target
	for _, audienceID := range r.audienceIDs() {
		for _, surface := range r.surfaces.Surfaces(audienceID) {
			if surface.Status != SurfaceActive {
				continue
			}
			if _, probed := r.probes[surface.Channel]; !probed {
				continue
			}
			targets = append(targets, target{audienceID, surface.Channel, surface.Target})
		}
	}

	for _, t := range targets {
		if err := ctx.Err(); err != nil {
			report.Errors = append(report.Errors, fmt.Sprintf("reconcile canceled: %v", err))
			return report
		}
		valid, err := r.probes[t.channel].Valid(ctx, t.handle)
		if err != nil {
			report.Skipped++
			report.Errors = append(report.Errors, fmt.Sprintf("%s/%s: probe: %v", t.audienceID, t.channel, err))
			continue
		}
		report.Checked++
		if valid {
			continue
		}
		if err := r.surfaces.InvalidateFor(t.audienceID, t.channel,
			fmt.Sprintf("reconcile: platform no longer knows %s", t.handle)); err != nil {
			report.Errors = append(report.Errors, fmt.Sprintf("%s/%s: invalidate: %v", t.audienceID, t.channel, err))
			continue
		}
		report.Corrected++
		for _, rel := range r.relations.RelationsByType(t.audienceID, RelationSubscription) {
			if rel.Channel != t.channel {
				continue
			}
			if _, err := r.relations.TerminateFor(t.audienceID, rel.Category, t.channel, ReconcileSource); err != nil {
				report.Errors = append(report.Errors, fmt.Sprintf("%s/%s/%s: terminate: %v", t.audienceID, rel.Category, t.channel, err))
			}
		}
	}
	return report
}

// audienceIDs snapshots the surface map's keys. The registry has no
// public listing — the reconciler is in-package, so it reads directly,
// under the same lock Surfaces uses.
func (r *Reconciler) audienceIDs() []string {
	r.surfaces.mu.RLock()
	defer r.surfaces.mu.RUnlock()
	out := make([]string, 0, len(r.surfaces.surfaces))
	for id := range r.surfaces.surfaces {
		out = append(out, id)
	}
	return out
}
