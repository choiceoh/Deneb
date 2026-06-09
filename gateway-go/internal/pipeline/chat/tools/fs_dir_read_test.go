package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestToolRead_DirectoryReturnsListing pins the fix for the bulk of read's
// recorded failures: a read on a directory returns a listing rather than a hard
// error. (Observed in prod via observe.behavior — a wiki-audit pass that read
// wiki pages whose .md paths were momentarily directories drove read's error
// rate to ~100% for two days.)
func TestToolRead_DirectoryReturnsListing(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "alpha.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "subdir"), 0o755); err != nil {
		t.Fatal(err)
	}
	out, err := callTool(t, ToolRead(dir), map[string]any{"file_path": "."})
	if err != nil {
		t.Fatalf("reading a directory should not error, got: %v", err)
	}
	for _, want := range []string{"is a directory", "alpha.txt", "subdir/"} {
		if !strings.Contains(out, want) {
			t.Errorf("listing missing %q:\n%s", want, out)
		}
	}
}

// A genuinely absent file must still error — that's real information the model
// needs, not a benign directory mistake.
func TestToolRead_MissingFileStillErrors(t *testing.T) {
	dir := t.TempDir()
	if _, err := callTool(t, ToolRead(dir), map[string]any{"file_path": "nope.txt"}); err == nil {
		t.Error("a missing file should still surface an error")
	}
}
