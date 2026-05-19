package route

import (
	"testing"

	"github.com/cuihairu/herald/core"
)

func TestNewRouter(t *testing.T) {
	config := &Config{
		Routes: map[string][]string{
			"error":   {"telegram", "slack"},
			"warning": {"slack"},
		},
	}

	router := NewRouter(config)
	if router == nil {
		t.Fatal("expected non-nil router")
	}
}

func TestRouterRoute(t *testing.T) {
	config := &Config{
		Routes: map[string][]string{
			"node.offline": {"telegram"},
			"error":       {"slack"},
		},
	}

	router := NewRouter(config)

	tests := []struct {
		name        string
		eventType   string
		expectedLen int
		expectError bool
	}{
		{
			name:        "existing event type",
			eventType:   "node.offline",
			expectedLen: 1,
			expectError: false,
		},
		{
			name:        "another existing event",
			eventType:   "error",
			expectedLen: 1,
			expectError: false,
		},
		{
			name:        "non-existing event",
			eventType:   "unknown",
			expectedLen: 0,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := &core.Event{
				Type: tt.eventType,
			}

			providers, err := router.Route(event)

			if tt.expectError && err == nil {
				t.Errorf("expected error, got nil")
			}
			if !tt.expectError && err != nil {
				t.Errorf("expected no error, got %v", err)
			}
			if len(providers) != tt.expectedLen {
				t.Errorf("expected %d providers, got %d", tt.expectedLen, len(providers))
			}
		})
	}
}

func TestRouterRouteByLevel(t *testing.T) {
	router := NewRouter(&Config{
		LevelRoutes: map[string][]string{
			"error":   {"telegram"},
			"warning": {"slack"},
		},
	})

	tests := []struct {
		name        string
		level       string
		expectedLen int
		expectError bool
	}{
		{
			name:        "error level",
			level:       "error",
			expectedLen: 1,
			expectError: false,
		},
		{
			name:        "warning level",
			level:       "warning",
			expectedLen: 1,
			expectError: false,
		},
		{
			name:        "unknown level",
			level:       "unknown",
			expectedLen: 0,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			providers, err := router.RouteByLevel(tt.level)

			if tt.expectError && err == nil {
				t.Errorf("expected error, got nil")
			}
			if !tt.expectError && err != nil {
				t.Errorf("expected no error, got %v", err)
			}
			if len(providers) != tt.expectedLen {
				t.Errorf("expected %d providers, got %d", tt.expectedLen, len(providers))
			}
		})
	}
}

func TestRouterSetRoute(t *testing.T) {
	router := NewRouter(nil)

	router.SetRoute("test.event", []string{"provider1"})

	event := &core.Event{Type: "test.event"}
	providers, err := router.Route(event)

	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if len(providers) != 1 {
		t.Errorf("expected 1 provider, got %d", len(providers))
	}
	if providers[0] != "provider1" {
		t.Errorf("expected provider1, got %s", providers[0])
	}
}

func TestRouterSetLevelRoute(t *testing.T) {
	router := NewRouter(nil)

	router.SetLevelRoute("critical", []string{"telegram"})

	providers, err := router.RouteByLevel("critical")

	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if len(providers) != 1 {
		t.Errorf("expected 1 provider, got %d", len(providers))
	}
	if providers[0] != "telegram" {
		t.Errorf("expected telegram, got %s", providers[0])
	}
}
