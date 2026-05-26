package template

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// Renderer renders channel-agnostic data into specific formats
// Renderers are independent of Providers
type Renderer interface {
	// Format returns the format this renderer produces
	Format() RenderFormat

	// Render renders the data into the specific format
	// Returns the rendered content (string) or data structure
	Render(ctx context.Context, data *RenderedData) (interface{}, error)
}

// StringRenderer is a renderer that produces string output
type StringRenderer interface {
	Renderer
	RenderString(ctx context.Context, data *RenderedData) (string, error)
}

// HTMLRenderer renders data as HTML
type HTMLRenderer struct{}

// Format returns the format
func (r *HTMLRenderer) Format() RenderFormat {
	return RenderFormatHTML
}

// Render renders as HTML string
func (r *HTMLRenderer) Render(ctx context.Context, data *RenderedData) (interface{}, error) {
	return r.RenderString(ctx, data)
}

// RenderString renders as HTML
func (r *HTMLRenderer) RenderString(ctx context.Context, data *RenderedData) (string, error) {
	var buf bytes.Buffer

	// Determine header color based on level
	headerColor := "#2196F3" // default blue
	switch data.Level {
	case "error":
		headerColor = "#f44336" // red
	case "warning":
		headerColor = "#ff9800" // orange
	case "info":
		headerColor = "#2196F3" // blue
	}

	buf.WriteString("<!DOCTYPE html><html><head>")
	buf.WriteString("<meta charset='UTF-8'>")
	buf.WriteString("<style>")
	buf.WriteString("body{font-family:Arial,sans-serif;font-size:14px;line-height:1.6;color:#333}")
	buf.WriteString(".header{background-color:" + headerColor + ";color:white;padding:15px;text-align:center}")
	buf.WriteString(".content{padding:20px}")
	buf.WriteString(".field{margin:10px 0}")
	buf.WriteString(".label{font-weight:bold;color:#666}")
	buf.WriteString(".value{margin-top:5px}")
	buf.WriteString("</style></head><body>")
	fmt.Fprintf(&buf, "<div class='header'>%s</div>", escapeHTML(data.Title))
	buf.WriteString("<div class='content'>")

	for _, field := range data.Fields {
		buf.WriteString("<div class='field'>")
		fmt.Fprintf(&buf, "<div class='label'>%s:</div>", escapeHTML(field.Label))
		fmt.Fprintf(&buf, "<div class='value'>%s</div>", escapeHTML(field.Value))
		buf.WriteString("</div>")
	}

	buf.WriteString("</div></body></html>")
	return buf.String(), nil
}

// MarkdownRenderer renders data as Markdown
type MarkdownRenderer struct{}

// Format returns the format
func (r *MarkdownRenderer) Format() RenderFormat {
	return RenderFormatMarkdown
}

// Render renders as Markdown string
func (r *MarkdownRenderer) Render(ctx context.Context, data *RenderedData) (interface{}, error) {
	return r.RenderString(ctx, data)
}

// RenderString renders as Markdown
func (r *MarkdownRenderer) RenderString(ctx context.Context, data *RenderedData) (string, error) {
	var buf bytes.Buffer

	// Level indicator
	icon := ""
	switch data.Level {
	case "error":
		icon = "🔴 "
	case "warning":
		icon = "🟡 "
	case "info":
		icon = "🔵 "
	}

	// Title
	buf.WriteString(fmt.Sprintf("### %s%s\n\n", icon, data.Title))

	// Fields
	for _, field := range data.Fields {
		if field.Type == string(FieldTypeLink) {
			buf.WriteString(fmt.Sprintf("**%s**: [%s](%s)\n", field.Label, field.Value, field.Value))
		} else {
			buf.WriteString(fmt.Sprintf("**%s**: `%s`\n", field.Label, field.Value))
		}
	}

	return buf.String(), nil
}

// PlainRenderer renders data as plain text
type PlainRenderer struct{}

// Format returns the format
func (r *PlainRenderer) Format() RenderFormat {
	return RenderFormatPlain
}

// Render renders as plain text string
func (r *PlainRenderer) Render(ctx context.Context, data *RenderedData) (interface{}, error) {
	return r.RenderString(ctx, data)
}

// RenderString renders as plain text
func (r *PlainRenderer) RenderString(ctx context.Context, data *RenderedData) (string, error) {
	var buf bytes.Buffer

	buf.WriteString(data.Title)
	buf.WriteString("\n")
	for _, field := range data.Fields {
		buf.WriteString(fmt.Sprintf("%s: %s\n", field.Label, field.Value))
	}

	return buf.String(), nil
}

// JSONRenderer renders data as JSON (for card-based platforms like Feishu)
type JSONRenderer struct{}

// Format returns the format
func (r *JSONRenderer) Format() RenderFormat {
	return RenderFormatJSON
}

// Render renders as JSON
func (r *JSONRenderer) Render(ctx context.Context, data *RenderedData) (interface{}, error) {
	// Build a card structure compatible with Feishu/DingTalk
	card := map[string]interface{}{
		"msg_type": "interactive",
		"card": r.buildCard(data),
	}
	return card, nil
}

// buildCard builds the card structure
func (r *JSONRenderer) buildCard(data *RenderedData) map[string]interface{} {
	// Header color based on level
	color := "blue"
	switch data.Level {
	case "error":
		color = "red"
	case "warning":
		color = "orange"
	}

	card := map[string]interface{}{
		"header": map[string]interface{}{
			"title": map[string]interface{}{
				"content": data.Title,
				"tag":     "plain_text",
			},
			"template": color,
		},
	}

	// Build elements from fields
	elements := make([]interface{}, 0)
	for _, field := range data.Fields {
		element := map[string]interface{}{
			"tag": "div",
			"fields": []map[string]interface{}{
				{
					"is_short": true,
					"text": map[string]interface{}{
						"tag":     "plain_text",
						"content": field.Label,
					},
				},
				{
					"is_short": true,
					"text": map[string]interface{}{
						"tag":     "plain_text",
						"content": field.Value,
					},
				},
			},
		}
		elements = append(elements, element)
	}
	card["elements"] = elements

	return card
}

// ToJSON converts the rendered JSON to string
func (r *JSONRenderer) ToJSON(data interface{}) (string, error) {
	bytes, err := json.Marshal(data)
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}

// Registry holds available renderers
var Registry = map[RenderFormat]Renderer{
	RenderFormatHTML:     &HTMLRenderer{},
	RenderFormatMarkdown: &MarkdownRenderer{},
	RenderFormatPlain:    &PlainRenderer{},
	RenderFormatJSON:     &JSONRenderer{},
}

// GetRenderer returns a renderer for the given format
func GetRenderer(format RenderFormat) (Renderer, bool) {
	r, ok := Registry[format]
	return r, ok
}

// Helper functions

func escapeHTML(text string) string {
	// Basic HTML escaping
	replacer := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		"\"", "&quot;",
		"'", "&#39;",
	)
	return replacer.Replace(text)
}
