package auth

import (
	"crypto/subtle"
	"net/http"
	"strings"
)

// Auth handles API authentication
type Auth struct {
	apiKeys map[string]string // key -> user
	enabled bool
}

// Config is the auth configuration
type Config struct {
	Enabled bool              `yaml:"enabled"`
	APIKeys map[string]string `yaml:"api_keys"` // key -> description
}

// New creates a new auth instance
func New(config *Config) *Auth {
	if config == nil {
		return &Auth{
			apiKeys: make(map[string]string),
			enabled: false,
		}
	}

	return &Auth{
		apiKeys: config.APIKeys,
		enabled: config.Enabled,
	}
}

// Middleware returns an authentication middleware
func (a *Auth) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip auth if disabled
		if !a.enabled {
			next.ServeHTTP(w, r)
			return
		}

		// Get API key from header or query
		apiKey := a.getAPIKey(r)
		if apiKey == "" {
			a.unauthorized(w)
			return
		}

		// Validate API key
		if !a.validateKey(apiKey) {
			a.unauthorized(w)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// getAPIKey extracts the API key from the request
func (a *Auth) getAPIKey(r *http.Request) string {
	// Try Authorization header: Bearer <token>
	auth := r.Header.Get("Authorization")
	if auth != "" {
		parts := strings.SplitN(auth, " ", 2)
		if len(parts) == 2 && strings.ToLower(parts[0]) == "bearer" {
			return parts[1]
		}
	}

	// Try X-API-Key header
	if key := r.Header.Get("X-API-Key"); key != "" {
		return key
	}

	// Try query parameter
	return r.URL.Query().Get("api_key")
}

// validateKey validates an API key using constant-time comparison
func (a *Auth) validateKey(key string) bool {
	for validKey := range a.apiKeys {
		if subtle.ConstantTimeCompare([]byte(key), []byte(validKey)) == 1 {
			return true
		}
	}
	return false
}

// unauthorized sends an unauthorized response
func (a *Auth) unauthorized(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", `Bearer realm="herald"`)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	w.Write([]byte(`{"code": 401, "message": "unauthorized"}`))
}

// IsEnabled returns whether auth is enabled
func (a *Auth) IsEnabled() bool {
	return a.enabled
}

// AddKey adds a new API key
func (a *Auth) AddKey(key, description string) {
	if a.apiKeys == nil {
		a.apiKeys = make(map[string]string)
	}
	a.apiKeys[key] = description
}

// RemoveKey removes an API key
func (a *Auth) RemoveKey(key string) {
	delete(a.apiKeys, key)
}
