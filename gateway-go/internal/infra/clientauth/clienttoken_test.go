package clientauth

import (
	"os"
	"path/filepath"
	"testing"
)

// isolateStateDir points config.ResolveStateDir at a temp dir so tests never
// touch the real ~/.deneb. config checks DENEB_STATE_DIR first.
func isolateStateDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("DENEB_STATE_DIR", dir)
	return dir
}

func TestGenerateAndVerify(t *testing.T) {
	dir := isolateStateDir(t)

	// Disabled before any token exists.
	if Verify("anything") {
		t.Fatal("Verify should be false when no token file exists")
	}
	if Load() != "" {
		t.Fatal("Load should be empty when no token file exists")
	}

	token, err := Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(token) != 64 { // 32 random bytes hex-encoded
		t.Errorf("expected 64-hex token, got len %d", len(token))
	}

	// File written with 0600 perms.
	info, err := os.Stat(filepath.Join(dir, tokenFilename))
	if err != nil {
		t.Fatalf("stat token file: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("expected 0600 perms, got %o", perm)
	}

	if Load() != token {
		t.Errorf("Load mismatch: got %q want %q", Load(), token)
	}
	if !Verify(token) {
		t.Error("Verify should accept the generated token")
	}
	if Verify(token + "x") {
		t.Error("Verify should reject a wrong token")
	}
	if Verify("") {
		t.Error("Verify should reject an empty presented token")
	}
}

func TestGenerateRotates(t *testing.T) {
	isolateStateDir(t)

	first, err := Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	second, err := Generate()
	if err != nil {
		t.Fatalf("Generate (rotate): %v", err)
	}
	if first == second {
		t.Error("rotation should produce a different token")
	}
	if Verify(first) {
		t.Error("old token must not verify after rotation")
	}
	if !Verify(second) {
		t.Error("new token must verify after rotation")
	}
}
