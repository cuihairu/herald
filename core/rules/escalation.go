package rules

import "time"

// DefaultAckTimeout is the escalation window when a rule sets escalation
// without an explicit ack_timeout.
const DefaultAckTimeout = 5 * time.Minute

// EscalationPlan is the compiled ack-gated upgrade plan of a rule: if the
// alert (identified by params "alert_id") is not acknowledged within
// Timeout, the delivery is repeated on the To channels. The plan is
// produced by the engine on a governing match; the scheduling itself
// happens after delivery (service layer) — evaluation stays pure.
type EscalationPlan struct {
	Timeout time.Duration
	To      []string
}
