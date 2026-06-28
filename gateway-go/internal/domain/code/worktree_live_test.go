package code

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestCodeLifecycle_Live drives the REAL Manager against real git (and `go` for
// verify) through the whole worktree lifecycle, so the unit suite's command-shape
// checks are backed by an actual end-to-end run. Gated behind DENEB_CODE_LIVE so
// normal `go test`/CI (which may lack a usable git) stays hermetic.
//
//	DENEB_CODE_LIVE=1 go test -run TestCodeLifecycle_Live ./internal/domain/code/
func TestCodeLifecycle_Live(t *testing.T) {
	if os.Getenv("DENEB_CODE_LIVE") == "" {
		t.Skip("set DENEB_CODE_LIVE=1 to run the real-git functional test")
	}
	requireBin(t, "git")
	requireBin(t, "go")

	root := t.TempDir()
	origin := filepath.Join(root, "origin")
	setupOriginRepo(t, origin)

	repo := Repo{Owner: "acme", Name: "app"}
	m := NewManager(root)

	// Pre-create the base clone from the LOCAL origin (EnsureBase's clone targets
	// github.com; everything after — fetch, worktree, commit, verify, push — is what
	// we exercise against real git). Give the base an identity so worktree commits work.
	base := m.basePath(repo)
	mustGit(t, "", "clone", "-q", origin, base)
	mustGit(t, base, "config", "user.email", "test@deneb.local")
	mustGit(t, base, "config", "user.name", "deneb test")
	mustGit(t, base, "config", "commit.gpgsign", "false")

	ctx := context.Background()

	// StartTask: EnsureBase (fetch) + worktree add a fresh branch off origin/main.
	task, err := m.StartTask(ctx, repo, "fix-readme")
	if err != nil {
		t.Fatalf("StartTask: %v", err)
	}
	if _, err := os.Stat(task.Dir); err != nil {
		t.Fatalf("worktree dir missing: %v", err)
	}
	if got := gitOut(t, task.Dir, "rev-parse", "--abbrev-ref", "HEAD"); got != "deneb/fix-readme" {
		t.Fatalf("branch = %q, want deneb/fix-readme", got)
	}

	// Edit + checkpoint.
	writeFile(t, filepath.Join(task.Dir, "README.md"), "# app\n\nhello from deneb\n")
	if err := m.Commit(ctx, task, "update readme"); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if sha, err := m.HeadSHA(ctx, task); err != nil || len(sha) < 7 {
		t.Fatalf("HeadSHA = %q err=%v", sha, err)
	}

	// Verify: detect the Go module → go build + go test, both pass on a trivial module.
	res, err := m.Verify(ctx, task.Dir)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if res.Kind != KindGo || !res.Passed {
		t.Fatalf("verify kind=%q passed=%v steps=%+v", res.Kind, res.Passed, res.Steps)
	}

	// Push the checkpoint branch to the local origin.
	if err := m.Push(ctx, task); err != nil {
		t.Fatalf("Push: %v", err)
	}
	if !strings.Contains(gitOut(t, origin, "branch", "--list", "deneb/fix-readme"), "deneb/fix-readme") {
		t.Error("pushed branch should exist on origin")
	}

	// Undo (dirty): an uncommitted edit is discarded, the checkpoint commit stays.
	writeFile(t, filepath.Join(task.Dir, "scratch.txt"), "wip")
	if popped, err := m.Undo(ctx, task); err != nil {
		t.Fatalf("Undo dirty: %v", err)
	} else if popped {
		t.Error("dirty undo must not report a popped checkpoint")
	}
	if _, err := os.Stat(filepath.Join(task.Dir, "scratch.txt")); !os.IsNotExist(err) {
		t.Error("dirty undo should have discarded scratch.txt")
	}
	if gitOut(t, task.Dir, "log", "--oneline", "-1") == "" {
		t.Error("checkpoint commit must survive a dirty undo")
	}

	// Undo (clean): drops the checkpoint commit, back to origin/main.
	if popped, err := m.Undo(ctx, task); err != nil {
		t.Fatalf("Undo clean: %v", err)
	} else if !popped {
		t.Error("clean undo should report a popped checkpoint")
	}
	if got := gitOut(t, task.Dir, "rev-list", "--count", "origin/main..HEAD"); got != "0" {
		t.Errorf("after clean undo, commits ahead of base = %q, want 0", got)
	}

	// A second clean undo has nothing left → refused (never reset past the fork).
	if _, err := m.Undo(ctx, task); err == nil {
		t.Error("undo with no checkpoints should be refused")
	}

	// Discard removes the worktree + branch.
	if err := m.Discard(ctx, task); err != nil {
		t.Fatalf("Discard: %v", err)
	}
	if _, err := os.Stat(task.Dir); !os.IsNotExist(err) {
		t.Error("worktree dir should be gone after Discard")
	}
}

// --- helpers ---

func requireBin(t *testing.T, name string) {
	t.Helper()
	if _, err := exec.LookPath(name); err != nil {
		t.Skipf("%s not on PATH", name)
	}
}

func setupOriginRepo(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	mustGit(t, dir, "init", "-q")
	mustGit(t, dir, "config", "user.email", "test@deneb.local")
	mustGit(t, dir, "config", "user.name", "deneb test")
	mustGit(t, dir, "config", "commit.gpgsign", "false")
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/app\n\ngo 1.21\n")
	writeFile(t, filepath.Join(dir, "main.go"), "package main\n\nfunc main() {}\n")
	mustGit(t, dir, "add", "-A")
	mustGit(t, dir, "commit", "-q", "-m", "init")
	mustGit(t, dir, "branch", "-M", "main")
}

func mustGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
