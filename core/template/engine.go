package template

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"

	texttemplate "text/template"
)

// Engine is the template engine for variable substitution
// Uses Go template syntax: {{.VariableName}}
type Engine struct {
	templates map[string]*texttemplate.Template
	mu        sync.RWMutex

	// Custom template functions
	funcMap texttemplate.FuncMap
}

// NewEngine creates a new template engine
func NewEngine() *Engine {
	return &Engine{
		templates: make(map[string]*texttemplate.Template),
		funcMap:   defaultFuncMap(),
	}
}

// defaultFuncMap returns the default template functions
func defaultFuncMap() texttemplate.FuncMap {
	return texttemplate.FuncMap{
		"toUpper": strings.ToUpper,
		"toLower": strings.ToLower,
		"title":   strings.Title,
		"trim":    strings.TrimSpace,
	}
}

// Execute executes the template text with the given parameters
func (e *Engine) Execute(text string, params map[string]interface{}) (string, error) {
	if text == "" {
		return "", nil
	}

	// Check if it contains template syntax
	if !strings.Contains(text, "{{") {
		return text, nil
	}

	// Create a unique key for this template based on content hash
	hash := sha256.Sum256([]byte(text))
	key := "tpl_" + hex.EncodeToString(hash[:8]) // Use first 8 bytes for shorter key

	// Try to get from cache first
	e.mu.RLock()
	tmpl, ok := e.templates[key]
	e.mu.RUnlock()

	// If not in cache, parse and store
	if !ok {
		var err error
		tmpl, err = texttemplate.New(key).Funcs(e.funcMap).Parse(text)
		if err != nil {
			return "", fmt.Errorf("failed to parse template: %w", err)
		}

		e.mu.Lock()
		e.templates[key] = tmpl
		e.mu.Unlock()
	}

	// Execute the template
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, params); err != nil {
		return "", fmt.Errorf("failed to execute template: %w", err)
	}

	return buf.String(), nil
}

// Validate validates the template syntax
func (e *Engine) Validate(text string) error {
	if text == "" {
		return nil
	}

	if !strings.Contains(text, "{{") {
		return nil
	}

	_, err := texttemplate.New("validate").Funcs(e.funcMap).Parse(text)
	return err
}

// ValidateTemplate validates a complete template structure
func (e *Engine) ValidateTemplate(tmpl *Template) error {
	if err := e.Validate(tmpl.Title); err != nil {
		return fmt.Errorf("invalid title template: %w", err)
	}

	for i, field := range tmpl.Fields {
		if err := e.Validate(field.Value); err != nil {
			return &FieldError{
				Index: i,
				Err:   fmt.Errorf("invalid value template: %w", err),
			}
		}
	}

	return nil
}

// Clear clears the template cache
func (e *Engine) Clear() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.templates = make(map[string]*texttemplate.Template)
}

// AddFunc adds a custom template function
func (e *Engine) AddFunc(name string, fn interface{}) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.funcMap == nil {
		e.funcMap = make(texttemplate.FuncMap)
	}
	e.funcMap[name] = fn

	// Clear cache as functions may have changed
	e.templates = make(map[string]*texttemplate.Template)
}
