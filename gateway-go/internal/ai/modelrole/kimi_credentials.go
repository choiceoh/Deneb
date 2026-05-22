package modelrole

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// defaultKimiCLIFile is the location, relative to the user's home
// directory, of the token cache the official Kimi CLI writes after
// `/login`. Override with the KIMI_CREDENTIALS_FILE env var if the CLI
// stores it elsewhere.
const defaultKimiCLIFile = ".kimi/credentials/kimi-code.json"

// kimiCLIToken reads the OAuth access token cached by the official Kimi
// CLI. Returns "" when the file is missing or unreadable, so the caller
// can fall back to KIMI_API_KEY. The file is read fresh on every call, so
// a token refreshed by re-running the Kimi CLI is picked up without
// restarting the gateway.
func kimiCLIToken() string {
	path := strings.TrimSpace(os.Getenv("KIMI_CREDENTIALS_FILE"))
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		path = filepath.Join(home, defaultKimiCLIFile)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}

	// The Kimi CLI writes a JSON object; the access token is the
	// `access_token` field. Tolerate a couple of alternate spellings.
	var creds struct {
		AccessToken string `json:"access_token"`
		Token       string `json:"token"`
		AccessTok2  string `json:"accessToken"`
	}
	if err := json.Unmarshal(data, &creds); err != nil {
		return ""
	}
	for _, tok := range []string{creds.AccessToken, creds.Token, creds.AccessTok2} {
		if t := strings.TrimSpace(tok); t != "" {
			return t
		}
	}
	return ""
}

// kimiToken returns the Kimi Code credential, preferring the OAuth token
// cached by the official Kimi CLI and falling back to the KIMI_API_KEY
// env var for operators who use a plain API key instead of the
// subscription. Returns "" when neither source is available.
func kimiToken() string {
	if t := kimiCLIToken(); t != "" {
		return t
	}
	return strings.TrimSpace(os.Getenv("KIMI_API_KEY"))
}
