// Package auth handles authentication, device pairing, and RBAC for the gateway.
//
// This mirrors the auth logic in src/gateway/auth/ and src/gateway/server-auth.ts
// from the TypeScript codebase.
package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"
)

// Role represents a client role in the gateway RBAC system.
type Role string

const (
	RoleOperator Role = "operator"
	RoleViewer   Role = "viewer"
	RoleAgent    Role = "agent"
	RoleProbe    Role = "probe"
)

// Scope represents a permission scope granted to a client.
// These match the OperatorScope values in src/gateway/method-scopes.ts.
type Scope string

const (
	ScopeAdmin     Scope = "operator.admin"
	ScopeRead      Scope = "operator.read"
	ScopeWrite     Scope = "operator.write"
	ScopeApprovals Scope = "operator.approvals"
	ScopePairing   Scope = "operator.pairing"
)

// TokenClaims represents the claims extracted from a validated token.
type TokenClaims struct {
	DeviceID  string    `json:"deviceId"`
	Role      Role      `json:"role"`
	Scopes    []Scope   `json:"scopes"`
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

// HasScope checks whether the claims include the given scope.
func (c *TokenClaims) HasScope(scope Scope) bool {
	for _, s := range c.Scopes {
		if s == scope {
			return true
		}
	}
	return false
}

// DeviceRecord represents a paired device.
type DeviceRecord struct {
	ID        string    `json:"id"`
	PublicKey string    `json:"publicKey"`
	Name      string    `json:"name,omitempty"`
	PairedAt  time.Time `json:"pairedAt"`
	LastSeen  time.Time `json:"lastSeen"`
}

// Validator handles token validation and device management.
type Validator struct {
	mu      sync.RWMutex
	secret  []byte
	devices map[string]*DeviceRecord // deviceID -> record
}

// NewValidator creates a new auth validator with the given secret.
func NewValidator(secret []byte) *Validator {
	return &Validator{
		secret:  secret,
		devices: make(map[string]*DeviceRecord),
	}
}

// ValidateToken checks a token string and returns the claims if valid.
// Token format: hex(hmac-sha256(payload, secret)):payload
// where payload is: deviceId:role:scopes:issuedAtUnix
func (v *Validator) ValidateToken(token string) (*TokenClaims, error) {
	// The HMAC hex is always 64 chars (sha256 = 32 bytes = 64 hex).
	// Split at the first colon after the hex prefix.
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

	// Check expiration if set.
	if claims.IsExpired(time.Now()) {
		return nil, fmt.Errorf("token expired")
	}

	return claims, nil
}

// IssueToken creates a signed token for the given device and role.
// Returns an error if deviceID is empty or contains the delimiter character.
func (v *Validator) IssueToken(deviceID string, role Role, scopes []Scope) (string, error) {
	if deviceID == "" {
		return "", fmt.Errorf("deviceID is required")
	}
	if strings.ContainsRune(deviceID, ':') {
		return "", fmt.Errorf("deviceID must not contain ':'")
	}

	now := time.Now().Unix()
	scopeStr := joinScopes(scopes)
	payload := fmt.Sprintf("%s:%s:%s:%d", deviceID, role, scopeStr, now)
	sig := hex.EncodeToString(v.computeHMAC([]byte(payload)))
	return sig + ":" + payload, nil
}

// RegisterDevice adds or updates a paired device.
func (v *Validator) RegisterDevice(device DeviceRecord) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.devices[device.ID] = &device
}

// GetDevice returns a device by ID, or nil if not found.
func (v *Validator) GetDevice(id string) *DeviceRecord {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.devices[id]
}

// RemoveDevice unregisters a device.
func (v *Validator) RemoveDevice(id string) bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	if _, ok := v.devices[id]; ok {
		delete(v.devices, id)
		return true
	}
	return false
}

// ListDevices returns copies of all registered devices.
// The returned records are safe to read without synchronization.
func (v *Validator) ListDevices() []DeviceRecord {
	v.mu.RLock()
	defer v.mu.RUnlock()
	result := make([]DeviceRecord, 0, len(v.devices))
	for _, d := range v.devices {
		result = append(result, *d)
	}
	return result
}

// TouchDevice updates the LastSeen time for a device.
func (v *Validator) TouchDevice(id string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if d, ok := v.devices[id]; ok {
		d.LastSeen = time.Now()
	}
}

func (v *Validator) computeHMAC(data []byte) []byte {
	mac := hmac.New(sha256.New, v.secret)
	mac.Write(data)
	return mac.Sum(nil)
}

func parsePayload(payload string) (*TokenClaims, error) {
	// payload: deviceId:role:scopes:issuedAtUnix
	parts := strings.SplitN(payload, ":", 4)
	if len(parts) != 4 {
		return nil, fmt.Errorf("invalid token payload")
	}

	deviceID := parts[0]
	role := Role(parts[1])
	scopeStrs := strings.Split(parts[2], ",")
	issuedAtStr := parts[3]

	if deviceID == "" {
		return nil, fmt.Errorf("empty device ID in token")
	}

	var issuedAt int64
	if _, err := fmt.Sscanf(issuedAtStr, "%d", &issuedAt); err != nil {
		return nil, fmt.Errorf("invalid issuedAt: %w", err)
	}

	scopes := make([]Scope, 0, len(scopeStrs))
	for _, s := range scopeStrs {
		s = strings.TrimSpace(s)
		if s != "" {
			scopes = append(scopes, Scope(s))
		}
	}

	return &TokenClaims{
		DeviceID: deviceID,
		Role:     role,
		Scopes:   scopes,
		IssuedAt: time.Unix(issuedAt, 0),
	}, nil
}

func joinScopes(scopes []Scope) string {
	strs := make([]string, len(scopes))
	for i, s := range scopes {
		strs[i] = string(s)
	}
	return strings.Join(strs, ",")
}

// DefaultScopes returns the default scopes for the given role.
func DefaultScopes(role Role) []Scope {
	return rolePermissions[role]
}

// DefaultScopesStrings returns the default scopes for a role as string slices.
func DefaultScopesStrings(role Role) []string {
	scopes := DefaultScopes(role)
	result := make([]string, len(scopes))
	for i, s := range scopes {
		result[i] = string(s)
	}
	return result
}

// CheckPermission verifies that a role+scopes combination allows the given action scope.
func CheckPermission(role Role, scopes []Scope, required Scope) error {
	for _, s := range scopes {
		if s == required || s == ScopeAdmin {
			return nil
		}
	}
	// Fallback: check role defaults.
	if defaults, ok := rolePermissions[role]; ok {
		for _, s := range defaults {
			if s == required || s == ScopeAdmin {
				return nil
			}
		}
	}
	return fmt.Errorf("role %q lacks scope %q", role, required)
}
