package route

import (
	"testing"
)

func TestNewRouter(t *testing.T) {
	router := NewRouter(&Config{
		Routes: map[string][]string{
			"error":   {"telegram", "slack"},
			"warning": {"slack"},
		},
	})
	if router == nil {
		t.Fatal("expected non-nil router")
	}
}

func TestRouterRoute(t *testing.T) {
	router := NewRouter(&Config{
		Routes: map[string][]string{
			"node.offline": {"telegram"},
		},
		LevelRoutes: map[string][]string{
			"error": {"slack"},
		},
	})

	tests := []struct {
		name             string
		notificationType string
		level            string
		expectedLen      int
		expectError      bool
	}{
		{"by type", "node.offline", "", 1, false},
		{"by level", "unknown", "error", 1, false},
		{"not found", "unknown", "unknown", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			providers, err := router.Route(tt.notificationType, tt.level)
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

	providers, err := router.Route("test.event", "")
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if providers[0] != "provider1" {
		t.Errorf("expected provider1, got %s", providers[0])
	}
}

func TestRouterSetLevelRoute(t *testing.T) {
	router := NewRouter(nil)
	router.SetLevelRoute("critical", []string{"telegram"})

	providers, err := router.Route("unknown", "critical")
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if providers[0] != "telegram" {
		t.Errorf("expected telegram, got %s", providers[0])
	}
}
