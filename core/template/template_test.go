package template

import (
	"context"
	"testing"
)

func TestManagerRender(t *testing.T) {
	manager := NewManager()

	// Register a test template
	tmpl := &Template{
		ID:    "test",
		Name:  "Test Template",
		Title: "Hello {{.Name}}",
		Level: "info",
		Fields: []Field{
			{Label: "Server", Value: "{{.Server}}", Type: "text"},
			{Label: "Error", Value: "{{.Error}}", Type: "text"},
		},
	}

	if err := manager.Register(tmpl); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	// Render with parameters
	params := map[string]interface{}{
		"Name":   "World",
		"Server": "server-01",
		"Error":  "Connection timeout",
	}

	data, err := manager.Render("test", params)
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}

	// Verify rendered data
	if data.Title != "Hello World" {
		t.Errorf("Title = %s, want 'Hello World'", data.Title)
	}

	if len(data.Fields) != 2 {
		t.Fatalf("Fields count = %d, want 2", len(data.Fields))
	}

	if data.Fields[0].Label != "Server" {
		t.Errorf("Field[0].Label = %s, want 'Server'", data.Fields[0].Label)
	}

	if data.Fields[0].Value != "server-01" {
		t.Errorf("Field[0].Value = %s, want 'server-01'", data.Fields[0].Value)
	}

	if data.Fields[1].Value != "Connection timeout" {
		t.Errorf("Field[1].Value = %s, want 'Connection timeout'", data.Fields[1].Value)
	}
}

func TestHTMLRenderer(t *testing.T) {
	renderer := &HTMLRenderer{}

	data := &RenderedData{
		Title: "Test Alert",
		Level: "error",
		Fields: []RenderedField{
			{Label: "Server", Value: "server-01", Type: "text"},
			{Label: "Error", Value: "CPU 95%", Type: "text"},
		},
	}

	result, err := renderer.Render(context.Background(), data)
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}

	html, ok := result.(string)
	if !ok {
		t.Fatalf("Render() result is not string")
	}

	// Check for HTML markers
	if !contains(html, "<html>") {
		t.Error("Output should contain <html> tag")
	}

	if !contains(html, "Test Alert") {
		t.Error("Output should contain title")
	}

	if !contains(html, "server-01") {
		t.Error("Output should contain server value")
	}

	// Check for red color (error level)
	if !contains(html, "#f44336") {
		t.Error("Error level should use red color")
	}
}

func TestMarkdownRenderer(t *testing.T) {
	renderer := &MarkdownRenderer{}

	data := &RenderedData{
		Title: "Test Alert",
		Level: "warning",
		Fields: []RenderedField{
			{Label: "Server", Value: "server-01", Type: "text"},
		},
	}

	result, err := renderer.Render(context.Background(), data)
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}

	md, ok := result.(string)
	if !ok {
		t.Fatalf("Render() result is not string")
	}

	// Check for markdown markers
	if !contains(md, "###") {
		t.Error("Output should contain markdown header")
	}

	if !contains(md, "🟡") {
		t.Error("Warning level should use yellow emoji")
	}

	if !contains(md, "**Server**") {
		t.Error("Field label should be bold")
	}
}

func TestJSONRenderer(t *testing.T) {
	renderer := &JSONRenderer{}

	data := &RenderedData{
		Title: "Test Alert",
		Level: "error",
		Fields: []RenderedField{
			{Label: "Server", Value: "server-01", Type: "text"},
			{Label: "Error", Value: "CPU 95%", Type: "text"},
		},
	}

	result, err := renderer.Render(context.Background(), data)
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}

	// Result should be a map (card structure)
	card, ok := result.(map[string]interface{})
	if !ok {
		t.Fatalf("Render() result is not map")
	}

	if card["msg_type"] != "interactive" {
		t.Error("Card should have msg_type=interactive")
	}

	cardData, ok := card["card"].(map[string]interface{})
	if !ok {
		t.Fatal("Card should have card field")
	}

	header, ok := cardData["header"].(map[string]interface{})
	if !ok {
		t.Fatal("Card should have header")
	}

	// Check error level uses red color
	if header["template"] != "red" {
		t.Errorf("Error level should use red color, got %v", header["template"])
	}
}

func TestRendererRegistry(t *testing.T) {
	tests := []struct {
		format   RenderFormat
		wantName string
	}{
		{RenderFormatHTML, "HTMLRenderer"},
		{RenderFormatMarkdown, "MarkdownRenderer"},
		{RenderFormatPlain, "PlainRenderer"},
		{RenderFormatJSON, "JSONRenderer"},
	}

	for _, tt := range tests {
		t.Run(string(tt.format), func(t *testing.T) {
			renderer, ok := GetRenderer(tt.format)
			if !ok {
				t.Errorf("GetRenderer(%s) returned false", tt.format)
				return
			}

			if renderer.Format() != tt.format {
				t.Errorf("Renderer.Format() = %s, want %s", renderer.Format(), tt.format)
			}
		})
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && indexOf(s, substr) >= 0
}

func indexOf(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
