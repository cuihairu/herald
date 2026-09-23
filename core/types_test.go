package core

import "testing"

func TestMaskConfig(t *testing.T) {
	config := map[string]interface{}{
		"token":             "abc",
		"secret":            "s3cr3t",
		"password":          "hunter2",
		"secret_key":        "sk-1",
		"access_key_secret": "aks",
		"app_secret":        "as",
		"bot_token":         "bt",
		"sign_secret":       "ss",
		"endpoint":          "https://example.com",
		"timeout":           30,
		"nested":            map[string]interface{}{"secret": "inner"},
	}

	masked := MaskConfig(config)

	for _, key := range []string{"token", "secret", "password", "secret_key",
		"access_key_secret", "app_secret", "bot_token", "sign_secret"} {
		if got := masked[key]; got != "******" {
			t.Errorf("masked[%q] = %v, want %q", key, got, "******")
		}
	}

	if got := masked["endpoint"]; got != "https://example.com" {
		t.Errorf("masked[endpoint] = %v, want original value", got)
	}
	if got := masked["timeout"]; got != 30 {
		t.Errorf("masked[timeout] = %v, want 30", got)
	}

	// Only top-level keys are masked; nested maps pass through untouched.
	nested, ok := masked["nested"].(map[string]interface{})
	if !ok || nested["secret"] != "inner" {
		t.Errorf("masked[nested] = %#v, want untouched nested map", masked["nested"])
	}

	// The original config must not be modified.
	if config["token"] != "abc" {
		t.Errorf("MaskConfig mutated the input map: token = %v", config["token"])
	}
}

func TestMaskConfigEmpty(t *testing.T) {
	got := MaskConfig(map[string]interface{}{})
	if len(got) != 0 {
		t.Errorf("MaskConfig(empty) = %v, want empty map", got)
	}
}
