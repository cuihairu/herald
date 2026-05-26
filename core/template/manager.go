package template

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Manager manages templates
type Manager struct {
	mu        sync.RWMutex
	templates map[string]*Template
	engine    *Engine
}

// NewManager creates a new template manager
func NewManager() *Manager {
	return &Manager{
		templates: make(map[string]*Template),
		engine:    NewEngine(),
	}
}

// Register registers a template
func (m *Manager) Register(tmpl *Template) error {
	if tmpl == nil {
		return fmt.Errorf("template cannot be nil")
	}

	// Validate the template
	if err := tmpl.Validate(); err != nil {
		return err
	}

	// Validate template syntax
	if err := m.engine.ValidateTemplate(tmpl); err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Set timestamps
	if existing, ok := m.templates[tmpl.ID]; ok {
		tmpl.CreatedAt = existing.CreatedAt
	} else if tmpl.CreatedAt.IsZero() {
		tmpl.CreatedAt = time.Now()
	}
	tmpl.UpdatedAt = time.Now()

	m.templates[tmpl.ID] = tmpl
	return nil
}

// Get retrieves a template by ID
func (m *Manager) Get(id string) (*Template, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	tmpl, ok := m.templates[id]
	if !ok {
		return nil, ErrTemplateNotFound
	}

	// Return a copy to prevent mutation
	copy := *tmpl
	copy.Fields = make([]Field, len(tmpl.Fields))
	copyFields(copy.Fields, tmpl.Fields)

	return &copy, nil
}

// List returns all templates
func (m *Manager) List() []*Template {
	m.mu.RLock()
	defer m.mu.RUnlock()

	templates := make([]*Template, 0, len(m.templates))
	for _, tmpl := range m.templates {
		// Return copies
		copy := *tmpl
		copy.Fields = make([]Field, len(tmpl.Fields))
		copyFields(copy.Fields, tmpl.Fields)
		templates = append(templates, &copy)
	}

	return templates
}

// Delete deletes a template by ID
func (m *Manager) Delete(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.templates[id]; !ok {
		return ErrTemplateNotFound
	}

	delete(m.templates, id)
	return nil
}

// Render renders a template with the given parameters using the specified renderer
func (m *Manager) Render(ctx context.Context, templateID string, renderer Renderer, params map[string]interface{}) (string, error) {
	tmpl, err := m.Get(templateID)
	if err != nil {
		return "", err
	}

	if renderer == nil {
		return "", fmt.Errorf("renderer cannot be nil")
	}

	return renderer.Render(ctx, tmpl, params)
}

// LoadFromMap loads templates from a map (used for config loading)
func (m *Manager) LoadFromMap(templates map[string]TemplateConfig) error {
	for id, config := range templates {
		tmpl := NewTemplate(id)
		tmpl.Name = config.Name
		tmpl.Title = config.Title
		tmpl.Level = config.Level

		// Convert field configs
		for _, fc := range config.Fields {
			tmpl.Fields = append(tmpl.Fields, Field{
				Label: fc.Label,
				Value: fc.Value,
				Type:  fc.Type,
			})
		}

		if err := m.Register(tmpl); err != nil {
			return fmt.Errorf("failed to register template %s: %w", id, err)
		}
	}

	return nil
}

// Engine returns the template engine
func (m *Manager) Engine() *Engine {
	return m.engine
}

// copyFields copies fields from src to dst
func copyFields(dst, src []Field) {
	for i := range src {
		dst[i] = src[i]
	}
}

// TemplateConfig is the configuration format for templates in YAML
type TemplateConfig struct {
	Name   string         `yaml:"name"`
	Title  string         `yaml:"title"`
	Level  string         `yaml:"level"`
	Fields []FieldConfig  `yaml:"fields"`
}

// FieldConfig is the configuration format for fields in YAML
type FieldConfig struct {
	Label string `yaml:"label"`
	Value string `yaml:"value"`
	Type  string `yaml:"type"`
}
