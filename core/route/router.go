package route

import (
	"fmt"
)

// Router routes notifications to providers
type Router struct {
	routes      map[string][]string
	levelRoutes map[string][]string
}

// Config is the router configuration
type Config struct {
	Routes      map[string][]string `yaml:"routes"`
	LevelRoutes map[string][]string `yaml:"level_routes"`
}

// NewRouter creates a new router
func NewRouter(config *Config) *Router {
	r := &Router{
		routes:      make(map[string][]string),
		levelRoutes: make(map[string][]string),
	}

	if config != nil {
		for k, v := range config.Routes {
			r.routes[k] = v
		}
		for k, v := range config.LevelRoutes {
			r.levelRoutes[k] = v
		}
	}

	if len(r.levelRoutes) == 0 {
		r.levelRoutes = map[string][]string{
			"error":   {},
			"warning": {},
			"info":    {},
		}
	}

	return r
}

// Route resolves providers for a notification type and level
func (r *Router) Route(notificationType, level string) ([]string, error) {
	// Try by notification type
	if providers, ok := r.routes[notificationType]; ok && len(providers) > 0 {
		return providers, nil
	}

	// Try by level
	if providers, ok := r.levelRoutes[level]; ok && len(providers) > 0 {
		return providers, nil
	}

	return nil, fmt.Errorf("no route found for type=%s level=%s", notificationType, level)
}

// SetRoute sets a route
func (r *Router) SetRoute(eventType string, providers []string) {
	r.routes[eventType] = providers
}

// SetLevelRoute sets a level route
func (r *Router) SetLevelRoute(level string, providers []string) {
	r.levelRoutes[level] = providers
}
