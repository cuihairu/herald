package auth

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
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
	// passwordStore persists rotated password hashes; nil keeps changes
	// process-local (they die with the process).
	passwordStore PasswordStore
	// adminUsers are the usernames seeded from config; the persisted
	// hash, when present, overrides their seeded password on startup.
	adminUsers []string
}

// PasswordStore persists password hashes beyond the process lifetime.
type PasswordStore interface {
	SetPasswordHash(username, hash string) error
	PasswordHash(username string) (string, bool, error)
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
	var adminUsers []string
	if config.AdminUser != nil {
		for username, password := range config.AdminUser {
			if username != "" && password != "" {
				_ = um.CreateDefaultUser(username, password)
				adminUsers = append(adminUsers, username)
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
		adminUsers:  adminUsers,
	}
}

// SetPasswordStore wires the persistence face for password hashes. When a
// store already holds a hash for a seeded admin, it overrides the config
// password — the rotated credential wins over the bootstrap one.
func (a *Auth) SetPasswordStore(ps PasswordStore) {
	a.passwordStore = ps
	if ps == nil {
		return
	}
	for _, username := range a.adminUsers {
		if hash, ok, err := ps.PasswordHash(username); err == nil && ok {
			_ = a.userManager.SetPasswordHash(username, hash)
		}
	}
}

// ChangePassword validates the old password, stores the new bcrypt hash
// and persists it through the password store when one is wired.
func (a *Auth) ChangePassword(username, oldPassword, newPassword string) error {
	if _, err := a.userManager.Authenticate(username, oldPassword); err != nil {
		return errInvalidOldPassword
	}
	if err := a.userManager.ChangePassword(username, oldPassword, newPassword); err != nil {
		return err
	}
	if a.passwordStore == nil {
		return nil
	}
	hash, _ := a.userManager.StoredHash(username)
	if err := a.passwordStore.SetPasswordHash(username, hash); err != nil {
		return errPersistFailed
	}
	return nil
}

// HandlePasswordChange serves POST /api/v1/auth/password with a Bearer
// token: {"old_password": ..., "new_password": ...}.
func (a *Auth) HandlePasswordChange(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
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

	var req PasswordChangeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		a.respondError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if req.NewPassword == "" {
		a.respondError(w, http.StatusBadRequest, "new password is required")
		return
	}

	if err := a.ChangePassword(claims.Username, req.OldPassword, req.NewPassword); err != nil {
		switch err {
		case errInvalidOldPassword:
			a.respondError(w, http.StatusBadRequest, "invalid old password")
		case errPersistFailed:
			a.respondError(w, http.StatusInternalServerError, "failed to persist password")
		default:
			a.respondError(w, http.StatusBadRequest, "password not accepted")
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"code": 0, "message": "ok"})
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
	// trial holds single-token buckets for keys that arrived while the
	// main table was at cap with no idle key to reclaim. Each such key
	// gets one granted attempt without being admitted to the main table
	// (which would evict an active bucket) and without letting a flood of
	// distinct keys obtain a full bucket. It is bounded by
	// loginLimiterMaxKeys and reclaimed by idle TTL or LRU.
	trial     map[string]*limiter.TokenBucket
	trialSeen map[string]time.Time
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
		keys:      make(map[string]*limiter.TokenBucket),
		rate:      rate,
		burst:     burst,
		enabled:   true,
		lastSeen:  make(map[string]time.Time),
		trial:     make(map[string]*limiter.TokenBucket),
		trialSeen: make(map[string]time.Time),
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
		// At the cap, reclaim only genuinely idle keys. Evicting an
		// active bucket would let a rotating-key flood reset somebody's
		// rate budget; refusing every new key would let the same flood
		// lock out legitimate users. If the table is full and active,
		// grant this unknown key a single trial attempt through the
		// bounded trial table without retaining it in the main table.
		if len(l.keys) >= loginLimiterMaxKeys {
			if !l.evictIdleLocked(loginLimiterIdleTTL) {
				return l.trialAllowLocked(key)
			}
		}
		b = limiter.NewTokenBucket(l.rate, l.burst)
		l.keys[key] = b
		now := time.Now()
		l.lastSeen[key] = now
	} else {
		l.lastSeen[key] = time.Now()
	}
	return b.Allow()
}

// trialAllowLocked handles an unknown key when the main table is at cap
// with no idle bucket to reclaim. The key gets a single-token trial bucket
// so a legitimate new user can still make one attempt; it is never placed
// in the main table, so no active bucket is evicted (and its rate budget
// reset) and a rotating-key flood cannot obtain a full bucket. The trial
// table is bounded by loginLimiterMaxKeys, reclaimed by idle TTL, and at
// capacity evicts its least-recently-used trial entries (which merely
// revokes a later second attempt for those keys, never a main-table budget).
// Callers hold the lock.
func (l *loginRateLimiter) trialAllowLocked(key string) bool {
	now := time.Now()
	tb, ok := l.trial[key]
	if !ok {
		// Reclaim idle entries only at capacity; the full scan is
		// amortized over the inserts that fill the table, so a sustained
		// flood does not pay an O(n) sweep per new key.
		if len(l.trial) >= loginLimiterMaxKeys {
			l.evictTrialIdleLocked(loginLimiterIdleTTL)
			if len(l.trial) >= loginLimiterMaxKeys {
				l.evictTrialLRUBatchLocked()
			}
		}
		tb = limiter.NewOnce()
		l.trial[key] = tb
	}
	l.trialSeen[key] = now
	return tb.Allow()
}

// evictTrialIdleLocked reclaims trial buckets that have seen no traffic for
// longer than ttl. Callers hold the lock.
func (l *loginRateLimiter) evictTrialIdleLocked(ttl time.Duration) {
	cutoff := time.Now().Add(-ttl)
	for k, seen := range l.trialSeen {
		if seen.Before(cutoff) {
			delete(l.trial, k)
			delete(l.trialSeen, k)
		}
	}
}

// evictTrialLRUBatchLocked drops the least-recently-used quarter of trial
// entries so the trial table stays bounded under a sustained flood. Evicting
// a batch amortizes the O(n) scan over the insertions that fill the table
// again, keeping per-key cost O(1) amortized. Callers hold the lock and have
// already confirmed the table is at capacity with no idle entry.
func (l *loginRateLimiter) evictTrialLRUBatchLocked() {
	type seen struct {
		key string
		at  time.Time
	}
	oldest := make([]seen, 0, len(l.trialSeen))
	for k, t := range l.trialSeen {
		oldest = append(oldest, seen{k, t})
	}
	if len(oldest) == 0 {
		return
	}
	sort.Slice(oldest, func(i, j int) bool { return oldest[i].at.Before(oldest[j].at) })
	n := len(oldest) / 4
	if n < 1 {
		n = 1
	}
	for _, s := range oldest[:n] {
		delete(l.trial, s.key)
		delete(l.trialSeen, s.key)
	}
}

// evictIdleLocked reclaims buckets that have seen no traffic for longer
// than ttl. Callers hold the lock. It is only invoked at the cap, so the
// extra scan is bounded by the table size and amortized over the insert
// that triggered it. It reports whether any entry was reclaimed; when
// nothing is idle it leaves the table untouched (never evicts an active
// bucket, which would reset its rate budget).
func (l *loginRateLimiter) evictIdleLocked(ttl time.Duration) bool {
	cutoff := time.Now().Add(-ttl)
	idle := make([]string, 0, len(l.keys))
	for k, seen := range l.lastSeen {
		if seen.Before(cutoff) {
			idle = append(idle, k)
		}
	}
	if len(idle) == 0 {
		return false
	}
	for _, k := range idle {
		delete(l.keys, k)
		delete(l.lastSeen, k)
	}
	return true
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

// PasswordChangeRequest is a password rotation request
type PasswordChangeRequest struct {
	OldPassword string `json:"old_password"`
	NewPassword string `json:"new_password"`
}

var (
	errInvalidOldPassword = errors.New("invalid old password")
	errPersistFailed      = errors.New("failed to persist password")
)

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
