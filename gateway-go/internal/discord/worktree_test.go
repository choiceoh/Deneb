package discord

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

func TestWorktreeManager_Create_NonGitParent(t *testing.T) {
	// Create a temp directory structure: parentDir is a plain directory
	// (not a git repo), which previously caused worktree creation to fail.
	tmpDir := t.TempDir()
	parentDir := filepath.Join(tmpDir, "workspace")
	baseDir := filepath.Join(tmpDir, "worktrees")

	if err := os.MkdirAll(parentDir, 0o755); err != nil {
		t.Fatal(err)
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	wm := NewWorktreeManager(baseDir, logger)

	// Create should succeed even though parentDir is not a git repo —
	// ensureGitRepo should auto-initialize it.
	ws, err := wm.Create("test-thread-1", parentDir)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	if ws.Dir == "" {
		t.Fatal("expected non-empty worktree dir")
	}
	if ws.ThreadID != "test-thread-1" {
		t.Errorf("expected threadID test-thread-1, got %s", ws.ThreadID)
	}

	// Verify the worktree directory was actually created.
	if _, err := os.Stat(ws.Dir); err != nil {
		t.Errorf("worktree dir does not exist: %v", err)
	}

	// Get should return the same workspace.
	got := wm.Get("test-thread-1")
	if got == nil || got.Dir != ws.Dir {
		t.Error("Get returned nil or different workspace")
	}

	// Cleanup.
	wm.Remove("test-thread-1")
	if wm.Get("test-thread-1") != nil {
		t.Error("workspace still exists after Remove")
	}
}

func TestWorktreeManager_Create_ExistingGitRepo(t *testing.T) {
	// When parentDir is already a git repo, Create should work without
	// re-initializing.
	tmpDir := t.TempDir()
	parentDir := filepath.Join(tmpDir, "repo")
	baseDir := filepath.Join(tmpDir, "worktrees")

	if err := os.MkdirAll(parentDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Pre-initialize as a git repo.
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	wm := NewWorktreeManager(baseDir, logger)
	wm.runGit(parentDir, "init")
	wm.runGit(parentDir, "commit", "--allow-empty", "-m", "seed")

	ws, err := wm.Create("test-thread-2", parentDir)
	if err != nil {
		t.Fatalf("Create failed for existing repo: %v", err)
	}
	if ws.Dir == "" {
		t.Fatal("expected non-empty worktree dir")
	}

	wm.Remove("test-thread-2")
}

func TestWorktreeManager_EnsureGitRepo_Idempotent(t *testing.T) {
	tmpDir := t.TempDir()
	parentDir := filepath.Join(tmpDir, "workspace")

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	wm := NewWorktreeManager(filepath.Join(tmpDir, "worktrees"), logger)

	// First call: should init.
	if err := wm.ensureGitRepo(parentDir); err != nil {
		t.Fatalf("first ensureGitRepo failed: %v", err)
	}
	// Second call: should be a no-op.
	if err := wm.ensureGitRepo(parentDir); err != nil {
		t.Fatalf("second ensureGitRepo failed: %v", err)
	}
}
