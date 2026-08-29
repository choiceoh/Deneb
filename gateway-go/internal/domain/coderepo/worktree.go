package coderepo

// Per-conversation git worktrees.
//
// A conversation bound to a repository gets its own worktree on a fresh branch,
// so two conversations working the same repo never fight over one working tree.
// The guards here mirror the harness scripts that already do this for ZCode and
// Cursor (scripts/dev/zcode-worktree-init.sh) — they were learned the hard way
// and are cheaper to copy than to rediscover.

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// gitTimeout bounds every git invocation. These run on the request path (a bind
// call), so a wedged git must fail rather than hold the caller open.
const gitTimeout = 30 * time.Second

// SessionSlug turns a session key into something usable as a directory and
// branch component ("client:cygnus:a1b2" → "client-cygnus-a1b2").
func SessionSlug(sessionKey string) string {
	var b strings.Builder
	for _, r := range sessionKey {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	return strings.Trim(b.String(), "-")
}

// BranchFor is the branch a conversation's worktree lives on. Always a new
// branch per conversation (operator decision): work never lands on whatever the
// operator happened to have checked out.
func BranchFor(sessionKey string) string {
	slug := SessionSlug(sessionKey)
	if slug == "" {
		slug = "session"
	}
	return "deneb/" + slug
}

// WorktreePath is where a conversation's worktree lives. Kept under the state
// dir, beside the other Deneb-owned state, rather than inside the repository —
// a stray directory in the operator's checkout would show up in their status.
func WorktreePath(stateDir, repoID, sessionKey string) string {
	slug := SessionSlug(sessionKey)
	if slug == "" {
		slug = "session"
	}
	return filepath.Join(stateDir, "worktrees", repoID, slug)
}

func git(ctx context.Context, repoPath string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, gitTimeout)
	defer cancel()
	full := append([]string{"-C", repoPath}, args...)
	out, err := exec.CommandContext(ctx, "git", full...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// EnsureWorktree creates the conversation's worktree, or returns the existing
// one. Idempotent: binding the same conversation twice is not an error.
func EnsureWorktree(ctx context.Context, repoPath, wtPath, branch string) error {
	// Prune first. A half-deleted worktree (directory gone, registration
	// still recorded) makes `worktree add` fail with a confusing message.
	_, _ = git(ctx, repoPath, "worktree", "prune")

	if listed, err := git(ctx, repoPath, "worktree", "list", "--porcelain"); err == nil {
		for _, line := range strings.Split(listed, "\n") {
			if strings.TrimSpace(line) == "worktree "+wtPath {
				return nil // already there — reuse
			}
		}
	}
	if err := os.MkdirAll(filepath.Dir(wtPath), 0o755); err != nil {
		return fmt.Errorf("워크트리 상위 디렉터리를 만들지 못했습니다: %w", err)
	}

	// An existing branch means this conversation had a worktree that was
	// cleaned up. Re-attach to keep its history instead of failing or, worse,
	// silently starting a second branch for the same conversation.
	if _, err := git(ctx, repoPath, "show-ref", "--verify", "--quiet", "refs/heads/"+branch); err == nil {
		if out, err := git(ctx, repoPath, "worktree", "add", wtPath, branch); err != nil {
			return fmt.Errorf("워크트리를 다시 연결하지 못했습니다: %s", out)
		}
		return nil
	}

	// Fresh branch from upstream HEAD, so a conversation starts from what is
	// pushed rather than whatever the operator left checked out. Quiet fetch;
	// offline falls back to the local branch.
	_, _ = git(ctx, repoPath, "fetch", "--quiet", "origin", defaultBranch(ctx, repoPath))
	seed := seedRef(ctx, repoPath)
	if out, err := git(ctx, repoPath, "worktree", "add", "-b", branch, wtPath, seed); err != nil {
		return fmt.Errorf("워크트리를 만들지 못했습니다: %s", out)
	}
	return nil
}

// defaultBranch reports the repository's default branch, falling back to main.
func defaultBranch(ctx context.Context, repoPath string) string {
	if out, err := git(ctx, repoPath, "symbolic-ref", "--short", "HEAD"); err == nil && out != "" {
		return out
	}
	return "main"
}

// seedRef prefers the upstream tip of the default branch, then the local one,
// then plain HEAD for a repository with no branches to speak of.
func seedRef(ctx context.Context, repoPath string) string {
	base := defaultBranch(ctx, repoPath)
	if _, err := git(ctx, repoPath, "rev-parse", "--verify", "origin/"+base); err == nil {
		return "origin/" + base
	}
	if _, err := git(ctx, repoPath, "rev-parse", "--verify", base); err == nil {
		return base
	}
	return "HEAD"
}

// HasUncommittedChanges reports whether the worktree holds work that would be
// lost if it were removed. Errors count as "yes": when we cannot tell, we must
// not delete.
func HasUncommittedChanges(ctx context.Context, wtPath string) bool {
	out, err := git(ctx, wtPath, "status", "--porcelain")
	if err != nil {
		return true
	}
	return strings.TrimSpace(out) != ""
}

// RemoveWorktree deletes a conversation's worktree. It REFUSES while the tree
// holds uncommitted work — automatic cleanup eating someone's changes is not
// recoverable, so a leftover directory is the better failure. The branch is left
// behind either way: EnsureWorktree re-attaches to it if the conversation comes
// back, and dropping a branch is not cleanup, it is deletion.
func RemoveWorktree(ctx context.Context, repoPath, wtPath string) error {
	// Already gone: drop the stale registration and report success — removing
	// something that is not there is the state the caller asked for.
	if !dirExists(wtPath) {
		_, _ = git(ctx, repoPath, "worktree", "prune")
		return nil
	}
	if HasUncommittedChanges(ctx, wtPath) {
		return fmt.Errorf("커밋되지 않은 변경이 있어 워크트리를 남깁니다: %s", wtPath)
	}
	if out, err := git(ctx, repoPath, "worktree", "remove", wtPath); err != nil {
		return fmt.Errorf("워크트리를 제거하지 못했습니다: %s", out)
	}
	return nil
}

// dirExists reports whether path is present. Split out so RemoveWorktree can
// treat "already gone" as success without shadowing an error it then ignores.
func dirExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
