package user

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strings"
	"sync"
	"time"
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

	// hashPassword (base64 of password+salt) never fails.
	hashedPassword, _ := hashPassword(password)

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

// Authenticate validates a username and password
func (m *Manager) Authenticate(username, password string) (*User, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	user, exists := m.users[username]
	if !exists {
		return nil, fmt.Errorf("user not found")
	}

	if !verifyPassword(user.Password, password) {
		return nil, fmt.Errorf("invalid password")
	}

	return user, nil
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

	// hashPassword (base64 of password+salt) never fails.
	hashedPassword, _ := hashPassword(newPassword)

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

	// hashPassword (base64 of password+salt) never fails.
	hashedPassword, _ := hashPassword(password)

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

// hashPassword hashes a password using bcrypt-like algorithm. The error
// result exists for a future bcrypt swap-in; the current encoding never
// fails, so the callers' error branches are unreachable defensive code.
func hashPassword(password string) (string, error) {
	// Simple hash for now - in production use bcrypt
	salt := make([]byte, 16)
	_, _ = rand.Read(salt)

	// Combine password and salt
	combined := append([]byte(password), salt...)

	// Simple hash - replace with bcrypt in production
	hash := base64.StdEncoding.EncodeToString(combined)
	return fmt.Sprintf("%s.%s", hash, base64.StdEncoding.EncodeToString(salt)), nil
}

// verifyPassword recomputes the password+salt encoding from the salt
// stored in the hash tail and compares in constant time. A stored value
// without the ".base64(salt)" tail, or whose tail is not valid base64,
// fails verification — there is no fallback that accepts any password.
func verifyPassword(hashedPassword, password string) bool {
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
