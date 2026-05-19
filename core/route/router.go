package route

import (
	"fmt"

	"github.com/cuihairu/herald/core"
)

// Router routes events to providers
type Router struct {
	routes map[string][]string
	// levelRoutes maps event levels to providers
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

	// Default level routes
	if len(r.levelRoutes) == 0 {
		r.levelRoutes = map[string][]string{
			"error":   {},
			"warning": {},
			"info":    {},
		}
	}

	return r
}

// Route routes an event to providers
func (r *Router) Route(event *core.Event) ([]string, error) {
	// First, try to route by event type
	if providers, ok := r.routes[event.Type]; ok {
		return providers, nil
	}

	// Then, try to route by event level (if present in labels)
	if level, ok := event.Labels["level"]; ok {
		if providers, ok := r.levelRoutes[level]; ok && len(providers) > 0 {
			return providers, nil
		}
	}

	return nil, fmt.Errorf("no route found for event type: %s", event.Type)
}

// RouteByLevel routes by notification level
func (r *Router) RouteByLevel(level string) ([]string, error) {
	// First check level routes
	if providers, ok := r.levelRoutes[level]; ok && len(providers) > 0 {
		return providers, nil
	}

	// Then check routes (for backward compatibility)
	if providers, ok := r.routes[level]; ok && len(providers) > 0 {
		return providers, nil
	}

	return nil, fmt.Errorf("no route found for level: %s", level)
}

// SetRoute sets a route
func (r *Router) SetRoute(eventType string, providers []string) {
	r.routes[eventType] = providers
}

// SetLevelRoute sets a level route
func (r *Router) SetLevelRoute(level string, providers []string) {
	r.levelRoutes[level] = providers
}
