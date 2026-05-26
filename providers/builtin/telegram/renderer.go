package telegram

import (
	"context"
	"fmt"
	"strings"

	"github.com/cuihairu/herald/core/template"
)

// Render renders a template as Telegram Markdown message
func (p *Provider) Render(ctx context.Context, tmpl *template.Template, params map[string]interface{}) (string, error) {
	engine := template.NewEngine()

	// Render title
	title, err := engine.Execute(tmpl.Title, params)
	if err != nil {
		return "", fmt.Errorf("failed to render title: %w", err)
	}

	var msg strings.Builder

	// Add level emoji
	switch tmpl.Level {
	case "error":
		msg.WriteString("🔴 ")
	case "warning":
		msg.WriteString("🟡 ")
	case "info":
		msg.WriteString("🔵 ")
	}

	// Add title as bold
	msg.WriteString("*" + escapeMarkdown(title) + "*\n\n")

	// Add fields
	for _, field := range tmpl.Fields {
		value, err := engine.Execute(field.Value, params)
		if err != nil {
			return "", fmt.Errorf("failed to render field %s: %w", field.Label, err)
		}

		// Format based on field type
		switch field.Type {
		case template.FieldTypeLink:
			msg.WriteString(fmt.Sprintf("[%s](%s)\n", escapeMarkdown(field.Label), escapeMarkdown(value)))
		default:
			msg.WriteString(fmt.Sprintf("%s: `%s`\n", escapeMarkdown(field.Label), escapeMarkdown(value)))
		}
	}

	return msg.String(), nil
}

// escapeMarkdown escapes special Markdown characters
func escapeMarkdown(text string) string {
	// Escape MarkdownV2 special characters: _ * [ ] ( ) ~ ` > # + - = | { } . !
	specialChars := []string{"_", "*", "[", "]", "(", ")", "~", "`", ">", "#", "+", "-", "=", "|", "{", "}", ".", "!"}

	result := text
	for _, char := range specialChars {
		result = strings.ReplaceAll(result, char, "\\"+char)
	}

	return result
}
