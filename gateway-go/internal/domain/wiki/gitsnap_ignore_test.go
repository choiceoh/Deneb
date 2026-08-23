package wiki

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestEnsureGitRepo_ReconcilesIgnoreListAndSetsGCPolicy: .gitignore used to be
// written once at repo creation, so every rule added in a later release did
// nothing on an existing wiki — which is how the dreamer ledgers stayed tracked
// and grew the loose-object pile (39MB loose vs 6.8MB packed, measured
// 2026-08-23). Operator-added lines must survive the reconciliation.
func TestEnsureGitRepo_ReconcilesIgnoreListAndSetsGCPolicy(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	s, err := NewStore(dir, dir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	// An old repo: created with a short ignore list plus an operator's own line.
	ignorePath := filepath.Join(dir, ".gitignore")
	if err := os.WriteFile(ignorePath, []byte(".semantic-cache.json\n내-메모.txt\n"), 0o644); err != nil {
		t.Fatalf("seed .gitignore: %v", err)
	}

	if err := s.ensureGitRepo(context.Background()); err != nil {
		t.Fatalf("ensureGitRepo: %v", err)
	}

	body, err := os.ReadFile(ignorePath)
	if err != nil {
		t.Fatalf("read .gitignore: %v", err)
	}
	got := string(body)
	for _, want := range []string{
		".semantic-cache.json", "내-메모.txt", // pre-existing lines survive
		".recall-hits.jsonl", ".verify-findings.json", ".dream-fact-ledger.jsonl", ".wiki.db",
	} {
		if !strings.Contains(got, want) {
			t.Errorf(".gitignore missing %q:\n%s", want, got)
		}
	}
	// No duplicates: a second pass is a no-op.
	before := got
	if err := s.ensureGitRepo(context.Background()); err != nil {
		t.Fatalf("second ensureGitRepo: %v", err)
	}
	after, _ := os.ReadFile(ignorePath)
	if string(after) != before {
		t.Errorf("reconciliation is not idempotent:\n%s\n---\n%s", before, string(after))
	}

	out, gerr := s.git(context.Background(), "config", "--get", "gc.auto")
	if gerr != nil || strings.TrimSpace(out) != "512" {
		t.Errorf("gc.auto = %q (err %v), want 512", strings.TrimSpace(out), gerr)
	}
}
