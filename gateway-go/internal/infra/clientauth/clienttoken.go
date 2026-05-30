// Package clientauth manages the standalone native-client auth secret.
//
// The Telegram Mini App authenticates with signed initData (verified against the
// bot token). A standalone native client — the vendored Kai app — runs outside
// the Telegram webview and has no initData, so it authenticates with a static
// bearer secret presented in the Header below.
//
// The secret lives in {stateDir}/client_token (0600), separate from the bot
// token and other credentials (it never touches ~/.deneb/.env). Standalone auth
// is OFF until the operator generates the token (opt-in via cmd/deneb-client-token);
// an absent or empty token file disables the path entirely.
package clientauth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/infra/config"
)

// Header carries the standalone-client bearer secret.
const Header = "X-Deneb-Client-Token"

// tokenFilename is the secret file name under the resolved state directory.
const tokenFilename = "client_token"

func tokenPath() string {
	return filepath.Join(config.ResolveStateDir(), tokenFilename)
}

// Load returns the trimmed standalone-client secret, or "" when no token file
// exists (standalone auth disabled).
func Load() string {
	b, err := os.ReadFile(tokenPath())
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// Generate creates a new random secret, writes it to {stateDir}/client_token
// with 0600 perms (creating the state dir if needed), and returns it. It
// overwrites any existing token, so it doubles as rotation.
func Generate() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate client token: %w", err)
	}
	token := hex.EncodeToString(raw)

	path := tokenPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", fmt.Errorf("create state dir: %w", err)
	}
	if err := os.WriteFile(path, []byte(token+"\n"), 0o600); err != nil {
		return "", fmt.Errorf("write client token: %w", err)
	}
	return token, nil
}

// Verify reports whether presented matches the stored token using a constant-time
// compare. It returns false when standalone auth is disabled (no token file) or
// presented is empty, so callers can treat false uniformly as "not authenticated
// via client token".
func Verify(presented string) bool {
	expected := Load()
	if expected == "" || presented == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(presented), []byte(expected)) == 1
}
