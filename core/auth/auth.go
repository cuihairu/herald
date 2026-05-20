package auth

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/cuihairu/herald/core/jwt"
	"github.com/cuihairu/herald/core/user"
)

// Auth handles authentication
type Auth struct {
	userManager *user.Manager
	jwtManager  *jwt.Manager
	apiKeys     map[string]string
	enabled     bool
}

// Config is the auth configuration
type Config struct {
	Enabled   bool              `yaml:"enabled"`
	APIKeys   map[string]string `yaml:"api_keys"` // key -> description
	SecretKey string            `yaml:"secret_key"`
	AdminUser map[string]string `yaml:"admin_user"` // username -> password
}

// New creates a new auth instance
func New(config *Config) *Auth {
	if config == nil {
		return &Auth{
			userManager: user.NewManager(),
			jwtManager:  jwt.NewManager(""),
			apiKeys:     make(map[string]string),
			enabled:     false,
		}
	}

	um := user.NewManager()
	jm := jwt.NewManager(config.SecretKey)

	// Create default admin user if configured
	if config.AdminUser != nil {
		for username, password := range config.AdminUser {
			if username != "" && password != "" {
				um.CreateDefaultUser(username, password)
			}
		}
	}

	apiKeys := config.APIKeys
	if apiKeys == nil {
		apiKeys = make(map[string]string)
	}

	return &Auth{
		userManager: um,
		jwtManager:  jm,
		apiKeys:     apiKeys,
		enabled:     config.Enabled,
	}
}

// LoginRequest is a login request
type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// LoginResponse is a login response
type LoginResponse struct {
	Token string `json:"token"`
	User  *User  `json:"user"`
}

// User is a public user info
type User struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Role     string `json:"role"`
}

// Middleware returns an authentication middleware
func (a *Auth) Middleware(next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip auth if disabled
		if !a.enabled {
			next.ServeHTTP(w, r)
			return
		}

		// Try API key authentication first
		if a.authenticateAPIKey(r) {
			next.ServeHTTP(w, r)
			return
		}

		// Try JWT authentication
		if a.authenticateJWT(r) {
			next.ServeHTTP(w, r)
			return
		}

		a.unauthorized(w)
	})
}

// authenticateAPIKey authenticates using API key
func (a *Auth) authenticateAPIKey(r *http.Request) bool {
	apiKey := a.getAPIKey(r)
	if apiKey == "" {
		return false
	}

	for validKey := range a.apiKeys {
		if apiKey == validKey {
			return true
		}
	}
	return false
}

// authenticateJWT authenticates using JWT token
func (a *Auth) authenticateJWT(r *http.Request) bool {
	token := a.getJWTToken(r)
	if token == "" {
		return false
	}

	_, err := a.jwtManager.Validate(token)
	return err == nil
}

// HandleLogin handles login requests
func (a *Auth) HandleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		a.respondError(w, http.StatusBadRequest, "invalid request")
		return
	}

	// Authenticate user
	u, err := a.userManager.Authenticate(req.Username, req.Password)
	if err != nil {
		a.respondError(w, http.StatusUnauthorized, "invalid username or password")
		return
	}

	// Generate token
	token, err := a.jwtManager.Generate(u.ID, u.Username, u.Role)
	if err != nil {
		a.respondError(w, http.StatusInternalServerError, "failed to generate token")
		return
	}

	// Send response
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(&LoginResponse{
		Token: token,
		User: &User{
			ID:       u.ID,
			Username: u.Username,
			Role:     u.Role,
		},
	})
}

// HandleRefresh handles token refresh requests
func (a *Auth) HandleRefresh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	token := a.getJWTToken(r)
	if token == "" {
		a.respondError(w, http.StatusUnauthorized, "missing token")
		return
	}

	newToken, err := a.jwtManager.Refresh(token)
	if err != nil {
		a.respondError(w, http.StatusUnauthorized, "invalid token")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"token": newToken})
}

// HandleMe returns the current user info
func (a *Auth) HandleMe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	token := a.getJWTToken(r)
	if token == "" {
		a.respondError(w, http.StatusUnauthorized, "missing token")
		return
	}

	claims, err := a.jwtManager.Validate(token)
	if err != nil {
		a.respondError(w, http.StatusUnauthorized, "invalid token")
		return
	}

	u, err := a.userManager.GetUser(claims.Username)
	if err != nil {
		a.respondError(w, http.StatusNotFound, "user not found")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(&User{
		ID:       u.ID,
		Username: u.Username,
		Role:     u.Role,
	})
}

// getJWTToken extracts the JWT token from the request
func (a *Auth) getJWTToken(r *http.Request) string {
	// Try Authorization header: Bearer <token>
	auth := r.Header.Get("Authorization")
	if auth != "" {
		parts := strings.SplitN(auth, " ", 2)
		if len(parts) == 2 && strings.ToLower(parts[0]) == "bearer" {
			return parts[1]
		}
	}

	// Try cookie
	if cookie, err := r.Cookie("herald_token"); err == nil {
		return cookie.Value
	}

	return r.URL.Query().Get("token")
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

// unauthorized sends an unauthorized response
func (a *Auth) unauthorized(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", `Bearer realm="herald"`)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	w.Write([]byte(`{"code": 401, "message": "unauthorized"}`))
}

// respondError sends an error response
func (a *Auth) respondError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"code":    status,
		"message": message,
	})
}

// IsEnabled returns whether auth is enabled
func (a *Auth) IsEnabled() bool {
	return a.enabled
}

// GetUserManager returns the user manager
func (a *Auth) GetUserManager() *user.Manager {
	return a.userManager
}
