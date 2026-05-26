package template

import (
	"context"
	"testing"
)

func TestTemplateValidation(t *testing.T) {
	tests := []struct {
		name    string
		tmpl    *Template
		wantErr bool
	}{
		{
			name: "valid template",
			tmpl: &Template{
				ID:    "test",
				Name:  "Test Template",
				Title: "Test {{.Var}}",
				Fields: []Field{
					{Label: "Label1", Value: "{{.Value1}}", Type: FieldTypeText},
				},
			},
			wantErr: false,
		},
		{
			name: "empty id",
			tmpl: &Template{
				Name:  "Test",
				Title: "Test",
			},
			wantErr: true,
		},
		{
			name: "empty name",
			tmpl: &Template{
				ID:    "test",
				Title: "Test",
			},
			wantErr: true,
		},
		{
			name: "empty title",
			tmpl: &Template{
				ID:   "test",
				Name: "Test",
			},
			wantErr: true,
		},
		{
			name: "empty field label",
			tmpl: &Template{
				ID:    "test",
				Name:  "Test",
				Title: "Test",
				Fields: []Field{
					{Label: "", Value: "{{.Value}}"},
				},
			},
			wantErr: true,
		},
		{
			name: "empty field value",
			tmpl: &Template{
				ID:    "test",
				Name:  "Test",
				Title: "Test",
				Fields: []Field{
					{Label: "Label", Value: ""},
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.tmpl.Validate(); (err != nil) != tt.wantErr {
				t.Errorf("Template.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestEngineExecute(t *testing.T) {
	engine := NewEngine()

	tests := []struct {
		name    string
		text    string
		params  map[string]interface{}
		want    string
		wantErr bool
	}{
		{
			name: "simple variable",
			text: "Hello {{.Name}}",
			params: map[string]interface{}{
				"Name": "World",
			},
			want:    "Hello World",
			wantErr: false,
		},
		{
			name: "multiple variables",
			text: "{{.Greeting}} {{.Name}}!",
			params: map[string]interface{}{
				"Greeting": "Hello",
				"Name":     "World",
			},
			want:    "Hello World!",
			wantErr: false,
		},
		{
			name: "with function",
			text: "{{.Name | toUpper}}",
			params: map[string]interface{}{
				"Name": "hello",
			},
			want:    "HELLO",
			wantErr: false,
		},
		{
			name: "no template syntax",
			text: "plain text",
			params: map[string]interface{}{
				"Name": "World",
			},
			want:    "plain text",
			wantErr: false,
		},
		{
			name:    "empty text",
			text:    "",
			params:  map[string]interface{}{},
			want:    "",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := engine.Execute(tt.text, tt.params)
			if (err != nil) != tt.wantErr {
				t.Errorf("Engine.Execute() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("Engine.Execute() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestManager(t *testing.T) {
	manager := NewManager()

	// Test Register
	tmpl := NewTemplate("test")
	tmpl.Name = "Test Template"
	tmpl.Title = "Test {{.Var}}"
	tmpl.Fields = []Field{
		{Label: "Label", Value: "{{.Value}}", Type: FieldTypeText},
	}

	if err := manager.Register(tmpl); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	// Test Get
	got, err := manager.Get("test")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.Name != tmpl.Name {
		t.Errorf("Get() Name = %v, want %v", got.Name, tmpl.Name)
	}

	// Test List
	templates := manager.List()
	if len(templates) != 1 {
		t.Errorf("List() count = %v, want 1", len(templates))
	}

	// Test Delete
	if err := manager.Delete("test"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if _, err := manager.Get("test"); err != ErrTemplateNotFound {
		t.Errorf("After Delete(), Get() should return ErrTemplateNotFound, got %v", err)
	}
}

func TestTextRenderer(t *testing.T) {
	engine := NewEngine()
	renderer := NewTextRenderer(engine)

	tmpl := &Template{
		ID:    "test",
		Name:  "Test",
		Title: "{{.Title}}",
		Fields: []Field{
			{Label: "Field1", Value: "{{.Value1}}", Type: FieldTypeText},
			{Label: "Field2", Value: "{{.Value2}}", Type: FieldTypeText},
		},
	}

	params := map[string]interface{}{
		"Title":  "Test Title",
		"Value1": "Value1",
		"Value2": "Value2",
	}

	result, err := renderer.Render(context.Background(), tmpl, params)
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}

	expected := "Test Title\nField1: Value1\nField2: Value2\n"
	if result != expected {
		t.Errorf("Render() = %v, want %v", result, expected)
	}
}

func TestMarkdownRenderer(t *testing.T) {
	engine := NewEngine()
	renderer := NewMarkdownRenderer(engine)

	tmpl := &Template{
		ID:    "test",
		Name:  "Test",
		Title: "{{.Title}}",
		Level: "error",
		Fields: []Field{
			{Label: "Field1", Value: "{{.Value1}}", Type: FieldTypeText},
		},
	}

	params := map[string]interface{}{
		"Title":  "Test Title",
		"Value1": "Value1",
	}

	result, err := renderer.Render(context.Background(), tmpl, params)
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}

	// Should contain header, title, and formatted field
	if !contains(result, "###") {
		t.Errorf("Render() should contain header, got %v", result)
	}
	if !contains(result, "Test Title") {
		t.Errorf("Render() should contain title, got %v", result)
	}
	if !contains(result, "Field1") {
		t.Errorf("Render() should contain field label, got %v", result)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && (s[:len(substr)] == substr || s[len(s)-len(substr):] == substr || containsMiddle(s, substr)))
}

func containsMiddle(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
