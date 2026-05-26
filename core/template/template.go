package template

import "time"

// Template represents a message template that is independent of delivery channels
type Template struct {
	ID        string    `json:"id" yaml:"id"`                 // Template unique identifier
	Name      string    `json:"name" yaml:"name"`             // Template name
	Title     string    `json:"title" yaml:"title"`           // Title template (supports variables)
	Level     string    `json:"level" yaml:"level"`           // Default level (error, warning, info)
	Fields    []Field   `json:"fields" yaml:"fields"`         // Field list (key-value pairs)
	CreatedAt time.Time `json:"created_at" yaml:"created_at"` // Creation time
	UpdatedAt time.Time `json:"updated_at" yaml:"updated_at"` // Update time
}

// Field represents a field in the template
type Field struct {
	Label string `json:"label" yaml:"label"`     // Field label (display name)
	Value string `json:"value" yaml:"value"`     // Field value template (supports variables)
	Type  string `json:"type" yaml:"type"`       // Field type: text, number, datetime, link, etc.
}

// FieldType constants
const (
	FieldTypeText     = "text"
	FieldTypeNumber   = "number"
	FieldTypeDateTime = "datetime"
	FieldTypeLink     = "link"
	FieldTypeMarkdown = "markdown"
)

// NewTemplate creates a new template with the given ID
func NewTemplate(id string) *Template {
	now := time.Now()
	return &Template{
		ID:        id,
		CreatedAt: now,
		UpdatedAt: now,
	}
}

// Validate validates the template
func (t *Template) Validate() error {
	if t.ID == "" {
		return ErrInvalidTemplateID
	}
	if t.Name == "" {
		return ErrInvalidTemplateName
	}
	if t.Title == "" {
		return ErrInvalidTemplateTitle
	}
	for i, field := range t.Fields {
		if field.Label == "" {
			return &FieldError{Index: i, Err: ErrInvalidFieldLabel}
		}
		if field.Value == "" {
			return &FieldError{Index: i, Err: ErrInvalidFieldValue}
		}
	}
	return nil
}
