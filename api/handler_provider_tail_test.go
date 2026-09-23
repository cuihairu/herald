package api

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"unsafe"

	"github.com/cuihairu/herald/core"
	"github.com/cuihairu/herald/core/runtime"
)

// removeRegisteredProviderEntry deletes one entry from the manager's private
// providers map. The Manager exposes no unregister API, so reflection is used
// to simulate a provider vanishing from the runtime mid-request.
func removeRegisteredProviderEntry(m *runtime.Manager, name string) {
	field := reflect.ValueOf(m).Elem().FieldByName("providers")
	if !field.IsValid() || field.Kind() != reflect.Map {
		panic("runtime.Manager has no providers map field")
	}
	entries := reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).Elem()
	entries.SetMapIndex(reflect.ValueOf(name), reflect.Value{})
}

// vanishingFactory removes its victim provider from the manager while the
// handler is inside CreateProvider: after GetProviderType has succeeded but
// before ReplaceProvider runs. That drives the defensive ReplaceProvider
// failure branch in HandleUpdateProviderConfig (500, "provider not found").
type vanishingFactory struct {
	stubFactory
	manager *runtime.Manager
	victim  string
}

func (f *vanishingFactory) Create(config map[string]interface{}) (core.Provider, error) {
	removeRegisteredProviderEntry(f.manager, f.victim)
	return f.stubFactory.Create(config)
}

func registerVanishingProvider(t *testing.T, env *testEnv, name string) {
	t.Helper()
	env.runtime.RegisterFactory(&vanishingFactory{
		stubFactory: stubFactory{name: "vanish"},
		manager:     env.runtime,
		victim:      name,
	})
	if err := env.runtime.RegisterProvider(name, &configProvider{
		stubProvider: stubProvider{name: name, pType: "vanish"},
		config:       map[string]interface{}{"url": "old"},
	}); err != nil {
		t.Fatal(err)
	}
}

func TestHandleUpdateProviderConfigReplaceFailure(t *testing.T) {
	t.Run("replace provider vanished mid-request returns 500", func(t *testing.T) {
		env := newTestEnv(t)
		registerVanishingProvider(t, env, "vp")

		code, resp := env.do(t, http.MethodPut, "/api/v1/config/vp", `{"config":{"url":"new"}}`, nil)
		if code != http.StatusInternalServerError {
			t.Fatalf("expected status 500, got %d", code)
		}
		if num(t, resp["code"]) != http.StatusInternalServerError {
			t.Errorf("expected code 500, got %v", resp["code"])
		}
		if msg, _ := resp["message"].(string); !strings.Contains(msg, "provider not found") {
			t.Errorf("expected 'provider not found' in message, got %v", resp["message"])
		}
	})

	t.Run("replace failure via handler call returns 500", func(t *testing.T) {
		env := newTestEnv(t)
		registerVanishingProvider(t, env, "vp2")

		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPut, "/api/v1/config/vp2", strings.NewReader(`{"config":{"url":"new"}}`))
		env.server.handler.HandleUpdateProviderConfig(w, req, "vp2")

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("expected status 500, got %d", w.Code)
		}
		if body := w.Body.String(); !strings.Contains(body, "provider not found") {
			t.Errorf("expected 'provider not found' in body, got %s", body)
		}
	})
}
