package callback

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/cuihairu/herald/core"
	"github.com/cuihairu/herald/core/apps"
	"github.com/cuihairu/herald/core/audience"
	"github.com/google/uuid"
)

// SignatureHeader carries the hex HMAC-SHA256 of the exact request body
// under the app's callback secret; EventIDHeader repeats the event id
// for convenience routing on the app side.
const (
	SignatureHeader = "X-Herald-Signature"
	EventIDHeader   = "X-Herald-Event-ID"
	// signaturePrefix follows the `scheme=<hex>` convention so the app
	// can migrate algorithms without a header change.
	signaturePrefix = "sha256="
)

// Emitter turns face events into signed queue tasks. The HTTP itself
// rides the delivery pipeline as an app-callback task: retry budget,
// at-least-once re-enqueue and the worker pool come from the machinery
// the deliveries already use. An app without callback configuration is
// skipped — the face is closed per app, not a process-wide switch.
type Emitter struct {
	apps  *apps.Registry
	queue core.Queue
	now   func() time.Time
}

// NewEmitter builds the callback emitter over the app registry (the
// per-app URL/secret source) and the delivery queue.
func NewEmitter(reg *apps.Registry, q core.Queue) *Emitter {
	return &Emitter{apps: reg, queue: q, now: time.Now}
}

// OnSettle is the runtime.Manager settle hook: every settled app-sourced
// delivery task becomes a delivery_result event. Push errors are
// swallowed — the callback is a best-effort mirror of state the §13.4
// query face already serves, and a full queue must not fail a delivery
// that succeeded.
func (e *Emitter) OnSettle(task *core.DeliveryTask, err error) {
	if task == nil || e.queue == nil {
		return
	}
	app, ok := appOf(task.Source)
	if !ok {
		return
	}
	status := "success"
	errMsg := ""
	if err != nil {
		status = "failed"
		errMsg = err.Error()
	}
	channel := task.Provider
	_ = e.push(app, Event{
		App:  app,
		Kind: KindDeliveryResult,
		Delivery: &DeliveryResult{
			TaskID:     task.ID,
			EventID:    task.EventID,
			AudienceID: task.AudienceID,
			Category:   task.Category,
			Channel:    channel,
			Status:     status,
			Error:      errMsg,
		},
	})
}

// Unsubscribed is the relation registry's unsubscribe hook: a relation
// ending whose entry source belongs to an app namespace flows back to
// that app. Relations from other entries (bot, preference_center) are
// not the app's to hear about.
func (e *Emitter) Unsubscribed(rel audience.Relation, actor string) {
	if e.queue == nil {
		return
	}
	app, ok := appOf(rel.Source)
	if !ok {
		return
	}
	_ = e.push(app, Event{
		App:  app,
		Kind: KindUnsubscribe,
		Unsub: &Unsubscribe{
			AudienceID:   rel.AudienceID,
			Category:     rel.Category,
			Channel:      rel.Channel,
			RelationType: string(rel.Type),
			Actor:        actor,
		},
	})
}

// push signs one event and enqueues its callback task. Errors return to
// the direct callers (tests, future sync callers); the hook callers
// above discard them by contract.
func (e *Emitter) push(app string, ev Event) error {
	cb, ok := e.apps.Callback(app)
	if !ok {
		// The app never configured the face (or cleared it): closed.
		return nil
	}
	ev.EventID = uuid.New().String()
	ev.At = e.now()
	// The event carries only JSON-native types — Marshal cannot fail.
	body, _ := json.Marshal(ev)
	mac := hmac.New(sha256.New, []byte(cb.Secret))
	mac.Write(body)

	task := &core.DeliveryTask{
		ID:        "cb-" + ev.EventID,
		Provider:  ProviderName,
		Targets:   []string{cb.URL},
		Level:     "info",
		CreatedAt: ev.At,
		Payload: core.DeliveryPayload{
			Kind: core.PayloadRaw,
			Raw: map[string]any{
				"event_id":  ev.EventID,
				"body":      string(body),
				"signature": signaturePrefix + hex.EncodeToString(mac.Sum(nil)),
			},
		},
	}
	if err := e.queue.Push(context.Background(), task); err != nil {
		return fmt.Errorf("callback: enqueue: %w", err)
	}
	return nil
}

// appOf splits an "app:<namespace>" source stamp into its namespace.
// Everything else (bot, preference_center, anonymous) has no app.
func appOf(source string) (string, bool) {
	const prefix = "app:"
	if len(source) <= len(prefix) || source[:len(prefix)] != prefix {
		return "", false
	}
	return source[len(prefix):], true
}
