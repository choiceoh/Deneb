// Package auth handles token-based authentication for the gateway.
//
// Single-user deployment: only operator and agent roles exist.
// No multi-device management, no scope-based RBAC.
package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

// Role represents a client role in the gateway.
type Role string

const (
	RoleOperator Role = "operator"
	RoleAgent    Role = "agent"
)

// TokenClaims represents the claims extracted from a validated token.
type TokenClaims struct {
	DeviceID  string    `json:"deviceId"`
	Role      Role      `json:"role"`
	IssuedAt  time.Time `json:"issuedAt"`
	ExpiresAt time.Time `json:"expiresAt,omitempty"`
}

// IsExpired checks whether the token claims have expired.
func (c *TokenClaims) IsExpired(now time.Time) bool {
	if c.ExpiresAt.IsZero() {
		return false
	}
	return now.After(c.ExpiresAt)
}

// Validator handles token validation.
type Validator struct {
	secret []byte
}

// NewValidator creates a new auth validator with the given secret.
func NewValidator(secret []byte) *Validator {
	return &Validator{secret: secret}
}

// ValidateToken checks a token string and returns the claims if valid.
// Token format: hex(hmac-sha256(payload, secret)):payload
// where payload is: deviceId:role:scopes:issuedAtUnix
func (v *Validator) ValidateToken(token string) (*TokenClaims, error) {
	// The HMAC hex is always 64 chars (sha256 = 32 bytes = 64 hex).
	if len(token) < 65 || token[64] != ':' {
		return nil, fmt.Errorf("invalid token format")
	}

	sig, err := hex.DecodeString(token[:64])
	if err != nil {
		return nil, fmt.Errorf("invalid token signature encoding")
	}

	payload := token[65:]
	expected := v.computeHMAC([]byte(payload))
	if !hmac.Equal(sig, expected) {
		return nil, fmt.Errorf("invalid token signature")
	}

	claims, err := parsePayload(payload)
	if err != nil {
		return nil, err
	}

	if claims.IsExpired(time.Now()) {
		return nil, fmt.Errorf("token expired")
	}

	return claims, nil
}

func (v *Validator) computeHMAC(data []byte) []byte {
	mac := hmac.New(sha256.New, v.secret)
	mac.Write(data)
	return mac.Sum(nil)
}

// parsePayload parses the token payload: deviceId:role:scopes:issuedAtUnix
// The scopes field is accepted for wire compatibility but ignored.
func parsePayload(payload string) (*TokenClaims, error) {
	parts := strings.SplitN(payload, ":", 4)
	if len(parts) != 4 {
		return nil, fmt.Errorf("invalid token payload")
	}

	deviceID := parts[0]
	role := Role(parts[1])
	// parts[2] is scopes — accepted for wire compat, ignored.
	issuedAtStr := parts[3]

	if deviceID == "" {
		return nil, fmt.Errorf("empty device ID in token")
	}

	var issuedAt int64
	if _, err := fmt.Sscanf(issuedAtStr, "%d", &issuedAt); err != nil {
		return nil, fmt.Errorf("invalid issuedAt: %w", err)
	}

	return &TokenClaims{
		DeviceID: deviceID,
		Role:     role,
		IssuedAt: time.Unix(issuedAt, 0),
	}, nil
}
