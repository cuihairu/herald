package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/cuihairu/herald/core"
	"github.com/cuihairu/herald/core/audit"
	"github.com/cuihairu/herald/core/dedup"
	"github.com/cuihairu/herald/core/route"
	"github.com/cuihairu/herald/core/template"
)

// gateTestService builds a service over one email provider with the
// given dedup window and an attached audit trail — the §11 gate needs
// the full pipeline minus the noise.
func gateTestService(t *testing.T, window time.Duration) (*NotificationService, *audit.Store) {
	t.Helper()
	templates := template.NewManager()
	router := route.NewRouter(&route.Config{})
	runtime := newMockProviderRuntime()
	dedupMgr := dedup.NewDedup(&dedup.Config{Window: window})
	queue := newMockQueue()
	provider := &mockProvider{
		providerType: "email",
		capability: core.ProviderCapability{
			PayloadKinds:   []core.PayloadKind{core.PayloadContent},
			ContentFormats: []string{"plain"},
		},
	}
	runtime.RegisterProvider("email", provider, true)

	trail := audit.New(0)
	service := NewNotificationService(templates, router, runtime, dedupMgr, queue)
	service.SetFoldAudit(trail)
	return service, trail
}

// TestProcessEventIdempotency: the §11.1 event layer — a repeat
// submission of the same event_id is dropped even when the content
// drifted, and the audit row names the layer.
func TestProcessEventIdempotency(t *testing.T) {
	service, trail := gateTestService(t, time.Minute)

	first := &core.Notification{
		ID:       "n-1",
		Channels: []string{"email"},
		Type:     "alerts",
		EventID:  "evt-20261007-001",
		Content:  &core.DirectContent{Title: "Alert", Body: "v1"},
	}
	res, err := service.Process(context.Background(), first)
	if err != nil {
		t.Fatalf("first process: %v", err)
	}
	if len(res.TaskIDs) != 1 {
		t.Fatalf("first delivery = %d tasks, want 1", len(res.TaskIDs))
	}

	// Same event id, drifted content: dropped with the layer named.
	repeat := &core.Notification{
		ID:       "n-2",
		Channels: []string{"email"},
		Type:     "alerts",
		EventID:  "evt-20261007-001",
		Content:  &core.DirectContent{Title: "Alert", Body: "v2-drifted"},
	}
	res, err = service.Process(context.Background(), repeat)
	if err != nil {
		t.Fatalf("repeat process: %v", err)
	}
	if len(res.TaskIDs) != 0 {
		t.Fatalf("repeat delivery = %d tasks, want 0", len(res.TaskIDs))
	}
	rows := trail.List()
	if len(rows) != 1 {
		t.Fatalf("audit rows = %d, want 1", len(rows))
	}
	if !strings.HasPrefix(rows[0].Detail, dedup.ReasonIdempotent+": ") {
		t.Errorf("Detail = %q, want %q prefix", rows[0].Detail, dedup.ReasonIdempotent+": ")
	}
}

// TestProcessStateMachine: state-type alerts deliver on the first
// sighting and on every flip; repeats of the current state fold.
func TestProcessStateMachine(t *testing.T) {
	service, trail := gateTestService(t, time.Minute)

	mk := func(id, state string) *core.Notification {
		return &core.Notification{
			ID:       id,
			Channels: []string{"email"},
			Type:     "alerts",
			DedupKey: "node-17",
			State:    state,
			Content:  &core.DirectContent{Title: "Node 17", Body: state},
		}
	}
	count := func(n *core.Notification) int {
		res, err := service.Process(context.Background(), n)
		if err != nil {
			t.Fatalf("%s process: %v", n.State, err)
		}
		return len(res.TaskIDs)
	}
	if got := count(mk("n-1", "down")); got != 1 {
		t.Errorf("first down = %d tasks, want 1", got)
	}
	if got := count(mk("n-2", "down")); got != 0 {
		t.Errorf("repeat down = %d tasks, want 0", got)
	}
	if got := count(mk("n-3", "ok")); got != 1 {
		t.Errorf("recovery = %d tasks, want 1", got)
	}
	if got := count(mk("n-4", "down")); got != 1 {
		t.Errorf("down again = %d tasks, want 1", got)
	}
	// Exactly one fold row: the single repeat; the flips delivered.
	rows := trail.List()
	if len(rows) != 1 || !strings.HasPrefix(rows[0].Detail, dedup.ReasonStateRepeat+": ") {
		t.Errorf("audit rows = %d, first Detail = %q, want one %s row",
			len(rows), detailOr(rows), dedup.ReasonStateRepeat)
	}
}

// TestProcessExplicitDedupKey: an explicit dedup_key groups content that
// would otherwise hash differently.
func TestProcessExplicitDedupKey(t *testing.T) {
	service, _ := gateTestService(t, time.Minute)

	mk := func(id, body string) *core.Notification {
		return &core.Notification{
			ID:       id,
			Channels: []string{"email"},
			Type:     "alerts",
			DedupKey: "deploy-9",
			Content:  &core.DirectContent{Title: "Deploy 9", Body: body},
		}
	}
	res, err := service.Process(context.Background(), mk("n-1", "step 1"))
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	if len(res.TaskIDs) != 1 {
		t.Fatalf("first = %d tasks, want 1", len(res.TaskIDs))
	}
	res, err = service.Process(context.Background(), mk("n-2", "step 2 — different content"))
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if len(res.TaskIDs) != 0 {
		t.Fatalf("same key = %d tasks, want 0", len(res.TaskIDs))
	}
}

// TestProcessSystemCategoryOnceTier: the §11.2 default table grades
// "system" once — a repeat folds even though the fold window is still
// wide open, and widening back is not a thing the default can do.
func TestProcessSystemCategoryOnceTier(t *testing.T) {
	service, trail := gateTestService(t, time.Minute)

	mk := func(id string) *core.Notification {
		return &core.Notification{
			ID:       id,
			Channels: []string{"email"},
			Type:     "system",
			Content:  &core.DirectContent{Title: "Deployed", Body: "rev 42"},
		}
	}
	if res, err := service.Process(context.Background(), mk("n-1")); err != nil || len(res.TaskIDs) != 1 {
		t.Fatalf("first system event = %d tasks, %v; want 1", len(res.TaskIDs), err)
	}
	if res, err := service.Process(context.Background(), mk("n-2")); err != nil || len(res.TaskIDs) != 0 {
		t.Fatalf("repeat system event = %d tasks, %v; want 0", len(res.TaskIDs), err)
	}
	if rows := trail.List(); len(rows) != 1 || !strings.HasPrefix(rows[0].Detail, dedup.ReasonOnce+": ") {
		t.Errorf("audit rows = %d, want one %s row", len(rows), dedup.ReasonOnce)
	}
}

func detailOr(rows []audit.Event) string {
	if len(rows) == 0 {
		return "<none>"
	}
	return rows[0].Detail
}
