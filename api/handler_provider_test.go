package api

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleProviders(t *testing.T) {
	env := newTestEnv(t)
	if err := env.runtime.RegisterProvider("p1", &stubProvider{name: "p1", pType: "webhook"}); err != nil {
		t.Fatal(err)
	}
	if err := env.runtime.RegisterProvider("p2", &stubProvider{name: "p2", pType: "webhook"}, false); err != nil {
		t.Fatal(err)
	}

	code, resp := env.do(t, http.MethodGet, "/api/v1/providers", "", nil)
	if code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", code)
	}
	data := dataOf(t, resp)
	providers, _ := data["providers"].([]any)
	if len(providers) != 2 {
		t.Fatalf("expected 2 providers, got %d", len(providers))
	}

	foundDisabled := false
	for _, p := range providers {
		entry, _ := p.(map[string]any)
		if entry["name"] == "p2" {
			foundDisabled = true
			if entry["enabled"] != false {
				t.Errorf("expected p2 disabled, got %v", entry["enabled"])
			}
		}
	}
	if !foundDisabled {
		t.Error("expected p2 in provider list")
	}
}

func TestHandleEnableDisableProvider(t *testing.T) {
	t.Run("enable", func(t *testing.T) {
		env := newTestEnv(t)
		if err := env.runtime.RegisterProvider("p1", &stubProvider{name: "p1", pType: "webhook"}, false); err != nil {
			t.Fatal(err)
		}
		code, resp := env.do(t, http.MethodPost, "/api/v1/providers/p1/enable", "", nil)
		if code != http.StatusOK {
			t.Fatalf("expected status 200, got %d", code)
		}
		if num(t, resp["code"]) != 0 {
			t.Errorf("expected code 0, got %v", resp["code"])
		}
		if !env.runtime.IsEnabled("p1") {
			t.Error("expected p1 enabled")
		}
	})

	t.Run("enable not found", func(t *testing.T) {
		env := newTestEnv(t)
		code, resp := env.do(t, http.MethodPost, "/api/v1/providers/missing/enable", "", nil)
		if code != http.StatusNotFound {
			t.Fatalf("expected status 404, got %d", code)
		}
		if num(t, resp["code"]) != http.StatusNotFound {
			t.Errorf("expected code 404, got %v", resp["code"])
		}
	})

	t.Run("disable", func(t *testing.T) {
		env := newTestEnv(t)
		if err := env.runtime.RegisterProvider("p1", &stubProvider{name: "p1", pType: "webhook"}); err != nil {
			t.Fatal(err)
		}
		code, resp := env.do(t, http.MethodPost, "/api/v1/providers/p1/disable", "", nil)
		if code != http.StatusOK {
			t.Fatalf("expected status 200, got %d", code)
		}
		if num(t, resp["code"]) != 0 {
			t.Errorf("expected code 0, got %v", resp["code"])
		}
		if env.runtime.IsEnabled("p1") {
			t.Error("expected p1 disabled")
		}
	})

	t.Run("disable not found", func(t *testing.T) {
		env := newTestEnv(t)
		code, _ := env.do(t, http.MethodPost, "/api/v1/providers/missing/disable", "", nil)
		if code != http.StatusNotFound {
			t.Errorf("expected status 404, got %d", code)
		}
	})
}

