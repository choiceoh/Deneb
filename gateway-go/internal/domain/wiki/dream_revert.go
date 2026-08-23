// dream_revert.go — structured per-cycle change records and selective revert
// (docs/research/improvement-ideas.md 5.8). WikiChangeSummary was prose: the
// rollback hint pointed at a manual `git revert` of the whole cycle, and there
// was no machine-readable map of which pages a cycle created/updated/merged/
// archived. The cycle record below captures exactly that (keyed by the cycle's
// git commit), RevertDreamCycle formalizes the whole-cycle undo, and
// RevertDreamPages restores individual pages to their pre-cycle state — good
// and bad changes in one commit no longer live or die together.
//
// The audit files (.dream-fact-ledger.jsonl, .dream-cycle-changes.jsonl) are
// deliberately NOT gitignored: unlike cursor state, they are the dreamer's
// accountability trail, and versioning them in the same repository means the
// dreamer cannot silently rewrite its own history.
package wiki

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const dreamCycleChangesFile = ".dream-cycle-changes.jsonl"

// DreamCycleChanges is one dream cycle's page-level mutation map, keyed by the
// git commit that captured the cycle's result.
type DreamCycleChanges struct {
	AtMs   int64  `json:"atMs"`
	Commit string `json:"commit"`
	// Learned: pages the cycle created or updated from proposals.
	Learned []string `json:"learned,omitempty"`
	// Merged: keeper pages of duplicate folds (RefPage detail lives in the
	// fact ledger).
	Merged []string `json:"merged,omitempty"`
	// Expired: pages archived (superseded / retention).
	Expired []string `json:"expired,omitempty"`
	// Moved: new paths of misclassification moves.
	Moved []string `json:"moved,omitempty"`
}

// recordDreamCycleChanges appends one cycle record. Best-effort.
func (wd *WikiDreamer) recordDreamCycleChanges(hash string, cycle *dreamCycle) {
	if wd == nil || wd.store == nil || strings.TrimSpace(hash) == "" {
		return
	}
	rec := DreamCycleChanges{
		AtMs:    time.Now().UnixMilli(),
		Commit:  hash,
		Learned: dedupeStrings(cycle.appliedPaths),
		Merged:  dedupeStrings(cycle.mergedPages),
		Expired: dedupeStrings(cycle.expiredPages),
		Moved:   dedupeStrings(cycle.movedPages),
	}
	raw, err := json.Marshal(rec)
	if err != nil {
		return
	}
	f, err := os.OpenFile(filepath.Join(wd.store.Dir(), dreamCycleChangesFile),
		os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		wd.warnf("wiki-dream: cycle-changes open failed", err)
		return
	}
	defer f.Close()
	if _, err := f.Write(append(raw, '\n')); err != nil {
		wd.warnf("wiki-dream: cycle-changes append failed", err)
	}
}

// LoadRecentDreamCycles returns the newest n cycle records (audit/revert UI).
func (wd *WikiDreamer) LoadRecentDreamCycles(n int) []DreamCycleChanges {
	if wd == nil || wd.store == nil || n <= 0 {
		return nil
	}
	raw, err := os.ReadFile(filepath.Join(wd.store.Dir(), dreamCycleChangesFile))
	if err != nil {
		return nil
	}
	var all []DreamCycleChanges
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var rec DreamCycleChanges
		if json.Unmarshal([]byte(line), &rec) == nil && rec.Commit != "" {
			all = append(all, rec)
		}
	}
	if len(all) > n {
		all = all[len(all)-n:]
	}
	return all
}

// RevertDreamCycle undoes one whole dream cycle (git revert of its snapshot
// commit) and rebuilds the search index to match the restored tree.
func (s *Store) RevertDreamCycle(ctx context.Context, hash string) error {
	hash = strings.TrimSpace(hash)
	if s == nil || hash == "" {
		return fmt.Errorf("wiki: empty cycle hash")
	}
	if out, err := s.git(ctx, "revert", "--no-edit", hash); err != nil {
		return fmt.Errorf("git revert %s: %w (%s)", hash, err, out)
	}
	if err := s.rebuildIndex(); err != nil {
		return fmt.Errorf("rebuild index after revert: %w", err)
	}
	return nil
}

// RevertDreamPages restores the given pages to their pre-cycle (hash^) state:
// a page the cycle UPDATED gets its previous content back, a page the cycle
// CREATED is deleted (it did not exist at hash^). Commits one selective-revert
// snapshot and rebuilds the index. Pages absent from both sides are skipped.
func (s *Store) RevertDreamPages(ctx context.Context, hash string, pages []string) error {
	hash = strings.TrimSpace(hash)
	if s == nil || hash == "" || len(pages) == 0 {
		return fmt.Errorf("wiki: revert-pages needs a cycle hash and pages")
	}
	restored, removed := 0, 0
	for _, p := range pages {
		p = strings.TrimSpace(p)
		if p == "" || strings.Contains(p, "..") || filepath.IsAbs(p) {
			continue // path-safety: only relative in-repo page paths
		}
		content, err := s.git(ctx, "show", hash+"^:"+p)
		if err != nil {
			// Not present before the cycle → the cycle created it → removal is
			// the faithful revert. (git show errors for both missing-in-parent
			// and malformed paths; a malformed path removes nothing below.)
			if _, statErr := os.Stat(filepath.Join(s.dir, p)); statErr == nil {
				if rmErr := os.Remove(filepath.Join(s.dir, p)); rmErr == nil {
					removed++
				}
			}
			continue
		}
		if !strings.HasSuffix(content, "\n") {
			content += "\n"
		}
		if wErr := os.WriteFile(filepath.Join(s.dir, p), []byte(content), 0o644); wErr == nil {
			restored++
		}
	}
	if restored+removed == 0 {
		return fmt.Errorf("wiki: no pages reverted for cycle %s", hash)
	}
	s.SnapshotGit(ctx, fmt.Sprintf("dream: selective revert of %s (%d restored, %d removed)", hash, restored, removed))
	if err := s.rebuildIndex(); err != nil {
		return fmt.Errorf("rebuild index after selective revert: %w", err)
	}
	return nil
}

func dedupeStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}
