package template

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"text/template"
)

// Engine handles template variable substitution
type Engine struct {
	templates map[string]*template.Template
	mu        sync.RWMutex
	funcMap   template.FuncMap
}

// NewEngine creates a new template engine
func NewEngine() *Engine {
	return &Engine{
		templates: make(map[string]*template.Template),
		funcMap:   defaultFuncMap(),
	}
}

func defaultFuncMap() template.FuncMap {
	return template.FuncMap{
		"toUpper": strings.ToUpper,
		"toLower": strings.ToLower,
		"trim":    strings.TrimSpace,
	}
}

// Execute executes a template string with the given parameters
func (e *Engine) Execute(text string, params map[string]interface{}) (string, error) {
	if text == "" {
		return "", nil
	}

	if !strings.Contains(text, "{{") {
		return text, nil
	}

	hash := sha256.Sum256([]byte(text))
	key := "tpl_" + hex.EncodeToString(hash[:8])

	e.mu.RLock()
	tmpl, ok := e.templates[key]
	e.mu.RUnlock()

	if !ok {
		var err error
		tmpl, err = template.New(key).Funcs(e.funcMap).Parse(text)
		if err != nil {
			return "", fmt.Errorf("failed to parse template: %w", err)
		}
		e.mu.Lock()
		e.templates[key] = tmpl
		e.mu.Unlock()
	}

	var buf strings.Builder
	if err := tmpl.Execute(&buf, params); err != nil {
		return "", fmt.Errorf("failed to execute template: %w", err)
	}

	return buf.String(), nil
}

// Render renders a template with parameters, producing RenderedData
func (e *Engine) Render(tmpl *Template, params map[string]interface{}) (*RenderedData, error) {
	// Render title
	title, err := e.Execute(tmpl.Title, params)
	if err != nil {
		return nil, fmt.Errorf("failed to render title: %w", err)
	}

	// Render fields
	renderedFields := make([]RenderedField, len(tmpl.Fields))
	for i, field := range tmpl.Fields {
		value, err := e.Execute(field.Value, params)
		if err != nil {
			return nil, fmt.Errorf("failed to render field %s: %w", field.Label, err)
		}
		renderedFields[i] = RenderedField{
			Label: field.Label,
			Value: value,
			Type:  field.Type,
		}
	}

	return &RenderedData{
		TemplateID: tmpl.ID,
		Title:      title,
		Level:      tmpl.Level,
		Fields:     renderedFields,
		Meta:       params,
	}, nil
}
