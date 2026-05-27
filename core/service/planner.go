package service

import (
	"context"
	"fmt"
	"time"

	"github.com/cuihairu/herald/core"
	"github.com/cuihairu/herald/core/template"
	"github.com/google/uuid"
)

// DeliveryPlanner generates DeliveryTasks based on provider capability
type DeliveryPlanner struct {
	templates *template.Manager
}

// NewDeliveryPlanner creates a new DeliveryPlanner
func NewDeliveryPlanner(templates *template.Manager) *DeliveryPlanner {
	return &DeliveryPlanner{templates: templates}
}

// Plan creates a DeliveryTask for a specific provider
func (p *DeliveryPlanner) Plan(
	ctx context.Context,
	provider core.Provider,
	notification *core.Notification,
	renderedData *template.RenderedData,
	targets []string,
	channel string,
) (*core.DeliveryTask, error) {
	capability := p.getCapability(provider)

	payload, err := p.buildPayload(ctx, provider, notification, renderedData, capability, channel)
	if err != nil {
		return nil, err
	}

	return &core.DeliveryTask{
		ID:        uuid.New().String(),
		Provider:  channel,
		Targets:   targets,
		Payload:   *payload,
		Level:     notification.Level,
		CreatedAt: time.Now(),
	}, nil
}

// buildPayload constructs the DeliveryPayload based on provider capability
func (p *DeliveryPlanner) buildPayload(
	ctx context.Context,
	provider core.Provider,
	notification *core.Notification,
	renderedData *template.RenderedData,
	cap core.ProviderCapability,
	channel string,
) (*core.DeliveryPayload, error) {
	// Determine title/body
	title, body := resolveContent(notification, renderedData)

	// Check if provider supports vendor template
	if cap.SupportsTemplate && hasKind(cap.PayloadKinds, core.PayloadProviderTemplate) {
		pt := p.buildProviderTemplate(notification, renderedData, channel)
		if pt != nil {
			return &core.DeliveryPayload{
				Kind:             core.PayloadProviderTemplate,
				ProviderTemplate: pt,
			}, nil
		}
	}

	// Content-based delivery
	if hasKind(cap.PayloadKinds, core.PayloadContent) {
		format := p.selectFormat(cap.ContentFormats, notification)
		return &core.DeliveryPayload{
			Kind: core.PayloadContent,
			Content: &core.RenderedContent{
				Title:  title,
				Body:   body,
				Format: format,
			},
		}, nil
	}

	// Raw delivery fallback
	if hasKind(cap.PayloadKinds, core.PayloadRaw) {
		return &core.DeliveryPayload{
			Kind: core.PayloadRaw,
			Raw: map[string]any{
				"title":  title,
				"body":   body,
				"level":  notification.Level,
				"params": notification.Params,
			},
		}, nil
	}

	return nil, fmt.Errorf("provider %s has no compatible payload kind", provider.Name())
}

// buildProviderTemplate builds a ProviderTemplatePayload from notification + template binding
func (p *DeliveryPlanner) buildProviderTemplate(
	notification *core.Notification,
	renderedData *template.RenderedData,
	channel string,
) *core.ProviderTemplatePayload {
	// TODO: P1 - resolve from template bindings config
	// For now, only build if notification.Params has template info
	params := notification.Params
	if params == nil {
		return nil
	}

	templateCode, _ := params["_template_code"].(string)
	templateID, _ := params["_template_id"].(string)

	// Extract template params (everything except internal fields)
	templateParams := make(map[string]string)
	for k, v := range params {
		if k[0] != '_' {
			if s, ok := v.(string); ok {
				templateParams[k] = s
			}
		}
	}

	if templateCode == "" && templateID == "" {
		return nil
	}

	return &core.ProviderTemplatePayload{
		TemplateCode: templateCode,
		TemplateID:   templateID,
		Params:       templateParams,
	}
}

// selectFormat picks the best content format for the provider
func (p *DeliveryPlanner) selectFormat(supported []string, notification *core.Notification) string {
	if len(supported) == 0 {
		return "plain"
	}
	// Default to first supported format
	return supported[0]
}

// getCapability returns the provider capability
func (p *DeliveryPlanner) getCapability(provider core.Provider) core.ProviderCapability {
	if cp, ok := provider.(core.CapableProvider); ok {
		return cp.Capability()
	}
	// Default fallback
	return core.ProviderCapability{
		PayloadKinds:   []core.PayloadKind{core.PayloadContent},
		ContentFormats: []string{"plain"},
	}
}

// resolveContent gets title and body from notification or rendered template
func resolveContent(notification *core.Notification, renderedData *template.RenderedData) (string, string) {
	var title, body string

	if renderedData != nil {
		title = renderedData.Title
		body = renderFieldsBody(renderedData)
	} else if notification.Content != nil {
		title = notification.Content.Title
		body = notification.Content.Body
	}

	return title, body
}

// renderFieldsBody generates a text body from template fields
func renderFieldsBody(data *template.RenderedData) string {
	if len(data.Fields) == 0 {
		return ""
	}
	body := ""
	for _, f := range data.Fields {
		body += f.Label + ": " + f.Value + "\n"
	}
	return body
}

// hasKind checks if a payload kind is in the list
func hasKind(kinds []core.PayloadKind, target core.PayloadKind) bool {
	for _, k := range kinds {
		if k == target {
			return true
		}
	}
	return false
}
