package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The skills index advertises SKILL.md locations outside the workspace
// (~/.deneb/skills). Before ResolvePathWithRoots, those reads were silently
// clamped to the workspace root and returned a directory listing instead of
// the skill body (observed in production transcripts: "is a directory with 64
// entries" for a SKILL.md path). These tests pin the fixed contract.

func TestResolvePathWithRoots_AllowsExtraRoot(t *testing.T) {
	ws := t.TempDir()
	catalog := t.TempDir()
	target := filepath.Join(catalog, "email-analysis", "SKILL.md")

	got := ResolvePathWithRoots(target, ws, []string{catalog})
	if got != target {
		t.Errorf("path under extra root should resolve as-is, got %q want %q", got, target)
	}
}

func TestResolvePathWithRoots_ClampsOutsideAllRoots(t *testing.T) {
	ws := t.TempDir()
	catalog := t.TempDir()

	got := ResolvePathWithRoots("/etc/passwd", ws, []string{catalog})
	if got != ws {
		t.Errorf("path outside all roots must clamp to workspace, got %q", got)
	}

	// Traversal out of an allowed extra root clamps too.
	esc := filepath.Join(catalog, "..", "outside.txt")
	got = ResolvePathWithRoots(esc, ws, []string{catalog})
	if got != ws {
		t.Errorf("traversal escaping the extra root must clamp, got %q", got)
	}
}

func TestResolvePathWithRoots_EmptyRootsKeepResolvePathBehavior(t *testing.T) {
	ws := t.TempDir()
	inside := filepath.Join(ws, "notes.md")
	if got := ResolvePathWithRoots(inside, ws, nil); got != inside {
		t.Errorf("workspace path should resolve as-is, got %q", got)
	}
	if got := ResolvePathWithRoots("notes.md", ws, []string{"", "  "}); got != inside {
		t.Errorf("relative path should join workspace even with blank extra roots, got %q", got)
	}
}

func TestResolvePathWithRoots_ExpandsTilde(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skip("no home dir")
	}
	ws := t.TempDir()

	// "~/..." expands against home; with home allowed as an extra root the
	// expanded path resolves as-is.
	got := ResolvePathWithRoots("~/somefile.txt", ws, []string{home})
	want := filepath.Join(home, "somefile.txt")
	if got != want {
		t.Errorf("tilde path = %q, want %q", got, want)
	}

	// Expansion does not widen the boundary: without an allowing root the
	// expanded path still clamps to the workspace.
	if got := ResolvePathWithRoots("~/somefile.txt", ws, nil); got != ws {
		t.Errorf("tilde path outside roots must clamp to workspace, got %q", got)
	}
}

func TestToolRead_ReadsSkillFromCatalogRoot(t *testing.T) {
	ws := t.TempDir()
	catalog := t.TempDir()
	skillDir := filepath.Join(catalog, "email-analysis")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "# Email Analysis Skill\nprocedure body"
	skillPath := filepath.Join(skillDir, "SKILL.md")
	if err := os.WriteFile(skillPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	read := ToolRead(ws, catalog)
	input, _ := json.Marshal(map[string]any{"file_path": skillPath})
	out, err := read(context.Background(), json.RawMessage(input))
	if err != nil {
		t.Fatalf("read on catalog skill failed: %v", err)
	}
	if !strings.Contains(out, "Email Analysis Skill") {
		t.Errorf("expected skill body, got: %s", out)
	}

	// Without the catalog root the same read degrades to the clamped
	// workspace-dir behavior (the pre-fix production symptom).
	readNoRoot := ToolRead(ws)
	out, err = readNoRoot(context.Background(), json.RawMessage(input))
	if err == nil && strings.Contains(out, "Email Analysis Skill") {
		t.Errorf("read without catalog root should not reach the skill body")
	}
}
