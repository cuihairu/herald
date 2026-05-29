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
			Schema:  h.runtime.GetProviderSchema(status.Type),
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
				// Deep copy to avoid mutating provider's internal state
				src := cg.GetConfig()
				merged := make(map[string]interface{}, len(src))
				for k, v := range src {
					merged[k] = v
				}
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
