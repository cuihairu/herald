package email

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cuihairu/herald/core/template"
)

// Render renders a template as HTML email
func (p *Provider) Render(ctx context.Context, tmpl *template.Template, params map[string]interface{}) (string, error) {
	engine := template.NewEngine()

	// Render title
	title, err := engine.Execute(tmpl.Title, params)
	if err != nil {
		return "", fmt.Errorf("failed to render title: %w", err)
	}

	var html strings.Builder

	// Build HTML email
	html.WriteString("<!DOCTYPE html><html><head>")
	html.WriteString("<meta charset='UTF-8'>")
	html.WriteString("<style>")
	html.WriteString("body { font-family: Arial, sans-serif; font-size: 14px; line-height: 1.6; color: #333; }")
	html.WriteString(".header { background-color: #f44336; color: white; padding: 15px; text-align: center; }")
	html.WriteString(".header.warning { background-color: #ff9800; }")
	html.WriteString(".header.info { background-color: #2196F3; }")
	html.WriteString(".content { padding: 20px; }")
	html.WriteString(".field { margin: 10px 0; }")
	html.WriteString(".label { font-weight: bold; color: #666; }")
	html.WriteString(".value { margin-top: 5px; }")
	html.WriteString(".footer { padding: 15px; text-align: center; color: #999; font-size: 12px; }")
	html.WriteString("</style></head><body>")

	// Header with level-based color
	headerClass := ""
	switch tmpl.Level {
	case "error":
		headerClass = "error"
	case "warning":
		headerClass = "warning"
	case "info":
		headerClass = "info"
	}

	if headerClass != "" {
		html.WriteString(fmt.Sprintf("<div class='header %s'>%s</div>", headerClass, title))
	} else {
		html.WriteString(fmt.Sprintf("<div class='header'>%s</div>", title))
	}

	// Content
	html.WriteString("<div class='content'>")

	// Render fields
	for _, field := range tmpl.Fields {
		value, err := engine.Execute(field.Value, params)
		if err != nil {
			return "", fmt.Errorf("failed to render field %s: %w", field.Label, err)
		}

		html.WriteString("<div class='field'>")
		html.WriteString(fmt.Sprintf("<div class='label'>%s:</div>", field.Label))
		html.WriteString(fmt.Sprintf("<div class='value'>%s</div>", value))
		html.WriteString("</div>")
	}

	html.WriteString("</div>")

	// Footer with timestamp
	html.WriteString("<div class='footer'>")
	html.WriteString(fmt.Sprintf("Time: %s", time.Now().Format("2006-01-02 15:04:05")))
	html.WriteString("</div>")

	html.WriteString("</body></html>")

	return html.String(), nil
}
