package feishu

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cuihairu/herald/core/template"
)

// Render renders a template as Feishu card message
func (p *Provider) Render(ctx context.Context, tmpl *template.Template, params map[string]interface{}) (string, error) {
	engine := template.NewEngine()

	// Render title
	title, err := engine.Execute(tmpl.Title, params)
	if err != nil {
		return "", fmt.Errorf("failed to render title: %w", err)
	}

	// Build card structure
	card := make(map[string]interface{})

	// Determine card color based on level
	color := "blue"
	switch tmpl.Level {
	case "error":
		color = "red"
	case "warning":
		color = "orange"
	case "info":
		color = "blue"
	}

	// Build header
	header := make(map[string]interface{})
	header["title"] = map[string]interface{}{
		"content": title,
		"tag":     "plain_text",
	}
	if color != "" {
		header["template"] = color
	}

	// Build elements (fields)
	elements := make([]interface{}, 0)

	// Add fields as table/div
	for _, field := range tmpl.Fields {
		value, err := engine.Execute(field.Value, params)
		if err != nil {
			return "", fmt.Errorf("failed to render field %s: %w", field.Label, err)
		}

		// Use div with fields for better layout
		divElement := map[string]interface{}{
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
						"content": value,
					},
				},
			},
		}
		elements = append(elements, divElement)
	}

	card["msg_type"] = "interactive"
	card["card"] = map[string]interface{}{
		"header":   header,
		"elements": elements,
	}

	// Marshal to JSON
	jsonData, err := json.Marshal(card)
	if err != nil {
		return "", fmt.Errorf("failed to marshal card: %w", err)
	}

	return string(jsonData), nil
}
