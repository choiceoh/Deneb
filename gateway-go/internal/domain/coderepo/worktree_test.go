package coderepo

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// realRepo builds an actual git repository with one commit. Worktrees are a git
// feature — a mocked git would prove nothing about the guards here.
func realRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(
			os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	run("init", "--initial-branch=main")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("hi\n"), 0o600); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	run("add", "README.md")
	run("commit", "-m", "seed")
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	return resolved
}

func TestSlugAndBranchAreFilesystemSafe(t *testing.T) {
	if got := SessionSlug("client:cygnus:a1b2"); got != "client-cygnus-a1b2" {
		t.Errorf("slug = %q", got)
	}
	if got := BranchFor("client:cygnus:a1b2"); got != "deneb/client-cygnus-a1b2" {
		t.Errorf("branch = %q", got)
	}
	// A key of only separators must still produce a usable name rather than an
	// empty path component.
	if got := BranchFor(":::"); got != "deneb/session" {
		t.Errorf("degenerate branch = %q", got)
	}
}

func TestEnsureWorktreeCreatesOnItsOwnBranch(t *testing.T) {
	repo := realRepo(t)
	wt := WorktreePath(t.TempDir(), "api-1", "client:cygnus:a")
	branch := BranchFor("client:cygnus:a")

	if err := EnsureWorktree(context.Background(), repo, wt, branch); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if _, err := os.Stat(filepath.Join(wt, "README.md")); err != nil {
		t.Fatalf("worktree has no content: %v", err)
	}
	out, err := exec.Command("git", "-C", wt, "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil {
		t.Fatalf("read branch: %v", err)
	}
	if got := strings.TrimSpace(string(out)); got != branch {
		t.Errorf("worktree branch = %q, want %q", got, branch)
	}
}

// Binding the same conversation twice must not fail — it is the operator asking
// for a state that already holds.
func TestEnsureWorktreeIsIdempotent(t *testing.T) {
	repo := realRepo(t)
	wt := WorktreePath(t.TempDir(), "api-1", "client:cygnus:a")
	branch := BranchFor("client:cygnus:a")

	for i := range 2 {
		if err := EnsureWorktree(context.Background(), repo, wt, branch); err != nil {
			t.Fatalf("ensure #%d: %v", i+1, err)
		}
	}
}

// A worktree whose directory was deleted out from under git leaves a stale
// registration; without a prune, the next add fails. This is the guard the
// harness scripts learned to need.
func TestEnsureWorktreeRecoversFromStaleRegistration(t *testing.T) {
	repo := realRepo(t)
	wt := WorktreePath(t.TempDir(), "api-1", "client:cygnus:a")
	branch := BranchFor("client:cygnus:a")

	if err := EnsureWorktree(context.Background(), repo, wt, branch); err != nil {
		t.Fatalf("first: %v", err)
	}
	if err := os.RemoveAll(wt); err != nil {
		t.Fatalf("simulate deletion: %v", err)
	}
	if err := EnsureWorktree(context.Background(), repo, wt, branch); err != nil {
		t.Fatalf("recreate after stale registration: %v", err)
	}
	if _, err := os.Stat(filepath.Join(wt, "README.md")); err != nil {
		t.Errorf("worktree not restored: %v", err)
	}
}

func TestRemoveWorktreeDeletesACleanTree(t *testing.T) {
	repo := realRepo(t)
	wt := WorktreePath(t.TempDir(), "api-1", "client:cygnus:a")
	branch := BranchFor("client:cygnus:a")
	if err := EnsureWorktree(context.Background(), repo, wt, branch); err != nil {
		t.Fatalf("ensure: %v", err)
	}

	if err := RemoveWorktree(context.Background(), repo, wt); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, err := os.Stat(wt); !os.IsNotExist(err) {
		t.Error("clean worktree should have been removed")
	}
}

// The load-bearing safety property: automatic cleanup must never eat work.
func TestRemoveWorktreeRefusesWhileWorkIsUncommitted(t *testing.T) {
	repo := realRepo(t)
	wt := WorktreePath(t.TempDir(), "api-1", "client:cygnus:a")
	branch := BranchFor("client:cygnus:a")
	if err := EnsureWorktree(context.Background(), repo, wt, branch); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if err := os.WriteFile(filepath.Join(wt, "draft.txt"), []byte("in progress\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	if err := RemoveWorktree(context.Background(), repo, wt); err == nil {
		t.Fatal("removing a dirty worktree must be refused")
	}
	if _, err := os.Stat(filepath.Join(wt, "draft.txt")); err != nil {
		t.Errorf("the uncommitted file was destroyed: %v", err)
	}
}

// After a removal the branch survives, so a returning conversation re-attaches
// to its own history instead of silently starting over.
func TestEnsureWorktreeReattachesToTheExistingBranch(t *testing.T) {
	repo := realRepo(t)
	state := t.TempDir()
	wt := WorktreePath(state, "api-1", "client:cygnus:a")
	branch := BranchFor("client:cygnus:a")

	if err := EnsureWorktree(context.Background(), repo, wt, branch); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	cmd := exec.Command("git", "-C", wt, "commit", "--allow-empty", "-m", "work")
	cmd.Env = append(
		os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("commit: %v\n%s", err, out)
	}
	before, err := exec.Command("git", "-C", wt, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatalf("head: %v", err)
	}
	if err := RemoveWorktree(context.Background(), repo, wt); err != nil {
		t.Fatalf("remove: %v", err)
	}

	if err := EnsureWorktree(context.Background(), repo, wt, branch); err != nil {
		t.Fatalf("re-ensure: %v", err)
	}
	after, err := exec.Command("git", "-C", wt, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatalf("head after: %v", err)
	}
	if string(before) != string(after) {
		t.Errorf("re-attached to a different commit: %s vs %s", before, after)
	}
}

func TestHasUncommittedChangesTreatsUnreadableTreeAsDirty(t *testing.T) {
	// Cannot tell → must not delete.
	if !HasUncommittedChanges(context.Background(), filepath.Join(t.TempDir(), "absent")) {
		t.Error("an unreadable worktree must count as dirty")
	}
}