func TestHandleProviderConfig(t *testing.T) {
	t.Run("provider with config", func(t *testing.T) {
		env := newTestEnv(t)
		err := env.runtime.RegisterProvider("cp", &configProvider{
			stubProvider: stubProvider{name: "cp", pType: "webhook"},
			config:       map[string]interface{}{"url": "http://example.com", "token": "secret-value"},
		})
		if err != nil {
			t.Fatal(err)
		}

		code, resp := env.do(t, http.MethodGet, "/api/v1/config/cp", "", nil)
		if code != http.StatusOK {
			t.Fatalf("expected status 200, got %d", code)
		}
		data := dataOf(t, resp)
		if data["name"] != "cp" {
			t.Errorf("expected name cp, got %v", data["name"])
		}
		if data["type"] != "webhook" {
			t.Errorf("expected type webhook, got %v", data["type"])
		}
		if data["enabled"] != true {
			t.Errorf("expected enabled true, got %v", data["enabled"])
		}
		config, _ := data["config"].(map[string]any)
		if config["url"] != "http://example.com" {
			t.Errorf("expected url preserved, got %v", config["url"])
		}
		if config["token"] != "******" {
			t.Errorf("expected token masked, got %v", config["token"])
		}
		schema, _ := data["schema"].(map[string]any)
		if len(schema) == 0 {
			t.Error("expected non-empty schema for known type")
		}
	})

	t.Run("provider without config", func(t *testing.T) {
		env := newTestEnv(t)
		if err := env.runtime.RegisterProvider("sp", &stubProvider{name: "sp", pType: "custom-type"}); err != nil {
			t.Fatal(err)
		}

		code, resp := env.do(t, http.MethodGet, "/api/v1/config/sp", "", nil)
		if code != http.StatusOK {
			t.Fatalf("expected status 200, got %d", code)
		}
		data := dataOf(t, resp)
		config, _ := data["config"].(map[string]any)
		if len(config) != 0 {
			t.Errorf("expected empty config, got %v", config)
		}
		schema, _ := data["schema"].(map[string]any)
		if schema["config"] != "object" {
			t.Errorf("expected default schema, got %v", schema)
		}
	})

	t.Run("not found", func(t *testing.T) {
		env := newTestEnv(t)
		code, _ := env.do(t, http.MethodGet, "/api/v1/config/missing", "", nil)
		if code != http.StatusNotFound {
			t.Errorf("expected status 404, got %d", code)
		}
	})

	t.Run("empty name", func(t *testing.T) {
		env := newTestEnv(t)
		h := env.server.handler
		w := httptest.NewRecorder()
		h.HandleProviderConfig(w, httptest.NewRequest(http.MethodGet, "/api/v1/config/", nil), "")
		if w.Code != http.StatusBadRequest {
			t.Errorf("expected status 400, got %d", w.Code)
		}
	})
}

