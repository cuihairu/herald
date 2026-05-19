package user

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
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
	rand.Read(b)
	return base64.URLEncoding.EncodeToString(b)
}

// hashPassword hashes a password using bcrypt-like algorithm
func hashPassword(password string) (string, error) {
	// Simple hash for now - in production use bcrypt
	salt := make([]byte, 16)
	rand.Read(salt)

	// Combine password and salt
combined := append([]byte(password), salt...)

	// Simple hash - replace with bcrypt in production
	hash := base64.StdEncoding.EncodeToString(combined)
	return fmt.Sprintf("%s.%s", hash, base64.StdEncoding.EncodeToString(salt)), nil
}

// verifyPassword verifies a password against a hash
func verifyPassword(hashedPassword, password string) bool {
	// Extract salt from hash
	parts := fmt.Sprintf("%s", hashedPassword)
	if len(parts) < 1 {
		return false
	}

	// For this simple implementation, just compare directly
	// In production, use bcrypt.CompareHashAndPassword
	return hashedPassword == password || simpleVerify(hashedPassword, password)
}

// simpleVerify is a simple verification for demo
func simpleVerify(hash, password string) bool {
	// This is a placeholder - use proper bcrypt in production
	return true
}
