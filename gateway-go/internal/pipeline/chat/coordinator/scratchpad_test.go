package coordinator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

func TestScratchpadDir_Creates(t *testing.T) {
	dir, err := ScratchpadDir("test-session-123")
	testutil.NoError(t, err)
	defer os.RemoveAll(dir)

	// Check directory exists.
	info, err := os.Stat(dir)
	testutil.NoError(t, err)
	if !info.IsDir() {
		t.Fatal("scratchpad path is not a directory")
	}

	// Check implementation subdirectory.
	implDir := filepath.Join(dir, "implementation")
	info, err = os.Stat(implDir)
	testutil.NoError(t, err)
	if !info.IsDir() {
		t.Fatal("implementation path is not a directory")
	}
}

func TestScratchpadDir_Idempotent(t *testing.T) {
	dir1, err := ScratchpadDir("idempotent-test")
	testutil.NoError(t, err)
	defer os.RemoveAll(dir1)

	dir2, err := ScratchpadDir("idempotent-test")
	testutil.NoError(t, err)
	if dir1 != dir2 {
		t.Errorf("expected same path, got %q and %q", dir1, dir2)
	}
}

func TestScratchpadDir_EmptyID(t *testing.T) {
	_, err := ScratchpadDir("")
	if err == nil {
		t.Error("expected error for empty session ID")
	}
}

func TestCleanupScratchpad(t *testing.T) {
	dir, err := ScratchpadDir("cleanup-test")
	testutil.NoError(t, err)

	if err := CleanupScratchpad("cleanup-test"); err != nil {
		t.Fatalf("CleanupScratchpad: %v", err)
	}

	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Error("scratchpad directory should have been removed")
	}
}

func TestSanitizeSessionID(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"simple", "simple"},
		{"acp:main:sub_1", "acp_main_sub_1"},
		{"../../../etc/passwd", "_________etc_passwd"},
		{"", ""},
		{"  spaces  ", "spaces"},
	}
	for _, tt := range tests {
		got := sanitizeSessionID(tt.input)
		if got != tt.want {
			t.Errorf("sanitizeSessionID(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestResolveScratchpadDir(t *testing.T) {
	dir := ResolveScratchpadDir("resolve-test")
	if dir == "" {
		t.Fatal("ResolveScratchpadDir should return non-empty path")
	}
	defer os.RemoveAll(dir)

	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("directory should exist: %v", err)
	}
}

func TestResolveScratchpadDir_Empty(t *testing.T) {
	dir := ResolveScratchpadDir("")
	if dir != "" {
		t.Errorf("expected empty string for invalid session ID, got %q", dir)
	}
}
