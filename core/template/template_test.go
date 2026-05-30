package template

import (
	"context"
	"testing"
	"time"
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

func TestManagerList(t *testing.T) {
	manager := NewManager()

	// Register multiple templates
	templates := []*Template{
		{ID: "test1", Name: "Test 1", Title: "Title 1", Fields: []Field{{Label: "f1", Value: "v1"}}},
		{ID: "test2", Name: "Test 2", Title: "Title 2", Fields: []Field{{Label: "f2", Value: "v2"}}},
		{ID: "test3", Name: "Test 3", Title: "Title 3", Fields: []Field{{Label: "f3", Value: "v3"}}},
	}

	for _, tmpl := range templates {
		if err := manager.Register(tmpl); err != nil {
			t.Fatalf("Register() error = %v", err)
		}
	}

	// List all templates
	list := manager.List()
	if len(list) != 3 {
		t.Errorf("List() returned %d templates, want 3", len(list))
	}

	// Verify templates are copies (modifications don't affect original)
	list[0].Name = "Modified"
	tmpl, _ := manager.Get("test1")
	if tmpl.Name == "Modified" {
		t.Error("List() should return copies, modifications should not affect originals")
	}
}

func TestManagerListEmpty(t *testing.T) {
	manager := NewManager()
	list := manager.List()
	if len(list) != 0 {
		t.Errorf("List() should return empty slice for empty manager, got %d items", len(list))
	}
}

func TestManagerDelete(t *testing.T) {
	manager := NewManager()

	tmpl := &Template{
		ID:    "test",
		Name:  "Test",
		Title: "Test Title",
		Fields: []Field{
			{Label: "f1", Value: "v1"},
		},
	}

	if err := manager.Register(tmpl); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	// Verify template exists
	_, err := manager.Get("test")
	if err != nil {
		t.Fatalf("Get() before Delete() error = %v", err)
	}

	// Delete template
	if err := manager.Delete("test"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	// Verify template is gone
	_, err = manager.Get("test")
	if err == nil {
		t.Error("Get() after Delete() should return error")
	}
}

func TestManagerDeleteNotFound(t *testing.T) {
	manager := NewManager()
	err := manager.Delete("nonexistent")
	if err == nil {
		t.Error("Delete() nonexistent template should return error")
	}
}

func TestManagerLoadFromMap(t *testing.T) {
	manager := NewManager()

	templates := map[string]TemplateConfig{
		"alert": {
			Name:  "Alert Template",
			Title: "Alert: {{.title}}",
			Level: "warning",
			Fields: []FieldConfig{
				{Label: "title", Value: "{{.title}}", Type: "text"},
				{Label: "message", Value: "{{.message}}", Type: "text"},
			},
		},
		"info": {
			Name:  "Info Template",
			Title: "Info: {{.text}}",
			Level: "info",
			Fields: []FieldConfig{
				{Label: "text", Value: "{{.text}}", Type: "text"},
			},
		},
	}

	if err := manager.LoadFromMap(templates); err != nil {
		t.Fatalf("LoadFromMap() error = %v", err)
	}

	// Verify templates were loaded
	tmpl, err := manager.Get("alert")
	if err != nil {
		t.Errorf("Get() alert template error = %v", err)
	}
	if tmpl.Name != "Alert Template" {
		t.Errorf("alert template Name = %s, want 'Alert Template'", tmpl.Name)
	}

	tmpl2, err := manager.Get("info")
	if err != nil {
		t.Errorf("Get() info template error = %v", err)
	}
	if tmpl2.Name != "Info Template" {
		t.Errorf("info template Name = %s, want 'Info Template'", tmpl2.Name)
	}
}

func TestManagerLoadFromMapWithBindings(t *testing.T) {
	manager := NewManager()

	templates := map[string]TemplateConfig{
		"sms": {
			Name:  "SMS Template",
			Title: "Verification Code",
			Level: "info",
			Fields: []FieldConfig{
				{Label: "code", Value: "{{.code}}", Type: "text"},
			},
			Bindings: map[string]Binding{
				"aliyun": {
					TemplateCode: "SMS_12345",
					Params:       map[string]string{"code": "code"},
				},
			},
		},
	}

	if err := manager.LoadFromMap(templates); err != nil {
		t.Fatalf("LoadFromMap() error = %v", err)
	}

	tmpl, err := manager.Get("sms")
	if err != nil {
		t.Fatalf("Get() sms template error = %v", err)
	}

	if tmpl.Bindings == nil {
		t.Error("template should have bindings")
	}
	if len(tmpl.Bindings) != 1 {
		t.Errorf("template should have 1 binding, got %d", len(tmpl.Bindings))
	}
}

func TestManagerValidateEmptyID(t *testing.T) {
	manager := NewManager()
	tmpl := &Template{
		ID:    "",
		Name:  "Test",
		Title: "Test Title",
		Fields: []Field{
			{Label: "f1", Value: "v1"},
		},
	}

	err := manager.Register(tmpl)
	if err == nil {
		t.Error("Register() with empty ID should return error")
	}
}

func TestManagerValidateEmptyName(t *testing.T) {
	manager := NewManager()
	tmpl := &Template{
		ID:    "test",
		Name:  "",
		Title: "Test Title",
		Fields: []Field{
			{Label: "f1", Value: "v1"},
		},
	}

	err := manager.Register(tmpl)
	if err == nil {
		t.Error("Register() with empty name should return error")
	}
}

func TestManagerValidateEmptyTitle(t *testing.T) {
	manager := NewManager()
	tmpl := &Template{
		ID:    "test",
		Name:  "Test",
		Title: "",
		Fields: []Field{
			{Label: "f1", Value: "v1"},
		},
	}

	err := manager.Register(tmpl)
	if err == nil {
		t.Error("Register() with empty title should return error")
	}
}

func TestManagerValidateEmptyFieldLabel(t *testing.T) {
	manager := NewManager()
	tmpl := &Template{
		ID:    "test",
		Name:  "Test",
		Title: "Test Title",
		Fields: []Field{
			{Label: "", Value: "v1"},
		},
	}

	err := manager.Register(tmpl)
	if err == nil {
		t.Error("Register() with empty field label should return error")
	}
}

func TestManagerValidateEmptyFieldValue(t *testing.T) {
	manager := NewManager()
	tmpl := &Template{
		ID:    "test",
		Name:  "Test",
		Title: "Test Title",
		Fields: []Field{
			{Label: "f1", Value: ""},
		},
	}

	err := manager.Register(tmpl)
	if err == nil {
		t.Error("Register() with empty field value should return error")
	}
}

func TestManagerRegisterNil(t *testing.T) {
	manager := NewManager()
	err := manager.Register(nil)
	if err == nil {
		t.Error("Register() nil template should return error")
	}
}

func TestManagerGetNotFound(t *testing.T) {
	manager := NewManager()
	_, err := manager.Get("nonexistent")
	if err == nil {
		t.Error("Get() nonexistent template should return error")
	}
}

func TestManagerGetWithBindings(t *testing.T) {
	manager := NewManager()

	tmpl := &Template{
		ID:    "test",
		Name:  "Test",
		Title: "Test Title",
		Fields: []Field{
			{Label: "f1", Value: "v1"},
		},
		Bindings: map[string]Binding{
			"email": {
				Format: "html",
			},
			"slack": {
				Format: "markdown",
			},
		},
	}

	if err := manager.Register(tmpl); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	// Get should return a copy with copied bindings
	retrieved, err := manager.Get("test")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	if retrieved.Bindings == nil {
		t.Error("retrieved template should have bindings")
	}

	// Modify the copy's bindings
	retrieved.Bindings["email"] = Binding{Format: "plain"}

	// Original should be unchanged
	original, _ := manager.Get("test")
	if original.Bindings["email"].Format == "plain" {
		t.Error("Get() should return a deep copy, modifications should not affect original")
	}
}

func TestManagerRenderNotFound(t *testing.T) {
	manager := NewManager()
	_, err := manager.Render("nonexistent", map[string]interface{}{})
	if err == nil {
		t.Error("Render() with nonexistent template should return error")
	}
}

func TestManagerRenderWithMissingParams(t *testing.T) {
	manager := NewManager()

	tmpl := &Template{
		ID:    "test",
		Name:  "Test",
		Title: "Hello {{.Name}}",
		Fields: []Field{
			{Label: "Server", Value: "{{.Server}}"},
		},
	}

	if err := manager.Register(tmpl); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	// Render with missing params - template engine should handle gracefully
	data, err := manager.Render("test", map[string]interface{}{})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	if data.Title == "" {
		t.Error("Render() should still produce title even with missing params")
	}
}

func TestManagerRegisterUpdate(t *testing.T) {
	manager := NewManager()

	tmpl := &Template{
		ID:    "test",
		Name:  "Test",
		Title: "Original Title",
		Fields: []Field{
			{Label: "f1", Value: "v1"},
		},
	}

	if err := manager.Register(tmpl); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	// Get the original CreatedAt
	original, _ := manager.Get("test")
	originalCreatedAt := original.CreatedAt

	// Wait a bit to ensure timestamp would be different
	time.Sleep(10 * time.Millisecond)

	// Update the template
	updated := &Template{
		ID:    "test",
		Name:  "Updated Test",
		Title: "Updated Title",
		Fields: []Field{
			{Label: "f2", Value: "v2"},
		},
	}

	if err := manager.Register(updated); err != nil {
		t.Fatalf("Register() update error = %v", err)
	}

	// Verify CreatedAt is preserved
	retrieved, _ := manager.Get("test")
	if retrieved.CreatedAt != originalCreatedAt {
		t.Error("UpdatedAt should be preserved when updating existing template")
	}

	// Verify other fields are updated
	if retrieved.Title != "Updated Title" {
		t.Errorf("Title should be updated, got %s", retrieved.Title)
	}
}

func TestManagerListWithBindings(t *testing.T) {
	manager := NewManager()

	tmpl := &Template{
		ID:    "test",
		Name:  "Test",
		Title: "Test Title",
		Fields: []Field{
			{Label: "f1", Value: "v1"},
		},
		Bindings: map[string]Binding{
			"email": {Format: "html"},
		},
	}

	if err := manager.Register(tmpl); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	templates := manager.List()
	if len(templates) != 1 {
		t.Fatalf("List() returned %d templates, want 1", len(templates))
	}

	if templates[0].Bindings == nil {
		t.Error("List() should include bindings")
	}

	// Modify returned template
	templates[0].Bindings["slack"] = Binding{Format: "markdown"}

	// Original should be unchanged
	list2 := manager.List()
	if len(list2[0].Bindings) != 1 {
		t.Error("List() should return deep copies")
	}
}
