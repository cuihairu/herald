package apps

import (
	"fmt"

	"github.com/cuihairu/herald/core/template"
)

// The §13.2 模板注册 face: every namespace holds its own template
// manager, so ferry's "node_down" and sinomed's "node_down" are
// strangers like their categories. The manager brings the existing
// template model with it — variable fields, levels and per-channel
// bindings (format / vendor template codes / param order).

// Templates returns the namespace's template manager, created on first
// use. Dispatch renders through it, never through the operator's
// global table.
func (r *Registry) Templates(app string) (*template.Manager, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	a, ok := r.apps[app]
	if !ok {
		return nil, ErrUnknownApp
	}
	if a.templates == nil {
		a.templates = template.NewManager()
	}
	return a.templates, nil
}

// ListTemplates returns the namespace's templates (possibly empty).
func (r *Registry) ListTemplates(app string) ([]*template.Template, error) {
	mgr, err := r.Templates(app)
	if err != nil {
		return nil, err
	}
	return mgr.List(), nil
}

// GetTemplate resolves one namespace template.
func (r *Registry) GetTemplate(app, id string) (*template.Template, error) {
	mgr, err := r.Templates(app)
	if err != nil {
		return nil, err
	}
	tmpl, err := mgr.Get(id)
	if err != nil {
		return nil, fmt.Errorf("template %s: %w", id, err)
	}
	return tmpl, nil
}

// DeleteTemplate removes one namespace template; deleting an unknown
// id is an error, not a no-op (config drift must be visible).
func (r *Registry) DeleteTemplate(app, id string) error {
	mgr, err := r.Templates(app)
	if err != nil {
		return err
	}
	return mgr.Delete(id)
}
