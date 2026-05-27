package template

import "time"

// RenderFormat represents the output format of rendered content
type RenderFormat string

const (
	RenderFormatAuto     RenderFormat = "auto"     // Let provider decide
	RenderFormatHTML     RenderFormat = "html"     // HTML format
	RenderFormatMarkdown RenderFormat = "markdown" // Markdown format
	RenderFormatPlain    RenderFormat = "plain"    // Plain text
	RenderFormatJSON     RenderFormat = "json"     // JSON format
)

// FieldType represents the type of a field value
type FieldType string

const (
	FieldTypeText     FieldType = "text"
	FieldTypeNumber   FieldType = "number"
	FieldTypeDateTime FieldType = "datetime"
	FieldTypeLink     FieldType = "link"
	FieldTypeMarkdown FieldType = "markdown"
)

// Template represents a message template (pure semantic structure)
type Template struct {
	ID        string             `json:"id" yaml:"id"`
	Name      string             `json:"name" yaml:"name"`
	Title     string             `json:"title" yaml:"title"`                           // Title template with variables
	Level     string             `json:"level" yaml:"level"`                           // Default level: error, warning, info
	Fields    []Field            `json:"fields" yaml:"fields"`                         // Field definitions
	Bindings  map[string]Binding `json:"bindings,omitempty" yaml:"bindings,omitempty"` // Per-channel config
	CreatedAt time.Time          `json:"created_at" yaml:"created_at"`
	UpdatedAt time.Time          `json:"updated_at" yaml:"updated_at"`
}

// Binding is a per-channel configuration that maps a template to a specific provider
type Binding struct {
	Format       string            `json:"format,omitempty" yaml:"format,omitempty"`               // html, markdown, plain, json (content providers)
	TemplateCode string            `json:"template_code,omitempty" yaml:"template_code,omitempty"` // SMS template code (aliyun)
	TemplateID   string            `json:"template_id,omitempty" yaml:"template_id,omitempty"`     // SMS template ID (tencent/netease)
	Params       map[string]string `json:"params,omitempty" yaml:"params,omitempty"`               // named params: field label → vendor param key
	ParamOrder   []string          `json:"param_order,omitempty" yaml:"param_order,omitempty"`     // ordered params: field labels in vendor-required order
}

// Field represents a single field in the template
type Field struct {
	Label string `json:"label" yaml:"label"` // Display label
	Value string `json:"value" yaml:"value"` // Value template with variables
	Type  string `json:"type" yaml:"type"`   // Field type hint (optional, as string)
}

// RenderedData represents the filled template data after parameter substitution
// This is a channel-agnostic data structure that renderers can format
type RenderedData struct {
	TemplateID string                 `json:"template_id"`
	Title      string                 `json:"title"`
	Level      string                 `json:"level"`
	Fields     []RenderedField        `json:"fields"`
	Meta       map[string]interface{} `json:"meta,omitempty"` // Original params for reference
	RenderedAt time.Time              `json:"rendered_at"`
}

// RenderedField represents a filled field
type RenderedField struct {
	Label string `json:"label"`
	Value string `json:"value"`
	Type  string `json:"type"`
}
