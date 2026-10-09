package user

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// User represents a user
type User struct {
	ID        string    `json:"id"`
	Username  string    `json:"username"`
	Password  string    `json:"-"` // hashed password
	Role      string    `json:"role"`
	CreatedAt time.Time `json:"created_at"`
}

// Manager manages users
type Manager struct {
	users map[string]*User // username -> user
	mu    sync.RWMutex
}

// NewManager creates a new user manager
func NewManager() *Manager {
	return &Manager{
		users: make(map[string]*User),
	}
}

// CreateDefaultUser creates the default admin user
func (m *Manager) CreateDefaultUser(username, password string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.users[username]; exists {
		return fmt.Errorf("user already exists: %s", username)
	}

	// hashPassword fails only for passwords over bcrypt's 72-byte
	// input limit — the error is a real user error, not a placeholder.
	hashedPassword, err := hashPassword(password)
	if err != nil {
		return err
	}

	user := &User{
		ID:        generateID(),
		Username:  username,
		Password:  hashedPassword,
		Role:      "admin",
		CreatedAt: time.Now(),
	}

	m.users[username] = user
	return nil
}

// Authenticate validates a username and password. A match against a
// legacy-format stored hash transparently rehashes the password with
// bcrypt (lazy migration), so long-lived processes upgrade their stored
// credentials on the next successful login; a failed migration keeps the
// legacy hash and the next login retries it.
func (m *Manager) Authenticate(username, password string) (*User, error) {
	m.mu.RLock()
	user, exists := m.users[username]
	if !exists {
		m.mu.RUnlock()
		return nil, fmt.Errorf("user not found")
	}
	stored := user.Password
	m.mu.RUnlock()

	if !verifyPassword(stored, password) {
		return nil, fmt.Errorf("invalid password")
	}

	if !isBcryptHash(stored) {
		m.migratePassword(username, stored, password)
	}

	return user, nil
}

// migratePassword re-stores password as a bcrypt hash if the entry still
// carries the hash it had when the legacy-format match happened.
func (m *Manager) migratePassword(username, stored, password string) {
	upgraded, err := hashPassword(password)
	if err != nil {
		// bcrypt refuses inputs over 72 bytes; keep the legacy hash —
		// verification still works, only the upgrade is deferred.
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if u, ok := m.users[username]; ok && u.Password == stored {
		u.Password = upgraded
	}
}

// GetUser returns a user by username
func (m *Manager) GetUser(username string) (*User, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	user, exists := m.users[username]
	if !exists {
		return nil, fmt.Errorf("user not found")
	}

	return user, nil
}

// ChangePassword changes a user's password
func (m *Manager) ChangePassword(username, oldPassword, newPassword string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	user, exists := m.users[username]
	if !exists {
		return fmt.Errorf("user not found")
	}

	if !verifyPassword(user.Password, oldPassword) {
		return fmt.Errorf("invalid old password")
	}

	// hashPassword fails only for passwords over bcrypt's 72-byte
	// input limit — the error is a real user error, not a placeholder.
	hashedPassword, err := hashPassword(newPassword)
	if err != nil {
		return err
	}

	user.Password = hashedPassword
	return nil
}

// CreateUser creates a new user
func (m *Manager) CreateUser(username, password, role string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.users[username]; exists {
		return fmt.Errorf("user already exists: %s", username)
	}

	// hashPassword fails only for passwords over bcrypt's 72-byte
	// input limit — the error is a real user error, not a placeholder.
	hashedPassword, err := hashPassword(password)
	if err != nil {
		return err
	}

	user := &User{
		ID:        generateID(),
		Username:  username,
		Password:  hashedPassword,
		Role:      role,
		CreatedAt: time.Now(),
	}

	m.users[username] = user
	return nil
}

// ListUsers returns all users
func (m *Manager) ListUsers() []*User {
	m.mu.RLock()
	defer m.mu.RUnlock()

	users := make([]*User, 0, len(m.users))
	for _, user := range m.users {
		users = append(users, user)
	}
	return users
}

// DeleteUser deletes a user
func (m *Manager) DeleteUser(username string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.users[username]; !exists {
		return fmt.Errorf("user not found")
	}

	delete(m.users, username)
	return nil
}

// generateID generates a random ID
func generateID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return base64.URLEncoding.EncodeToString(b)
}

// bcryptCost balances login latency against offline-guessing cost; both
// hashing moments (startup seeding, login migration) are human-rate paths.
// Tests drop it to bcrypt.MinCost (see TestMain) to keep the suite fast.
var bcryptCost = 10

// hashPassword hashes with bcrypt. It fails only for passwords over
// bcrypt's 72-byte input limit; callers surface that as a user error.
func hashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	return string(hash), err
}

// isBcryptHash reports whether the stored hash is bcrypt format ("$2").
// Anything else is the legacy base64(password+salt) encoding.
func isBcryptHash(hashedPassword string) bool {
	return strings.HasPrefix(hashedPassword, "$2")
}

// verifyPassword checks a password against its stored hash in whichever
// format the hash carries: bcrypt since the 2026-10 swap, and the legacy
// base64(password+salt) encoding (recomputed from the salt in the hash
// tail) for hashes stored before it. A stored value without the legacy
// ".base64(salt)" tail, or whose tail is not valid base64, fails
// verification — there is no fallback that accepts any password.
func verifyPassword(hashedPassword, password string) bool {
	if isBcryptHash(hashedPassword) {
		return bcrypt.CompareHashAndPassword([]byte(hashedPassword), []byte(password)) == nil
	}
	idx := strings.LastIndex(hashedPassword, ".")
	if idx < 0 {
		return false
	}
	salt, err := base64.StdEncoding.DecodeString(hashedPassword[idx+1:])
	if err != nil {
		return false
	}
	combined := append([]byte(password), salt...)
	expected := base64.StdEncoding.EncodeToString(combined)
	return subtle.ConstantTimeCompare([]byte(expected), []byte(hashedPassword[:idx])) == 1
}
