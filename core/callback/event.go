// Package callback is the §13.5 webhook 回调 face: delivery results and
// unsubscribe backflow pushed to an app's registered URL. Events ride
// the same queue→worker pipeline as deliveries — at-least-once with the
// retry budget the queue already owns — and every event carries an event
// id (plus an HMAC-SHA256 signature over the exact body) so the app can
// dedupe repeats and verify the source.
package callback

import (
	"time"
)

// Event kinds.
const (
	// KindDeliveryResult reports one app-sourced delivery task's settled
	// status (§13.5 投递结果): success, or the final failure with the
	// provider's error. One event per settled task — retries before the
	// settle are the pipeline's business, not the app's.
	KindDeliveryResult = "delivery_result"
	// KindUnsubscribe reports one relation ending at the audience's word
	// (§13.5 退订事件回流), so the app can sync its local state.
	KindUnsubscribe = "unsubscribe"
)

// DeliveryResult is the payload of a delivery_result event.
type DeliveryResult struct {
	TaskID string `json:"task_id"`
	// EventID echoes the integrator's own event identity (the dispatch
	// request's event_id), not this callback event's id — the app
	// correlates its original event (e.g. an outbox row id) to the
	// settled delivery through it. Empty when the dispatch carried none.
	EventID    string `json:"event_id,omitempty"`
	AudienceID string `json:"audience_id,omitempty"`
	Category   string `json:"category,omitempty"`
	Channel    string `json:"channel,omitempty"`
	Status     string `json:"status"`
	Error      string `json:"error,omitempty"`
}

// Unsubscribe is the payload of an unsubscribe event.
type Unsubscribe struct {
	AudienceID   string `json:"audience_id"`
	Category     string `json:"category"`
	Channel      string `json:"channel"`
	RelationType string `json:"relation_type"`
	Actor        string `json:"actor,omitempty"`
}

// Event is one callback payload. The app dedupes on EventID: the
// at-least-once contract means the same event may arrive more than once.
type Event struct {
	EventID  string          `json:"event_id"`
	App      string          `json:"app"`
	Kind     string          `json:"kind"`
	At       time.Time       `json:"at"`
	Delivery *DeliveryResult `json:"delivery,omitempty"`
	Unsub    *Unsubscribe    `json:"unsubscribe,omitempty"`
}
