package template

import (
	"fmt"
	"sync"
	"time"
)

// Manager manages templates and rendering
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

	if err := m.validate(tmpl); err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

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
		return nil, fmt.Errorf("template not found: %s", id)
	}

	// Return a copy
	copy := *tmpl
	copy.Fields = make([]Field, len(tmpl.Fields))
	for i := range tmpl.Fields {
		copy.Fields[i] = tmpl.Fields[i]
	}
	if tmpl.Bindings != nil {
		copy.Bindings = make(map[string]Binding, len(tmpl.Bindings))
		for k, v := range tmpl.Bindings {
			copy.Bindings[k] = v
		}
	}
	return &copy, nil
}

// List returns all templates
func (m *Manager) List() []*Template {
	m.mu.RLock()
	defer m.mu.RUnlock()

	templates := make([]*Template, 0, len(m.templates))
	for _, tmpl := range m.templates {
		copy := *tmpl
		copy.Fields = make([]Field, len(tmpl.Fields))
		for i := range tmpl.Fields {
			copy.Fields[i] = tmpl.Fields[i]
		}
		if tmpl.Bindings != nil {
			copy.Bindings = make(map[string]Binding, len(tmpl.Bindings))
			for k, v := range tmpl.Bindings {
				copy.Bindings[k] = v
			}
		}
		templates = append(templates, &copy)
	}
	return templates
}

// Delete deletes a template
func (m *Manager) Delete(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.templates[id]; !ok {
		return fmt.Errorf("template not found: %s", id)
	}

	delete(m.templates, id)
	return nil
}

// Render renders a template with parameters
func (m *Manager) Render(templateID string, params map[string]interface{}) (*RenderedData, error) {
	tmpl, err := m.Get(templateID)
	if err != nil {
		return nil, err
	}

	data, err := m.engine.Render(tmpl, params)
	if err != nil {
		return nil, err
	}

	data.RenderedAt = time.Now()
	return data, nil
}

// LoadFromMap loads templates from config map
func (m *Manager) LoadFromMap(templates map[string]TemplateConfig) error {
	for id, config := range templates {
		tmpl := &Template{
			ID:       id,
			Name:     config.Name,
			Title:    config.Title,
			Level:    config.Level,
			Fields:   make([]Field, len(config.Fields)),
			Bindings: config.Bindings,
		}

		for i, fc := range config.Fields {
			tmpl.Fields[i] = Field(fc)
		}

		if err := m.Register(tmpl); err != nil {
			return fmt.Errorf("failed to register template %s: %w", id, err)
		}
	}
	return nil
}

// validate validates a template
func (m *Manager) validate(tmpl *Template) error {
	if tmpl.ID == "" {
		return fmt.Errorf("template id cannot be empty")
	}
	if tmpl.Name == "" {
		return fmt.Errorf("template name cannot be empty")
	}
	if tmpl.Title == "" {
		return fmt.Errorf("template title cannot be empty")
	}
	for i, field := range tmpl.Fields {
		if field.Label == "" {
			return fmt.Errorf("field at index %d: label cannot be empty", i)
		}
		if field.Value == "" {
			return fmt.Errorf("field at index %d: value cannot be empty", i)
		}
	}
	return nil
}

// TemplateConfig is the config format for templates in YAML
type TemplateConfig struct {
	Name     string             `yaml:"name"`
	Title    string             `yaml:"title"`
	Level    string             `yaml:"level"`
	Fields   []FieldConfig      `yaml:"fields"`
	Bindings map[string]Binding `yaml:"bindings"`
}

// FieldConfig is the config format for fields in YAML
type FieldConfig struct {
	Label string `yaml:"label"`
	Value string `yaml:"value"`
	Type  string `yaml:"type"`
}
