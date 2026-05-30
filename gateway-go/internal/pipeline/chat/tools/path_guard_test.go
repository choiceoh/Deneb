package tools

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckProtectedPath_Denied(t *testing.T) {
	home := "/home/user"
	denied := []string{
		home + "/.deneb/credentials/telegram.json",
		home + "/.deneb/deneb.json",
		home + "/.deneb/auth.json",
		home + "/.deneb/sessions/abc.jsonl",
		home + "/.ssh/id_ed25519",
		home + "/.ssh/config",
		home + "/.aws/credentials",
		home + "/.netrc",
		home + "/project/.env",
		home + "/project/.env.production",
		home + "/work/id_rsa",
	}
	for _, p := range denied {
		if err := CheckProtectedPath(p, "read"); err == nil {
			t.Errorf("expected %q to be denied, but it was allowed", p)
		}
	}
}

func TestCheckProtectedPath_Allowed(t *testing.T) {
	home := "/home/user"
	allowed := []string{
		home + "/project/main.go",
		home + "/project/README.md",
		home + "/project/config.json",    // ordinary json, not under .deneb
		home + "/project/environment.md", // not a .env file
		home + "/.deneb/notes.md",        // non-secret file under .deneb (not json/env/token)
		home + "/project/src/env.go",
	}
	for _, p := range allowed {
		if err := CheckProtectedPath(p, "read"); err != nil {
			t.Errorf("expected %q to be allowed, got: %v", p, err)
		}
	}
}

func TestCheckProtectedPath_RelativeResolves(t *testing.T) {
	// A relative .env path must be denied the same as an absolute one.
	if err := CheckProtectedPath(".env", "write"); err == nil {
		t.Error("relative .env should be denied")
	}
	if err := CheckProtectedPath(filepath.Join("sub", "dir", ".env"), "edit"); err == nil {
		t.Error("nested relative .env should be denied")
	}
}

func TestCheckProtectedPath_OpInMessage(t *testing.T) {
	err := CheckProtectedPath("/home/user/.ssh/id_rsa", "write")
	if err == nil || !strings.Contains(err.Error(), "write") {
		t.Errorf("error message should mention the op, got: %v", err)
	}
}
