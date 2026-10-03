// Package roster implements duty rosters (值班表): named schedules of
// absolute time periods that an external scheduling system pushes into
// herald through the API, and that a rule's silence can name as its
// schedule source (the rule-engine design's 值班表对接排班占位).
//
// Herald stores and evaluates pushed periods only — rotating schedules,
// shift authoring and who-is-on-call stay outside; the roster is a seam
// for an external scheduler, not a built-in scheduling engine. See
// docs/rule-engine-design.md and docs/guide/configuration.md.
package roster

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Period is one pushed shift: a half-open absolute interval [Start, End)
// during which the roster is in effect. RFC3339 on the wire.
type Period struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

// Roster is a named duty schedule: id, an optional human description and
// the periods the external scheduler pushed. A silence naming this roster
// is quiet exactly while a period covers the moment.
type Roster struct {
	ID          string   `json:"id"`
	Description string   `json:"description,omitempty"`
	Periods     []Period `json:"periods"`
}

// idPattern is the canonical herald id shape, shared by rules, groups and
// rosters so a reference can be rejected at save time when it could never
// resolve.
var idPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,63}$`)

// Bounds: a roster is a schedule, not an archive. 512 periods is years of
// weekly maintenance windows at one push per change; a longer list almost
// always means the scheduler is pushing history instead of replacing the
// current schedule.
const (
	maxPeriods          = 512
	maxDescriptionChars = 512
)

// Normalize trims the id and sorts periods by start so Covers can stop at
// the first period that starts after the queried time.
func (r *Roster) Normalize() {
	r.ID = strings.TrimSpace(r.ID)
	sort.SliceStable(r.Periods, func(i, j int) bool {
		return r.Periods[i].Start.Before(r.Periods[j].Start)
	})
}

// Validate checks structural hygiene: a well-formed bounded id and
// description, at least one and at most maxPeriods well-formed periods,
// and — in the sorted order Normalize establishes — no period out of order
// and none starting before the previous one ends. Overlapping periods are
// rejected instead of merged: two pushes claiming the same moment is a
// scheduler bug that deserves a loud save failure, not a silent union.
// An empty schedule is rejected too — clearing the roster is a delete,
// which fails the silence gate open with the same effect.
func (r Roster) Validate() error {
	if !idPattern.MatchString(r.ID) {
		return fmt.Errorf("roster: invalid roster id %q (want 1-64 chars of letters, digits, dot, dash, underscore)", r.ID)
	}
	if len(r.Description) > maxDescriptionChars {
		return fmt.Errorf("roster: roster %q: description is %d chars, max is %d", r.ID, len(r.Description), maxDescriptionChars)
	}
	if len(r.Periods) == 0 {
		return fmt.Errorf("roster: roster %q requires at least one period (delete the roster to clear its schedule)", r.ID)
	}
	if len(r.Periods) > maxPeriods {
		return fmt.Errorf("roster: roster %q: %d periods, max is %d", r.ID, len(r.Periods), maxPeriods)
	}
	for i, p := range r.Periods {
		if p.Start.IsZero() || p.End.IsZero() {
			return fmt.Errorf("roster: roster %q: period %d is missing a start or end (want RFC3339 timestamps)", r.ID, i)
		}
		if !p.End.After(p.Start) {
			return fmt.Errorf("roster: roster %q: period %d ends at or before it starts", r.ID, i)
		}
		if i == 0 {
			continue
		}
		prev := r.Periods[i-1]
		if p.Start.Before(prev.Start) {
			return fmt.Errorf("roster: roster %q: period %d is out of start order (Normalize first)", r.ID, i)
		}
		if p.Start.Before(prev.End) {
			return fmt.Errorf("roster: roster %q: period %d overlaps the previous period", r.ID, i)
		}
	}
	return nil
}

// Covers reports whether any period covers t, half-open: a period ending
// at 06:00 covers up to but not including 06:00. Periods are in
// Normalize's sorted order, so the scan stops at the first period that
// starts after t. An empty roster covers nothing.
func (r Roster) Covers(t time.Time) bool {
	for _, p := range r.Periods {
		if p.Start.After(t) {
			return false
		}
		if t.Before(p.End) {
			return true
		}
	}
	return false
}
