package service

import (
	"context"
	"errors"
	"testing"

	"github.com/cuihairu/herald/core"
	"github.com/cuihairu/herald/core/template"
)

// stubRenderer is a Renderer that deliberately does NOT implement
// template.StringRenderer, so planner.renderContent has to go through the
// generic Renderer.Render path and type-switch its result.
type stubRenderer struct {
	format template.RenderFormat
	result interface{}
	err    error
}

func (r *stubRenderer) Format() template.RenderFormat { return r.format }

func (r *stubRenderer) Render(ctx context.Context, data *template.RenderedData) (interface{}, error) {
	if r.err != nil {
		return nil, r.err
	}
	return r.result, nil
}

// registerStubRenderer installs a stub renderer in the global registry for the
// duration of the test and restores the previous state afterwards.
func registerStubRenderer(t *testing.T, r template.Renderer) {
	t.Helper()
	format := r.Format()
	prev, existed := template.Registry[format]
	template.Registry[format] = r
	t.Cleanup(func() {
		if existed {
			template.Registry[format] = prev
		} else {
			delete(template.Registry, format)
		}
	})
}

const stubFormat = template.RenderFormat("stubfmt")

func newRenderTestData() *template.RenderedData {
	return &template.RenderedData{
		TemplateID: "tpl-1",
		Title:      "Rendered Title",
		Fields: []template.RenderedField{
			{Label: "field1", Value: "value1"},
		},
	}
}

func TestRenderContentNonStringRendererReturnsString(t *testing.T) {
	registerStubRenderer(t, &stubRenderer{format: stubFormat, result: "rendered body"})

	planner := NewDeliveryPlanner(template.NewManager())
	got, err := planner.renderContent(context.Background(), newRenderTestData(), string(stubFormat))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if got != "rendered body" {
		t.Errorf("expected rendered body from generic renderer, got %q", got)
	}
}

func TestRenderContentNonStringRendererError(t *testing.T) {
	registerStubRenderer(t, &stubRenderer{format: stubFormat, err: errors.New("render failed")})

	planner := NewDeliveryPlanner(template.NewManager())
	_, err := planner.renderContent(context.Background(), newRenderTestData(), string(stubFormat))
	if err == nil || err.Error() != "render failed" {
		t.Errorf("expected renderer error to propagate, got %v", err)
	}
}

func TestRenderContentNonStringRendererNonStringResult(t *testing.T) {
	// A renderer returning a non-string payload yields an empty string body.
	registerStubRenderer(t, &stubRenderer{format: stubFormat, result: map[string]any{"card": true}})

	planner := NewDeliveryPlanner(template.NewManager())
	got, err := planner.renderContent(context.Background(), newRenderTestData(), string(stubFormat))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if got != "" {
		t.Errorf("expected empty body for non-string render result, got %q", got)
	}
}

// plainProvider is a provider that does not implement core.CapableProvider,
// forcing the planner onto its default capability.
type plainProvider struct {
	name string
}

func (p *plainProvider) Deliver(ctx context.Context, task *core.DeliveryTask) error { return nil }

func (p *plainProvider) Name() string { return p.name }

func (p *plainProvider) Type() string { return "plain" }

func (p *plainProvider) Status() *core.ProviderStatus {
	return &core.ProviderStatus{Name: p.name, Type: "plain", Status: "available"}
}

func TestPlanWithNonCapableProvider(t *testing.T) {
	planner := NewDeliveryPlanner(template.NewManager())

	notification := &core.Notification{
		Content: &core.DirectContent{Title: "Hello", Body: "World"},
	}

	task, err := planner.Plan(context.Background(), &plainProvider{name: "legacy"}, notification, nil, nil, "legacy")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if task.Payload.Kind != core.PayloadContent {
		t.Errorf("expected default content payload, got %q", task.Payload.Kind)
	}
	if task.Payload.Content == nil {
		t.Fatal("expected content payload")
	}
	if task.Payload.Content.Format != "plain" {
		t.Errorf("expected default format 'plain', got %q", task.Payload.Content.Format)
	}
	if task.Payload.Content.Title != "Hello" || task.Payload.Content.Body != "World" {
		t.Errorf("unexpected content: %+v", task.Payload.Content)
	}
}

func TestPlanCarriesTheAlertIdentity(t *testing.T) {
	planner := NewDeliveryPlanner(template.NewManager())

	// The caller's business alert id rides through delivery so card
	// buttons can acknowledge the right alert.
	n := &core.Notification{
		Content: &core.DirectContent{Title: "disk full", Body: "b"},
		Params:  map[string]any{"alert_id": "inc-7"},
	}
	task, err := planner.Plan(context.Background(), &plainProvider{name: "p"}, n, nil, nil, "p")
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if task.AlertID != "inc-7" {
		t.Fatalf("task must carry the caller's alert id, got %q", task.AlertID)
	}

	// Without one, the content fingerprint is the identity — the same
	// fallback the ack API, escalation and the ledger use.
	plain := &core.Notification{Content: &core.DirectContent{Title: "t", Body: "b"}}
	task, err = planner.Plan(context.Background(), &plainProvider{name: "p"}, plain, nil, nil, "p")
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if task.AlertID == "" || task.AlertID != alertIDOf(plain) {
		t.Fatalf("task must fall back to the content identity, got %q", task.AlertID)
	}
}
