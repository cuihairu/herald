package jwt

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Claims represents JWT claims
type Claims struct {
	UserID   string `json:"user_id"`
	Username string `json:"username"`
	Role     string `json:"role"`
	Exp      int64  `json:"exp"`
	Iat      int64  `json:"iat"`
}

// Manager manages JWT tokens
type Manager struct {
	secretKey string
}

// NewManager creates a new JWT manager
func NewManager(secretKey string) *Manager {
	if secretKey == "" {
		secretKey = "herald-default-secret-key-change-in-production"
	}
	return &Manager{
		secretKey: secretKey,
	}
}

// Generate generates a JWT token
func (m *Manager) Generate(userID, username, role string) (string, error) {
	now := time.Now()
	exp := now.Add(24 * time.Hour) // 24 hour expiration

	claims := &Claims{
		UserID:   userID,
		Username: username,
		Role:     role,
		Exp:      exp.Unix(),
		Iat:      now.Unix(),
	}

	// Encode claims
	claimsJSON, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}

	claimsEncoded := base64.URLEncoding.EncodeToString(claimsJSON)

	// Create signature
	payload := fmt.Sprintf("%s.%s", m.header(), claimsEncoded)
	signature := m.sign(payload)

	token := fmt.Sprintf("%s.%s", payload, signature)
	return token, nil
}

// Validate validates a JWT token
func (m *Manager) Validate(token string) (*Claims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("invalid token format")
	}

	header, claimsEncoded, signature := parts[0], parts[1], parts[2]

	// Verify signature
	payload := fmt.Sprintf("%s.%s", header, claimsEncoded)
	expectedSignature := m.sign(payload)

	if !hmac.Equal([]byte(signature), []byte(expectedSignature)) {
		return nil, fmt.Errorf("invalid signature")
	}

	// Decode claims
	claimsJSON, err := base64.URLEncoding.DecodeString(claimsEncoded)
	if err != nil {
		return nil, fmt.Errorf("invalid claims encoding")
	}

	var claims Claims
	if err := json.Unmarshal(claimsJSON, &claims); err != nil {
		return nil, fmt.Errorf("invalid claims json")
	}

	// Check expiration
	if time.Now().Unix() > claims.Exp {
		return nil, fmt.Errorf("token expired")
	}

	return &claims, nil
}

// Refresh refreshes a token
func (m *Manager) Refresh(token string) (string, error) {
	claims, err := m.Validate(token)
	if err != nil {
		return "", err
	}

	return m.Generate(claims.UserID, claims.Username, claims.Role)
}

// header returns the JWT header
func (m *Manager) header() string {
	header := map[string]string{
		"alg": "HS256",
		"typ": "JWT",
	}

	headerJSON, _ := json.Marshal(header)
	return base64.URLEncoding.EncodeToString(headerJSON)
}

// sign creates a signature for the payload
func (m *Manager) sign(payload string) string {
	h := hmac.New(sha256.New, []byte(m.secretKey))
	h.Write([]byte(payload))
	return base64.URLEncoding.EncodeToString(h.Sum(nil))
}
