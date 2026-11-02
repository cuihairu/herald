package auth

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/cuihairu/herald/core/jwt"
	"github.com/cuihairu/herald/core/limiter"
	"github.com/cuihairu/herald/core/user"
)

// Auth handles authentication
type Auth struct {
	userManager *user.Manager
	jwtManager  *jwt.Manager
	apiKeys     map[string]string
	enabled     bool
	loginLimits *loginRateLimiter
}

// Config is the auth configuration
type Config struct {
	Enabled   bool              `yaml:"enabled"`
	APIKeys   map[string]string `yaml:"api_keys"` // key -> description
	SecretKey string            `yaml:"secret_key"`
	AdminUser map[string]string `yaml:"admin_user"` // username -> password
	// LoginRate is the maximum login attempts per minute per client+username
	// key. Zero uses the built-in default (5/min). A negative value disables
	// login rate limiting (not recommended).
	LoginRate  float64 `yaml:"login_rate"`
	LoginBurst int     `yaml:"login_burst"`
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
				_ = um.CreateDefaultUser(username, password)
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
		loginLimits: newLoginRateLimiter(config.LoginRate, config.LoginBurst),
	}
}

// loginRateLimiter buckets one token per (clientIP, username) key. A burst
// covers repeated typos; beyond it a 429 is returned without running bcrypt.
type loginRateLimiter struct {
	mu      sync.Mutex
	keys    map[string]*limiter.TokenBucket
	rate    float64
	burst   int
	enabled bool
	// lastSeen tracks the most recent allow for each key so idle entries
	// can be evicted. Without it the map grows one bucket per unique
	// clientIP|username key forever — a rotating-username attacker can
	// exhaust memory.
	lastSeen map[string]time.Time
}

func newLoginRateLimiter(rate float64, burst int) *loginRateLimiter {
	if rate < 0 {
		return &loginRateLimiter{enabled: false}
	}
	if rate == 0 {
		rate = 5.0 / 60.0 // 5 per minute
	}
	if burst <= 0 {
		burst = 5
	}
	return &loginRateLimiter{
		keys:     make(map[string]*limiter.TokenBucket),
		rate:     rate,
		burst:    burst,
		enabled:  true,
		lastSeen: make(map[string]time.Time),
	}
}

const (
	// loginLimiterMaxKeys bounds the bucket table so a rotating-username
	// or spoofed-IP attacker cannot grow it without bound.
	loginLimiterMaxKeys = 10000
	// loginLimiterIdleTTL is how long an unused key keeps its bucket
	// before a sweep reclaims it.
	loginLimiterIdleTTL = 10 * time.Minute
)

func (l *loginRateLimiter) allow(key string) bool {
	if l == nil || !l.enabled {
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	b, ok := l.keys[key]
	if !ok {
		b = limiter.NewTokenBucket(l.rate, l.burst)
		l.keys[key] = b
		now := time.Now()
		l.lastSeen[key] = now
		// Amortized cleanup: when the table reaches the cap, drop the
		// longest-idle entries in one pass instead of growing forever.
		if len(l.keys) > loginLimiterMaxKeys {
			l.evictIdleLocked(loginLimiterIdleTTL)
		}
	} else {
		l.lastSeen[key] = time.Now()
	}
	return b.Allow()
}

// evictIdleLocked reclaims buckets that have seen no traffic for longer
// than ttl. Callers hold the lock. It is only invoked at the cap, so the
// extra scan is bounded by the table size and amortized over the insert
// that triggered it.
func (l *loginRateLimiter) evictIdleLocked(ttl time.Duration) {
	cutoff := time.Now().Add(-ttl)
	idle := make([]string, 0, len(l.keys))
	for k, seen := range l.lastSeen {
		if seen.Before(cutoff) {
			idle = append(idle, k)
		}
	}
	if len(idle) == 0 {
		// Nothing idle: fall back to dropping the oldest quarter so the
		// table stays bounded even under a burst of distinct active keys.
		type kv struct {
			k string
			t time.Time
		}
		all := make([]kv, 0, len(l.keys))
		for k, seen := range l.lastSeen {
			all = append(all, kv{k, seen})
		}
		sort.Slice(all, func(i, j int) bool { return all[i].t.Before(all[j].t) })
		drop := len(all) / 4
		if drop < 1 {
			drop = 1
		}
		idle = make([]string, 0, drop)
		for i := 0; i < drop; i++ {
			idle = append(idle, all[i].k)
		}
	}
	for _, k := range idle {
		delete(l.keys, k)
		delete(l.lastSeen, k)
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
		if constantTimeEqual(apiKey, validKey) {
			return true
		}
	}
	return false
}

// constantTimeEqual compares two bearer secrets without early-exiting on a
// length or prefix difference: both sides are hashed to a fixed 32 bytes
// before the constant-time compare, so a timing oracle learns nothing about
// the stored secret's length or contents.
func constantTimeEqual(a, b string) bool {
	ha := sha256.Sum256([]byte(a))
	hb := sha256.Sum256([]byte(b))
	return subtle.ConstantTimeCompare(ha[:], hb[:]) == 1
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

	// Rate limit by client IP + username before running bcrypt. A 429
	// avoids CPU-bound password hashing on the public login endpoint.
	key := clientIP(r) + "|" + req.Username
	if !a.loginLimits.allow(key) {
		w.Header().Set("Retry-After", "60")
		a.respondError(w, http.StatusTooManyRequests, "too many login attempts, retry later")
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
	_ = json.NewEncoder(w).Encode(&LoginResponse{
		Token: token,
		User: &User{
			ID:       u.ID,
			Username: u.Username,
			Role:     u.Role,
		},
	})
}

// clientIP extracts the remote address host, falling back to a constant for
// tests that do not set RemoteAddr.
func clientIP(r *http.Request) string {
	if r.RemoteAddr != "" {
		if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
			return host
		}
		return r.RemoteAddr
	}
	return "unknown"
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
	_ = json.NewEncoder(w).Encode(map[string]string{"token": newToken})
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
	_ = json.NewEncoder(w).Encode(&User{
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

	// Cookies are not an authentication channel: the token travels only in
	// the Authorization header. No code path sets herald_token, and a cookie
	// would be attachable to a cross-site request (CSRF) — so one is ignored
	// rather than honoured.
	return ""
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

	return ""
}

// unauthorized sends an unauthorized response
func (a *Auth) unauthorized(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", `Bearer realm="herald"`)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = w.Write([]byte(`{"code": 401, "message": "unauthorized"}`))
}

// respondError sends an error response
func (a *Auth) respondError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
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
