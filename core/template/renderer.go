package template

import (
	"context"
	"fmt"
)

// Renderer is the interface for rendering templates to channel-specific formats
// Each Provider implements its own renderer to adapt templates to its platform format
type Renderer interface {
	// Render renders the template with the given parameters into a channel-specific format
	Render(ctx context.Context, tmpl *Template, params map[string]interface{}) (string, error)
}

// RendererFunc is a function type that implements Renderer
type RendererFunc func(ctx context.Context, tmpl *Template, params map[string]interface{}) (string, error)

// Render implements the Renderer interface
func (f RendererFunc) Render(ctx context.Context, tmpl *Template, params map[string]interface{}) (string, error) {
	return f(ctx, tmpl, params)
}

// TextRenderer renders templates as plain text
type TextRenderer struct {
	engine *Engine
}

// NewTextRenderer creates a new text renderer
func NewTextRenderer(engine *Engine) *TextRenderer {
	return &TextRenderer{engine: engine}
}

// Render renders the template as plain text
func (r *TextRenderer) Render(ctx context.Context, tmpl *Template, params map[string]interface{}) (string, error) {
	// Render title
	title, err := r.engine.Execute(tmpl.Title, params)
	if err != nil {
		return "", fmt.Errorf("failed to render title: %w", err)
	}

	result := title + "\n"

	// Render fields
	for _, field := range tmpl.Fields {
		value, err := r.engine.Execute(field.Value, params)
		if err != nil {
			return "", fmt.Errorf("failed to render field %s: %w", field.Label, err)
		}
		result += fmt.Sprintf("%s: %s\n", field.Label, value)
	}

	return result, nil
}

// MarkdownRenderer renders templates as Markdown
type MarkdownRenderer struct {
	engine *Engine
}

// NewMarkdownRenderer creates a new Markdown renderer
func NewMarkdownRenderer(engine *Engine) *MarkdownRenderer {
	return &MarkdownRenderer{engine: engine}
}

// Render renders the template as Markdown
func (r *MarkdownRenderer) Render(ctx context.Context, tmpl *Template, params map[string]interface{}) (string, error) {
	// Render title with level indicator
	title, err := r.engine.Execute(tmpl.Title, params)
	if err != nil {
		return "", fmt.Errorf("failed to render title: %w", err)
	}

	levelIcon := ""
	switch tmpl.Level {
	case "error":
		levelIcon = "🔴 "
	case "warning":
		levelIcon = "🟡 "
	case "info":
		levelIcon = "🔵 "
	}

	result := "### " + levelIcon + title + "\n\n"

	// Render fields
	for _, field := range tmpl.Fields {
		value, err := r.engine.Execute(field.Value, params)
		if err != nil {
			return "", fmt.Errorf("failed to render field %s: %w", field.Label, err)
		}

		switch field.Type {
		case FieldTypeLink:
			result += fmt.Sprintf("**%s**: [%s](%s)\n", field.Label, value, value)
		default:
			result += fmt.Sprintf("**%s**: `%s`\n", field.Label, value)
		}
	}

	return result, nil
}
