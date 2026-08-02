package filesystem

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A request outside the workspace gets clamped to the workspace ROOT before the
// old error text was composed, so edit reported "SKILL.md is a directory" about
// a path that was a regular file all along — the heartbeat burned 15 straight
// edit calls on that invented fact (2026-08-02 regression alert). These pin the
// honesty rule: no verdict about the clamp target may be phrased against the
// requested path.

func TestEditOutsideWorkspaceSaysOutsideNotDirectory(t *testing.T) {
	workspace := t.TempDir()
	elsewhere := t.TempDir()
	target := filepath.Join(elsewhere, "SKILL.md")
	if err := os.WriteFile(target, []byte("# skill\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := callTool(t, ToolEdit(workspace), map[string]any{
		"file_path": target, "old_string": "body", "new_string": "new body",
	})
	if err == nil {
		t.Fatal("an out-of-workspace edit must be refused")
	}
	if strings.Contains(err.Error(), "is a directory") {
		t.Errorf("must not claim a regular file is a directory: %v", err)
	}
	if !strings.Contains(err.Error(), "outside") || !strings.Contains(err.Error(), target) {
		t.Errorf("must name the requested path and say it is outside the workspace: %v", err)
	}
}

func TestWriteOutsideWorkspaceSaysOutsideNotDirectory(t *testing.T) {
	workspace := t.TempDir()
	elsewhere := t.TempDir()
	target := filepath.Join(elsewhere, "note.md")

	_, err := callTool(t, ToolWrite(workspace), map[string]any{
		"file_path": target, "content": "x",
	})
	if err == nil {
		t.Fatal("an out-of-workspace write must be refused")
	}
	if strings.Contains(err.Error(), "is a directory") {
		t.Errorf("must not describe the clamp target: %v", err)
	}
	if !strings.Contains(err.Error(), "outside") {
		t.Errorf("must say the path is outside the workspace: %v", err)
	}
}

func TestEditGenuineDirectoryInsideWorkspaceKeepsDirectoryMessage(t *testing.T) {
	// The directory message is correct when the target really is a directory
	// under the workspace — that diagnosis must survive the honesty fix.
	workspace := t.TempDir()
	sub := filepath.Join(workspace, "docs")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := callTool(t, ToolEdit(workspace), map[string]any{
		"file_path": sub, "old_string": "a", "new_string": "b",
	})
	if err == nil {
		t.Fatal("editing a directory must be refused")
	}
	if !strings.Contains(err.Error(), "is a directory") {
		t.Errorf("a real directory must still be called one: %v", err)
	}
}

func TestEditInsideWorkspaceStillWorks(t *testing.T) {
	workspace := t.TempDir()
	target := filepath.Join(workspace, "file.md")
	if err := os.WriteFile(target, []byte("old text"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := mustCallTool(t, ToolEdit(workspace), map[string]any{
		"file_path": "file.md", "old_string": "old", "new_string": "new",
	})
	if out == "" {
		t.Fatal("in-workspace edit must succeed")
	}
	got, _ := os.ReadFile(target)
	if string(got) != "new text" {
		t.Errorf("edit did not apply: %q", got)
	}
}
