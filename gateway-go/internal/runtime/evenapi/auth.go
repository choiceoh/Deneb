package evenapi

import (
	"crypto/subtle"
	"os"
	"strings"
)

// EnvBridgeToken is the operator-set bearer for Even G2 Custom AI / Glance.
// Distinct from the native X-Deneb-Client-Token so glasses ingress can be
// rotated without rotating the phone client secret.
const EnvBridgeToken = "DENEB_EVEN_G2_BRIDGE_TOKEN"

// LoadBridgeToken returns the trimmed bridge bearer, or "" when unset
// (feature disabled).
func LoadBridgeToken() string {
	return strings.TrimSpace(os.Getenv(EnvBridgeToken))
}

// VerifyBridgeToken reports whether presented matches the configured bridge
// token using a constant-time compare. Empty expected or presented → false.
func VerifyBridgeToken(presented string) bool {
	expected := LoadBridgeToken()
	if expected == "" || presented == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(presented), []byte(expected)) == 1
}

// ParseBearer extracts the token from an Authorization: Bearer … header.
func ParseBearer(authorization string) string {
	authorization = strings.TrimSpace(authorization)
	const prefix = "Bearer "
	if len(authorization) < len(prefix) || !strings.EqualFold(authorization[:len(prefix)], prefix) {
		return ""
	}
	return strings.TrimSpace(authorization[len(prefix):])
}
