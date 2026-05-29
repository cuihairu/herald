package api

import (
	"encoding/json"
	"net/http"

	"github.com/cuihairu/herald/core"
)

// HandleProviders handles providers requests
func (h *Handler) HandleProviders(w http.ResponseWriter, r *http.Request) {
	statuses := h.runtime.GetProviderStatus()
	h.respondJSON(w, &Response{
		Code:    0,
		Message: "ok",
		Data: map[string]interface{}{
			"providers": statuses,
		},
	})
}

// HandleEnableProviderWithName enables a provider by name
func (h *Handler) HandleEnableProviderWithName(w http.ResponseWriter, r *http.Request, name string) {
	if err := h.runtime.Enable(name); err != nil {
		h.respondError(w, http.StatusNotFound, err.Error())
		return
	}
	h.respondJSON(w, &Response{Code: 0, Message: "provider enabled"})
}

// HandleDisableProviderWithName disables a provider by name
func (h *Handler) HandleDisableProviderWithName(w http.ResponseWriter, r *http.Request, name string) {
	if err := h.runtime.Disable(name); err != nil {
		h.respondError(w, http.StatusNotFound, err.Error())
		return
	}
	h.respondJSON(w, &Response{Code: 0, Message: "provider disabled"})
}

// ProviderConfigResponse is a provider config response
type ProviderConfigResponse struct {
	Name    string                 `json:"name"`
	Type    string                 `json:"type"`
	Enabled bool                   `json:"enabled"`
	Config  map[string]interface{} `json:"config"`
	Schema  map[string]string      `json:"schema,omitempty"`
}

// HandleProviderConfig handles provider config requests
func (h *Handler) HandleProviderConfig(w http.ResponseWriter, r *http.Request, name string) {
	if name == "" {
		h.respondError(w, http.StatusBadRequest, "provider name is required")
		return
	}

	provider, err := h.runtime.GetProvider(name)
	if err != nil {
		h.respondError(w, http.StatusNotFound, err.Error())
		return
	}

	// Read actual config from provider if it exposes one
	config := make(map[string]interface{})
	if cg, ok := provider.(interface{ GetConfig() map[string]interface{} }); ok {
		config = core.MaskConfig(cg.GetConfig())
	}

	status := provider.Status()
	h.respondJSON(w, &Response{
		Code:    0,
		Message: "ok",
		Data: &ProviderConfigResponse{
			Name:    name,
			Type:    status.Type,
			Enabled: h.runtime.IsEnabled(name),
			Config:  config,
			Schema:  getProviderSchema(name),
		},
	})
}

// UpdateProviderConfigRequest is a request to update provider config
type UpdateProviderConfigRequest struct {
	Config map[string]interface{} `json:"config"`
	Merge  bool                   `json:"merge,omitempty"`
}

// HandleUpdateProviderConfig handles provider config update requests
func (h *Handler) HandleUpdateProviderConfig(w http.ResponseWriter, r *http.Request, name string) {
	if name == "" {
		h.respondError(w, http.StatusBadRequest, "provider name is required")
		return
	}

	var req UpdateProviderConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.respondError(w, http.StatusBadRequest, "invalid request")
		return
	}

	config := req.Config

	// Merge with existing config if requested
	if req.Merge {
		if existing, err := h.runtime.GetProvider(name); err == nil {
			if cg, ok := existing.(interface{ GetConfig() map[string]interface{} }); ok {
				merged := cg.GetConfig()
				for k, v := range req.Config {
					// Don't overwrite with masked values
					if s, ok := v.(string); !ok || s != "******" {
						merged[k] = v
					}
				}
				config = merged
			}
		}
	}

	rt := h.runtime
	providerType, err := rt.GetProviderType(name)
	if err != nil {
		h.respondError(w, http.StatusNotFound, err.Error())
		return
	}

	provider, err := rt.CreateProvider(providerType, config)
	if err != nil {
		h.respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	wasEnabled := rt.IsEnabled(name)
	if err := rt.ReplaceProvider(name, provider); err != nil {
		h.respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if wasEnabled {
		_ = rt.Enable(name)
	}

	h.respondJSON(w, &Response{Code: 0, Message: "provider config updated"})
}

// getProviderSchema returns the config schema for a provider
func getProviderSchema(name string) map[string]string {
	schemas := map[string]map[string]string{
		"telegram":   {"token": "string", "chat_id": "string"},
		"feishu":     {"webhook_url": "string"},
		"wecom":      {"webhook_url": "string"},
		"dingtalk":   {"access_token": "string", "secret": "string"},
		"slack":      {"webhook_url": "string"},
		"discord":    {"webhook_url": "string"},
		"email":      {"host": "string", "port": "number", "username": "string", "password": "string", "from": "string"},
		"webhook":    {"url": "string"},
		"wechat":     {"service": "string", "send_key": "string", "token": "string", "app_token": "string", "uid": "string"},
		"wechatmp":   {"app_id": "string", "app_secret": "string", "template_id": "string", "default_url": "string"},
		"aliyunsms":  {"access_key_id": "string", "access_key_secret": "string", "sign_name": "string"},
		"tencentsms": {"secret_id": "string", "secret_key": "string", "app_id": "string", "sign_name": "string"},
		"neteasesms": {"app_key": "string", "app_secret": "string"},
	}
	if schema, ok := schemas[name]; ok {
		return schema
	}
	return map[string]string{"config": "object"}
}
