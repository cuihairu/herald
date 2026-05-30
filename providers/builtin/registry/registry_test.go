package builtin

import (
	"testing"

	"github.com/cuihairu/herald/core/runtime"
)

func TestRegisterBuiltinProviders(t *testing.T) {
	t.Run("registers all providers", func(t *testing.T) {
		manager := runtime.NewManager(100)

		// Register builtin providers - should not panic
		RegisterBuiltinProviders(manager)

		// Verify expected providers can be created
		expectedProviders := []string{
			"log",
			"telegram",
			"feishu",
			"wecom",
			"email",
			"webhook",
			"discord",
			"slack",
			"dingtalk",
			"aliyunsms",
			"tencentsms",
			"neteasesms",
			"wechat",
			"wechatmp",
			"worker",
		}

		for _, providerType := range expectedProviders {
			// Try to get schema - this verifies the factory is registered
			schema := manager.GetProviderSchema(providerType)
			if schema == nil {
				t.Errorf("expected schema for provider %s", providerType)
			}

			// Try to create a provider instance with minimal config
			var config map[string]interface{}
			switch providerType {
			case "log":
				config = map[string]interface{}{}
			case "webhook":
				config = map[string]interface{}{"url": "http://example.com"}
			case "email":
				config = map[string]interface{}{"host": "smtp.example.com", "from": "test@example.com"}
			default:
				// For SMS/messaging providers, we need specific config
				// Just verify the schema exists for these
				continue
			}

			if config != nil {
				provider, err := manager.CreateProvider(providerType, config)
				if err != nil && providerType != "worker" {
					// worker might fail without proper config
					t.Logf("Failed to create %s: %v", providerType, err)
				}
				if provider != nil && provider.Type() != providerType {
					t.Errorf("expected provider type %s, got %s", providerType, provider.Type())
				}
			}
		}
	})

	t.Run("idempotent registration", func(t *testing.T) {
		manager := runtime.NewManager(100)

		// Register twice - should not panic
		RegisterBuiltinProviders(manager)
		RegisterBuiltinProviders(manager)

		// Verify we can still create providers after second registration
		_, err := manager.CreateProvider("log", map[string]interface{}{})
		if err != nil {
			t.Errorf("expected to create log provider after double registration, got %v", err)
		}
	})
}
