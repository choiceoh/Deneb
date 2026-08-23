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
// cache, dreamer cursors, append-only ledgers, and write-temp artifacts churn
// on every cycle and would bury the meaningful page history.
//
// The ledgers below are appended several times an hour and each commit stores a
// fresh whole-file blob, so they dominated the repository's loose objects
// (measured 2026-08-23: 39MB loose vs 6.8MB packed, with the ledgers plus the
// regenerated index/log among the largest contributors). They are still backed
// up — the daily offsite tar takes untracked files too. `.deals.jsonl` stays
// tracked on purpose: it is the financial audit surface, not derived state.
const wikiGitIgnore = `.semantic-cache.json
.semantic-cache.f32
.diary-semantic-cache.json
.diary-process-state.json
.dream-last-proposal.json
.recall-hits.jsonl
.verify-findings.json
.dream-selfcompare.jsonl
.dream-fact-ledger.jsonl
.wiki.db
.wiki.db-shm
.wiki.db-wal
*.tmp
*.lock
`

var gitMissingOnce sync.Once

// SnapshotGit commits the current wiki state with the given message and
// returns the short commit hash ("" when nothing changed or git is
// unavailable). The repository is created lazily on first use.
func (s *Store) SnapshotGit(ctx context.Context, message string) string {
	if _, err := exec.LookPath("git"); err != nil {
		gitMissingOnce.Do(func() {
			slog.Warn("wiki: git not found; memory versioning disabled")
		})
		return ""
	}
	if err := s.ensureGitRepo(ctx); err != nil {
		slog.Warn("wiki: git init failed; snapshot skipped", "error", err)
		return ""
	}

	if out, err := s.git(ctx, "add", "-A"); err != nil {
		slog.Warn("wiki: git add failed; snapshot skipped", "error", err, "output", out)
		return ""
	}
	status, err := s.git(ctx, "status", "--porcelain")
	if err != nil {
		slog.Warn("wiki: git status failed; snapshot skipped", "error", err)
		return ""
	}
	if strings.TrimSpace(status) == "" {
		return "" // nothing changed since the last snapshot
	}
	if message == "" {
		message = "wiki snapshot"
	}
	if out, err := s.git(ctx, "commit", "-m", message); err != nil {
		slog.Warn("wiki: git commit failed", "error", err, "output", out)
		return ""
	}
	hash, err := s.git(ctx, "rev-parse", "--short", "HEAD")
	if err != nil {
		hash = ""
	}
	slog.Info("wiki: git snapshot committed", "message", message, "commit", hash)
	return hash
}

// gitSnapshotStat returns the diffstat of a snapshot commit ("" on any
// failure) — used for the dream-cycle change report.
func (s *Store) gitSnapshotStat(ctx context.Context, hash string) string {
	if hash == "" {
		return ""
	}
	out, err := s.git(ctx, "show", "--stat", "--format=", hash)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
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
	if err := s.reconcileGitIgnore(); err != nil {
		return err
	}
	// Pack early. Nothing here ever runs `git gc`, so the only maintenance is
	// the one `git commit` triggers at gc.auto — 6700 loose objects by default,
	// which at this repo's ~24KB/object is well over 100MB of loose files
	// before anything is packed. autoDetach=false keeps the pack inside the
	// snapshot's own 30s timeout instead of leaving a background process behind.
	if out, err := s.git(ctx, "config", "gc.auto", "512"); err != nil {
		return fmt.Errorf("git config gc.auto: %w (%s)", err, out)
	}
	if out, err := s.git(ctx, "config", "gc.autoDetach", "false"); err != nil {
		return fmt.Errorf("git config gc.autoDetach: %w (%s)", err, out)
	}
	return nil
}

// reconcileGitIgnore creates .gitignore, or appends the entries a newer release
// added. It used to be written once at repo creation, so every ignore rule
// added afterwards silently did nothing on existing wikis — which is why the
// dreamer ledgers were still tracked months later. Existing lines are never
// removed: an operator may have added their own.
func (s *Store) reconcileGitIgnore() error {
	ignorePath := filepath.Join(s.dir, ".gitignore")
	current, err := os.ReadFile(ignorePath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read .gitignore: %w", err)
	}
	have := make(map[string]bool)
	for _, line := range strings.Split(string(current), "\n") {
		have[strings.TrimSpace(line)] = true
	}
	var missing []string
	for _, want := range strings.Split(strings.TrimSpace(wikiGitIgnore), "\n") {
		want = strings.TrimSpace(want)
		if want != "" && !have[want] {
			missing = append(missing, want)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	body := string(current)
	if body != "" && !strings.HasSuffix(body, "\n") {
		body += "\n"
	}
	body += strings.Join(missing, "\n") + "\n"
	if werr := os.WriteFile(ignorePath, []byte(body), 0o644); werr != nil {
		return fmt.Errorf("write .gitignore: %w", werr)
	}
	return nil
}

// formatWikiChangeSummary renders the per-cycle change block appended to the
// dream notification: which pages, which snapshot, and how to undo it. The
// rollback hint is a concrete command so "되돌려줘" needs no archaeology.
func formatWikiChangeSummary(hash, stat, wikiDir string, paths []string) string {
	var sb strings.Builder
	if len(paths) > 0 {
		show := paths
		if len(show) > 6 {
			show = show[:6]
		}
		sb.WriteString("📄 " + strings.Join(show, ", "))
		if extra := len(paths) - len(show); extra > 0 {
			fmt.Fprintf(&sb, " 외 %d", extra)
		}
		sb.WriteString("\n")
	}
	sb.WriteString("🧾 스냅샷 " + hash)
	if stat != "" {
		lines := strings.Split(stat, "\n")
		if last := strings.TrimSpace(lines[len(lines)-1]); last != "" {
			sb.WriteString(" — " + last)
		}
	}
	fmt.Fprintf(&sb, "\n↩️ 되돌리기: `git -C %s revert --no-edit %s`", wikiDir, hash)
	return sb.String()
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
