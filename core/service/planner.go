package service

import (
	"context"
	"fmt"
	"time"

	"github.com/cuihairu/herald/core"
	"github.com/cuihairu/herald/core/template"
	"github.com/google/uuid"
)

// DeliveryPlanner generates DeliveryTasks based on provider capability and template bindings
type DeliveryPlanner struct {
	templates *template.Manager
}

// NewDeliveryPlanner creates a new DeliveryPlanner
func NewDeliveryPlanner(templates *template.Manager) *DeliveryPlanner {
	return &DeliveryPlanner{templates: templates}
}

// Plan creates a DeliveryTask for a specific provider/channel
func (p *DeliveryPlanner) Plan(
	ctx context.Context,
	provider core.Provider,
	notification *core.Notification,
	renderedData *template.RenderedData,
	targets []string,
	channel string,
) (*core.DeliveryTask, error) {
	capability := p.getCapability(provider)

	// Resolve binding for this channel
	var binding *template.Binding
	if renderedData != nil {
		binding = p.resolveBinding(renderedData.TemplateID, channel)
	}

	payload, err := p.buildPayload(ctx, notification, renderedData, capability, channel, binding)
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

// resolveBinding looks up the template binding for a specific channel
func (p *DeliveryPlanner) resolveBinding(templateID, channel string) *template.Binding {
	if p.templates == nil || templateID == "" {
		return nil
	}
	tmpl, err := p.templates.Get(templateID)
	if err != nil {
		return nil
	}
	if tmpl.Bindings == nil {
		return nil
	}
	b, ok := tmpl.Bindings[channel]
	if !ok {
		return nil
	}
	return &b
}

// buildPayload constructs the DeliveryPayload based on capability + binding
func (p *DeliveryPlanner) buildPayload(
	ctx context.Context,
	notification *core.Notification,
	renderedData *template.RenderedData,
	cap core.ProviderCapability,
	channel string,
	binding *template.Binding,
) (*core.DeliveryPayload, error) {
	// 1. If binding specifies a vendor template (SMS), build provider_template payload
	if binding != nil && (binding.TemplateCode != "" || binding.TemplateID != "") {
		pt := p.buildProviderTemplate(notification, renderedData, binding)
		return &core.DeliveryPayload{
			Kind:             core.PayloadProviderTemplate,
			ProviderTemplate: pt,
		}, nil
	}

	// 2. If provider supports template natively and binding provides template info
	if cap.SupportsTemplate && hasKind(cap.PayloadKinds, core.PayloadProviderTemplate) && binding != nil {
		if binding.TemplateCode != "" || binding.TemplateID != "" {
			pt := p.buildProviderTemplate(notification, renderedData, binding)
			return &core.DeliveryPayload{
				Kind:             core.PayloadProviderTemplate,
				ProviderTemplate: pt,
			}, nil
		}
	}

	// 3. Content-based delivery (email, IM, etc.)
	if hasKind(cap.PayloadKinds, core.PayloadContent) {
		title, body := resolveContent(notification, renderedData)
		format := p.selectFormat(cap.ContentFormats, notification, binding)

		// Use renderer if we have rendered template data
		if renderedData != nil {
			if rendered, err := p.renderContent(ctx, renderedData, format); err == nil && rendered != "" {
				body = rendered
			}
		}

		return &core.DeliveryPayload{
			Kind: core.PayloadContent,
			Content: &core.RenderedContent{
				Title:  title,
				Body:   body,
				Format: format,
			},
		}, nil
	}

	// 4. Raw delivery fallback
	if hasKind(cap.PayloadKinds, core.PayloadRaw) {
		title, body := resolveContent(notification, renderedData)
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

	return nil, fmt.Errorf("provider %s has no compatible payload kind", channel)
}

// buildProviderTemplate builds a ProviderTemplatePayload from binding config
func (p *DeliveryPlanner) buildProviderTemplate(
	notification *core.Notification,
	renderedData *template.RenderedData,
	binding *template.Binding,
) *core.ProviderTemplatePayload {
	pt := &core.ProviderTemplatePayload{
		TemplateCode: binding.TemplateCode,
		TemplateID:   binding.TemplateID,
	}

	// Build field value map from rendered data
	fieldValues := p.buildFieldValues(renderedData, notification)

	switch {
	// Named params (aliyun): map[string]string
	case len(binding.Params) > 0:
		params := make(map[string]string, len(binding.Params))
		for fieldLabel, vendorKey := range binding.Params {
			if v, ok := fieldValues[fieldLabel]; ok {
				params[vendorKey] = v
			}
		}
		pt.Params = params

	// Ordered params (tencent/netease): []string
	case len(binding.ParamOrder) > 0:
		params := make([]string, 0, len(binding.ParamOrder))
		for _, fieldLabel := range binding.ParamOrder {
			if v, ok := fieldValues[fieldLabel]; ok {
				params = append(params, v)
			}
		}
		pt.Params = params

	default:
		// Fallback: pass all params as map[string]string
		params := make(map[string]string, len(fieldValues))
		for k, v := range fieldValues {
			params[k] = v
		}
		pt.Params = params
	}

	return pt
}

// buildFieldValues extracts field label→value map from rendered data or notification params
func (p *DeliveryPlanner) buildFieldValues(renderedData *template.RenderedData, notification *core.Notification) map[string]string {
	values := make(map[string]string)

	if renderedData != nil {
		for _, f := range renderedData.Fields {
			values[f.Label] = f.Value
		}
	}

	// Also include raw params (for fields not in template)
	if notification.Params != nil {
		for k, v := range notification.Params {
			if s, ok := v.(string); ok {
				if _, exists := values[k]; !exists {
					values[k] = s
				}
			}
		}
	}

	return values
}

// renderContent renders template data through the appropriate renderer
func (p *DeliveryPlanner) renderContent(ctx context.Context, data *template.RenderedData, format string) (string, error) {
	renderer, ok := template.GetRenderer(template.RenderFormat(format))
	if !ok {
		// No renderer for this format, fall back to plain text join
		return renderFieldsBody(data), nil
	}

	if sr, ok := renderer.(template.StringRenderer); ok {
		return sr.RenderString(ctx, data)
	}

	result, err := renderer.Render(ctx, data)
	if err != nil {
		return "", err
	}

	if s, ok := result.(string); ok {
		return s, nil
	}
	return "", nil
}

// selectFormat picks the best content format for the provider
func (p *DeliveryPlanner) selectFormat(supported []string, notification *core.Notification, binding *template.Binding) string {
	// Binding takes priority
	if binding != nil && binding.Format != "" {
		return binding.Format
	}

	if len(supported) == 0 {
		return "plain"
	}
	return supported[0]
}

// getCapability returns the provider capability
func (p *DeliveryPlanner) getCapability(provider core.Provider) core.ProviderCapability {
	if cp, ok := provider.(core.CapableProvider); ok {
		return cp.Capability()
	}
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

// renderFieldsBody generates a plain text body from template fields (fallback)
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
