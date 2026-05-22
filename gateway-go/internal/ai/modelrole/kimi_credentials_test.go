package modelrole

import (
	"os"
	"path/filepath"
	"testing"
)

func TestKimiCLIToken(t *testing.T) {
	dir := t.TempDir()

	// Missing file → empty, no error.
	t.Setenv("KIMI_CREDENTIALS_FILE", filepath.Join(dir, "absent.json"))
	if got := kimiCLIToken(); got != "" {
		t.Errorf("kimiCLIToken() with missing file = %q, want empty", got)
	}

	// Valid credentials file.
	credPath := filepath.Join(dir, "kimi-code.json")
	if err := os.WriteFile(credPath, []byte(`{"access_token":"oauth-jwt-123","token_type":"Bearer"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KIMI_CREDENTIALS_FILE", credPath)
	if got := kimiCLIToken(); got != "oauth-jwt-123" {
		t.Errorf("kimiCLIToken() = %q, want oauth-jwt-123", got)
	}

	// Malformed JSON → empty, no panic.
	if err := os.WriteFile(credPath, []byte(`not json`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := kimiCLIToken(); got != "" {
		t.Errorf("kimiCLIToken() with bad JSON = %q, want empty", got)
	}
}

func TestKimiToken(t *testing.T) {
	dir := t.TempDir()

	// No file and no env var → empty.
	t.Setenv("KIMI_CREDENTIALS_FILE", filepath.Join(dir, "absent.json"))
	t.Setenv("KIMI_API_KEY", "")
	if got := kimiToken(); got != "" {
		t.Errorf("kimiToken() with no source = %q, want empty", got)
	}

	// Env-var fallback when there is no credentials file.
	t.Setenv("KIMI_API_KEY", "sk-env-key")
	if got := kimiToken(); got != "sk-env-key" {
		t.Errorf("kimiToken() env fallback = %q, want sk-env-key", got)
	}

	// The CLI credentials file takes precedence over the env var.
	credPath := filepath.Join(dir, "kimi-code.json")
	if err := os.WriteFile(credPath, []byte(`{"access_token":"oauth-jwt-123"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KIMI_CREDENTIALS_FILE", credPath)
	if got := kimiToken(); got != "oauth-jwt-123" {
		t.Errorf("kimiToken() = %q, want oauth-jwt-123 (file should win over env)", got)
	}
}
