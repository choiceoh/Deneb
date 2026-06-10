// gitsnap.go — git versioning for the wiki directory.
//
// The wiki is the agent's curated long-term memory and the dreamer rewrites
// pages autonomously. A plain directory gives no history: a bad LLM cycle or
// an operator mistake silently destroys the previous state of a fact. Keeping
// the wiki as a local git repository makes every consolidation cycle a commit
// — history, diff, and rollback come for free, with zero new infrastructure.
//
// Snapshots are best-effort: a missing git binary or a failing commit must
// never break dreaming or backups, only log.
package wiki

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// gitSnapTimeout bounds each git invocation. The wiki is small (hundreds of
// pages); anything slower than this means git is wedged, not busy.
const gitSnapTimeout = 30 * time.Second

// wikiGitIgnore excludes derived/state files from version control: embedding
// cache, dreamer cursors, and write-temp artifacts churn on every cycle and
// would bury the meaningful page history.
const wikiGitIgnore = `.semantic-cache.json
.diary-process-state.json
.dream-last-proposal.json
*.tmp
*.lock
`

var gitMissingOnce sync.Once

// SnapshotGit commits the current wiki state with the given message. The
// repository is created lazily on first use. No-op (with a Warn) when the git
// binary is unavailable; no commit is created when nothing changed.
func (s *Store) SnapshotGit(ctx context.Context, message string) {
	if _, err := exec.LookPath("git"); err != nil {
		gitMissingOnce.Do(func() {
			slog.Warn("wiki: git not found; memory versioning disabled")
		})
		return
	}
	if err := s.ensureGitRepo(ctx); err != nil {
		slog.Warn("wiki: git init failed; snapshot skipped", "error", err)
		return
	}

	if out, err := s.git(ctx, "add", "-A"); err != nil {
		slog.Warn("wiki: git add failed; snapshot skipped", "error", err, "output", out)
		return
	}
	status, err := s.git(ctx, "status", "--porcelain")
	if err != nil {
		slog.Warn("wiki: git status failed; snapshot skipped", "error", err)
		return
	}
	if strings.TrimSpace(status) == "" {
		return // nothing changed since the last snapshot
	}
	if message == "" {
		message = "wiki snapshot"
	}
	if out, err := s.git(ctx, "commit", "-m", message); err != nil {
		slog.Warn("wiki: git commit failed", "error", err, "output", out)
		return
	}
	slog.Info("wiki: git snapshot committed", "message", message)
}

// ensureGitRepo initializes the wiki git repository on first use, with a
// repo-local identity (no dependency on the host's global git config) and a
// .gitignore for derived state files.
func (s *Store) ensureGitRepo(ctx context.Context) error {
	if _, err := os.Stat(filepath.Join(s.dir, ".git")); err == nil {
		return nil
	}
	if out, err := s.git(ctx, "init", "-q"); err != nil {
		return fmt.Errorf("git init: %w (%s)", err, out)
	}
	if out, err := s.git(ctx, "config", "user.name", "Deneb"); err != nil {
		return fmt.Errorf("git config user.name: %w (%s)", err, out)
	}
	if out, err := s.git(ctx, "config", "user.email", "deneb@localhost"); err != nil {
		return fmt.Errorf("git config user.email: %w (%s)", err, out)
	}
	ignorePath := filepath.Join(s.dir, ".gitignore")
	if _, err := os.Stat(ignorePath); os.IsNotExist(err) {
		if werr := os.WriteFile(ignorePath, []byte(wikiGitIgnore), 0o644); werr != nil {
			return fmt.Errorf("write .gitignore: %w", werr)
		}
	}
	return nil
}

// git runs a git subcommand rooted at the wiki directory.
func (s *Store) git(ctx context.Context, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, gitSnapTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = s.dir
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}