func TestHandleUpdateProviderConfig(t *testing.T) {
	t.Run("replace config", func(t *testing.T) {
		env := newTestEnv(t)
		env.runtime.RegisterFactory(&stubFactory{name: "webhook"})
		err := env.runtime.RegisterProvider("cp", &configProvider{
			stubProvider: stubProvider{name: "cp", pType: "webhook"},
			config:       map[string]interface{}{"url": "old", "token": "secret"},
		})
		if err != nil {
			t.Fatal(err)
		}

		code, resp := env.do(t, http.MethodPut, "/api/v1/config/cp", `{"config":{"url":"new","token":"tok2"}}`, nil)
		if code != http.StatusOK {
			t.Fatalf("expected status 200, got %d", code)
		}
		if num(t, resp["code"]) != 0 {
			t.Errorf("expected code 0, got %v", resp["code"])
		}
		if !env.runtime.IsEnabled("cp") {
			t.Error("expected cp to stay enabled after replace")
		}

		_, got := env.do(t, http.MethodGet, "/api/v1/config/cp", "", nil)
		data := dataOf(t, got)
		config, _ := data["config"].(map[string]any)
		if config["url"] != "new" {
			t.Errorf("expected url new, got %v", config["url"])
		}
		if config["token"] != "******" {
			t.Errorf("expected token masked, got %v", config["token"])
		}
	})

	t.Run("post is accepted", func(t *testing.T) {
		env := newTestEnv(t)
		env.runtime.RegisterFactory(&stubFactory{name: "webhook"})
		err := env.runtime.RegisterProvider("cp", &configProvider{
			stubProvider: stubProvider{name: "cp", pType: "webhook"},
			config:       map[string]interface{}{"url": "old"},
		})
		if err != nil {
			t.Fatal(err)
		}

		code, resp := env.do(t, http.MethodPost, "/api/v1/config/cp", `{"config":{"url":"posted"}}`, nil)
		if code != http.StatusOK {
			t.Fatalf("expected status 200, got %d", code)
		}
		if num(t, resp["code"]) != 0 {
			t.Errorf("expected code 0, got %v", resp["code"])
		}
	})

	t.Run("merge preserves masked values", func(t *testing.T) {
		env := newTestEnv(t)
		env.runtime.RegisterFactory(&stubFactory{name: "webhook"})
		err := env.runtime.RegisterProvider("cp", &configProvider{
			stubProvider: stubProvider{name: "cp", pType: "webhook"},
			config:       map[string]interface{}{"url": "old", "token": "secret"},
		})
		if err != nil {
			t.Fatal(err)
		}

		body := `{"config":{"token":"******","extra":"e"},"merge":true}`
		code, _ := env.do(t, http.MethodPut, "/api/v1/config/cp", body, nil)
		if code != http.StatusOK {
			t.Fatalf("expected status 200, got %d", code)
		}

		_, got := env.do(t, http.MethodGet, "/api/v1/config/cp", "", nil)
		data := dataOf(t, got)
		config, _ := data["config"].(map[string]any)
		if config["url"] != "old" {
			t.Errorf("expected url kept, got %v", config["url"])
		}
		if config["token"] != "******" {
			t.Errorf("expected token masked (not overwritten by mask), got %v", config["token"])
		}
		if config["extra"] != "e" {
			t.Errorf("expected extra e, got %v", config["extra"])
		}
	})

	t.Run("merge with provider without config", func(t *testing.T) {
		env := newTestEnv(t)
		env.runtime.RegisterFactory(&stubFactory{name: "webhook"})
		if err := env.runtime.RegisterProvider("sp", &stubProvider{name: "sp", pType: "webhook"}); err != nil {
			t.Fatal(err)
		}

		body := `{"config":{"url":"merged"},"merge":true}`
		code, _ := env.do(t, http.MethodPut, "/api/v1/config/sp", body, nil)
		if code != http.StatusOK {
			t.Fatalf("expected status 200, got %d", code)
		}

		_, got := env.do(t, http.MethodGet, "/api/v1/config/sp", "", nil)
		data := dataOf(t, got)
		config, _ := data["config"].(map[string]any)
		if config["url"] != "merged" {
			t.Errorf("expected url merged, got %v", config["url"])
		}
	})

	t.Run("disabled provider stays disabled", func(t *testing.T) {
		env := newTestEnv(t)
		env.runtime.RegisterFactory(&stubFactory{name: "webhook"})
		err := env.runtime.RegisterProvider("dp", &configProvider{
			stubProvider: stubProvider{name: "dp", pType: "webhook"},
			config:       map[string]interface{}{"url": "old"},
		}, false)
		if err != nil {
			t.Fatal(err)
		}

		code, _ := env.do(t, http.MethodPut, "/api/v1/config/dp", `{"config":{"url":"new"}}`, nil)
		if code != http.StatusOK {
			t.Fatalf("expected status 200, got %d", code)
		}
		if env.runtime.IsEnabled("dp") {
			t.Error("expected dp to stay disabled after replace")
		}
	})

	t.Run("invalid body", func(t *testing.T) {
		env := newTestEnv(t)
		code, _ := env.do(t, http.MethodPut, "/api/v1/config/cp", "not-json", nil)
		if code != http.StatusBadRequest {
			t.Errorf("expected status 400, got %d", code)
		}
	})

	t.Run("provider not found", func(t *testing.T) {
		env := newTestEnv(t)
		code, _ := env.do(t, http.MethodPut, "/api/v1/config/missing", `{"config":{}}`, nil)
		if code != http.StatusNotFound {
			t.Errorf("expected status 404, got %d", code)
		}
	})

	t.Run("unknown factory", func(t *testing.T) {
		env := newTestEnv(t)
		err := env.runtime.RegisterProvider("np", &stubProvider{name: "np", pType: "notype"})
		if err != nil {
			t.Fatal(err)
		}
		code, resp := env.do(t, http.MethodPut, "/api/v1/config/np", `{"config":{}}`, nil)
		if code != http.StatusBadRequest {
			t.Fatalf("expected status 400, got %d", code)
		}
		if num(t, resp["code"]) != http.StatusBadRequest {
			t.Errorf("expected code 400, got %v", resp["code"])
		}
	})

	t.Run("factory error", func(t *testing.T) {
		env := newTestEnv(t)
		env.runtime.RegisterFactory(&stubFactory{name: "errtype", err: errors.New("cannot create")})
		err := env.runtime.RegisterProvider("ep", &stubProvider{name: "ep", pType: "errtype"})
		if err != nil {
			t.Fatal(err)
		}
		code, _ := env.do(t, http.MethodPut, "/api/v1/config/ep", `{"config":{}}`, nil)
		if code != http.StatusBadRequest {
			t.Errorf("expected status 400, got %d", code)
		}
	})

	t.Run("empty name", func(t *testing.T) {
		env := newTestEnv(t)
		h := env.server.handler
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPut, "/api/v1/config/", nil)
		h.HandleUpdateProviderConfig(w, req, "")
		if w.Code != http.StatusBadRequest {
			t.Errorf("expected status 400, got %d", w.Code)
		}
	})
}
