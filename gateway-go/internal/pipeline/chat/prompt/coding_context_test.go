package prompt

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadCodingRepoContext(t *testing.T) {
	t.Cleanup(Cache.Reset)
	dir := t.TempDir()
	mustWrite := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite("CLAUDE.md", "# Rules\n커밋은 Conventional Commit.")
	mustWrite("AGENTS.md", "# Agents\nRun make check before push.")

	rc := LoadCodingRepoContext(dir, "code:task-1")
	for _, want := range []string{"### CLAUDE.md", "커밋은 Conventional Commit.", "### AGENTS.md", "make check"} {
		if !strings.Contains(rc.Content, want) {
			t.Errorf("repo context missing %q", want)
		}
	}
	if rc.Hash == "" || len(rc.Hash) != 12 {
		t.Errorf("expected 12-char content hash, got %q", rc.Hash)
	}

	// Frozen per session: an edit after the first load must not change the
	// session's snapshot (Static cache key stability for vLLM APC).
	mustWrite("CLAUDE.md", "# Rules v2 — totally different")
	again := LoadCodingRepoContext(dir, "code:task-1")
	if again.Content != rc.Content || again.Hash != rc.Hash {
		t.Error("session snapshot must be frozen across mid-session edits")
	}
	// A different session sees the new bytes.
	fresh := LoadCodingRepoContext(dir, "code:task-2")
	if !strings.Contains(fresh.Content, "totally different") {
		t.Error("new session should load the edited docs")
	}
	// ClearSession (the /reset path) drops the frozen snapshot.
	Cache.ClearSession("code:task-1")
	cleared := LoadCodingRepoContext(dir, "code:task-1")
	if cleared.Content == rc.Content {
		t.Error("ClearSession should force a re-read from disk")
	}
}

func TestLoadCodingRepoContextEmptyAndMissing(t *testing.T) {
	t.Cleanup(Cache.Reset)
	if rc := LoadCodingRepoContext("", "code:x"); rc.Content != "" || rc.Hash != "" {
		t.Error("empty worktree dir must yield no injection")
	}
	dir := t.TempDir() // no docs at all
	rc := LoadCodingRepoContext(dir, "code:y")
	if rc.Content != "" || rc.Hash != "" {
		t.Error("doc-less repo must yield no injection")
	}
	// The empty result is frozen too (no per-turn re-stat).
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("late doc"), 0o644); err != nil {
		t.Fatal(err)
	}
	if again := LoadCodingRepoContext(dir, "code:y"); again.Content != "" {
		t.Error("empty snapshot must stay frozen for the session")
	}
}

func TestLoadCodingRepoContextTruncation(t *testing.T) {
	t.Cleanup(Cache.Reset)
	dir := t.TempDir()
	big := strings.Repeat("헤드규칙 A. ", 2_000) // ~26KB > 8K per-file budget
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte(big), 0o644); err != nil {
		t.Fatal(err)
	}
	rc := loadCodingRepoContextFromDisk(dir)
	if len(rc.Content) > maxCodingRepoFileChars+1_000 { // header + marker slack
		t.Errorf("oversized doc not truncated: %d bytes", len(rc.Content))
	}
	if rc.Content == "" {
		t.Error("truncated doc should still inject head/tail content")
	}
}
