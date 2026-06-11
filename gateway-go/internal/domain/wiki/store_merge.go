// store_merge.go — page lifecycle beyond simple CRUD: supersession stamps
// (MarkSuperseded) and the duplicate-page merge pass (MergePage), including
// reference repointing and frontmatter/related-list union helpers. Split
// from store.go (Store core).
package wiki

import (
	"fmt"
	"strings"
	"time"
)

// MarkSuperseded stamps oldPath as replaced by newPath: the page stays
// readable (history is memory too) but search demotes it so the stale fact
// stops surfacing as current. Idempotent; refuses self-supersession.
func (s *Store) MarkSuperseded(oldPath, newPath string) error {
	oldPath = normalizePagePath(oldPath)
	newPath = normalizePagePath(newPath)
	if oldPath == "" || newPath == "" || oldPath == newPath {
		return nil
	}
	page, err := s.ReadPage(oldPath)
	if err != nil {
		return fmt.Errorf("wiki: mark superseded: %w", err)
	}
	if page.Meta.SupersededBy == newPath {
		return nil
	}
	page.Meta.SupersededBy = newPath
	page.Meta.Updated = time.Now().Format("2006-01-02")
	return s.writePageInternal(oldPath, page, true)
}

func toSet(ss []string) map[string]struct{} {
	m := make(map[string]struct{}, len(ss))
	for _, s := range ss {
		m[s] = struct{}{}
	}
	return m
}

// MergeOptions reserves room for future merge-policy knobs. v1 uses fixed
// defaults (see MergePage), so the struct is currently empty.
type MergeOptions struct{}

// MergeResult summarizes a completed page merge.
type MergeResult struct {
	TargetPath    string // the surviving page
	MergedTitle   string // target's title after the merge
	RewriteCount  int    // other pages whose Related was repointed source→target
	SourceRemoved bool   // whether the source page was deleted
}

// MergePage folds sourcePath into targetPath and deletes source. The target
// keeps its identity (title, category, path) and takes mergedBody as its new
// body; the two pages' frontmatter is unioned (see mergeFrontmatterInto); every
// other page that referenced source is repointed to target; and source is
// removed from disk, the master index, and the search index.
//
// mergedBody is supplied by the caller (LLM-synthesized, or a plain
// concatenation fallback) so this method carries no LLM dependency — the domain
// layer stays pure.
//
// Ordering is deliberate for crash-safety: target is written first (so the
// merged body is durable before anything is destroyed), references are
// repointed next, and source is deleted last. A failure partway through leaves
// source intact and the merge re-runnable, with no data loss.
//
// Backlinks are managed manually here (every write passes skipBacklinks=true)
// because the merge already rewrites both link directions itself; letting
// maintainBacklinks also fire would double-process the same edges.
func (s *Store) MergePage(targetPath, sourcePath, mergedBody string, _ MergeOptions) (MergeResult, error) {
	targetPath = strings.TrimSpace(targetPath)
	sourcePath = strings.TrimSpace(sourcePath)
	if targetPath == "" || sourcePath == "" {
		return MergeResult{}, fmt.Errorf("wiki: merge needs both target and source paths")
	}
	if targetPath == sourcePath {
		return MergeResult{}, fmt.Errorf("wiki: cannot merge a page into itself")
	}

	target, err := s.ReadPage(targetPath)
	if err != nil {
		return MergeResult{}, fmt.Errorf("wiki: read merge target %q: %w", targetPath, err)
	}
	source, err := s.ReadPage(sourcePath)
	if err != nil {
		return MergeResult{}, fmt.Errorf("wiki: read merge source %q: %w", sourcePath, err)
	}

	// Collect every page that references source. The index scan is the source
	// of truth (robust against backlink-mirror drift); source's own Related is
	// folded in too in case the index lags. target/source/blank are excluded.
	refSet := make(map[string]struct{})
	for _, p := range s.findPagesReferencingPath(sourcePath) {
		refSet[p] = struct{}{}
	}
	for _, r := range source.Meta.Related {
		refSet[strings.TrimSpace(r)] = struct{}{}
	}
	delete(refSet, targetPath)
	delete(refSet, sourcePath)
	delete(refSet, "")

	// 1. Body — caller-supplied merged text. Guard against an empty body
	//    silently wiping content: fall back to a plain concatenation.
	if strings.TrimSpace(mergedBody) != "" {
		target.Body = mergedBody
	} else {
		target.Body = strings.TrimSpace(target.Body + "\n\n" + source.Body)
	}

	// 2. Frontmatter union (tags, importance, due, summary, created).
	mergeFrontmatterInto(&target.Meta, source.Meta)

	// 3. Related = target's ∪ source's ∪ referencing-pages, minus self+source.
	exclude := map[string]struct{}{targetPath: {}, sourcePath: {}}
	target.Meta.Related = unionRelated(exclude,
		target.Meta.Related, source.Meta.Related, keysOf(refSet))
	target.Meta.Updated = time.Now().Format("2006-01-02")

	// 4. Write target first (manual backlinks → skip auto maintenance).
	if err := s.writePageInternal(targetPath, target, true); err != nil {
		return MergeResult{}, fmt.Errorf("wiki: write merge target: %w", err)
	}

	// 5. Repoint each referencing page: source → target.
	rewrites := 0
	for p := range refSet {
		if s.repointReference(p, sourcePath, targetPath) {
			rewrites++
		}
	}

	// 6. Delete source last. Its neighbors were already repointed above, so
	//    DeletePage's own backlink cleanup is a harmless no-op.
	if err := s.DeletePage(sourcePath); err != nil {
		return MergeResult{TargetPath: targetPath, MergedTitle: target.Meta.Title, RewriteCount: rewrites},
			fmt.Errorf("wiki: delete merge source: %w", err)
	}

	_ = s.AppendLog("merge", targetPath+" ← "+sourcePath+" — "+target.Meta.Title) // best-effort: audit log is non-critical

	return MergeResult{
		TargetPath:    targetPath,
		MergedTitle:   target.Meta.Title,
		RewriteCount:  rewrites,
		SourceRemoved: true,
	}, nil
}

// findPagesReferencingPath scans the master index for every page (other than
// relPath itself) whose Related list contains relPath. Index-based so it sees
// all inbound references regardless of any backlink-mirror drift.
func (s *Store) findPagesReferencingPath(relPath string) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []string
	for path, entry := range s.index.Entries {
		if path == relPath {
			continue
		}
		for _, r := range entry.Related {
			if r == relPath {
				out = append(out, path)
				break
			}
		}
	}
	return out
}

// repointReference rewrites oldRef→newRef in relPath's Related list (dedup,
// dropping any self-reference) and persists with skipBacklinks=true. Returns
// true if the page changed. Best-effort: an unreadable page is skipped.
func (s *Store) repointReference(relPath, oldRef, newRef string) bool {
	page, err := s.ReadPage(relPath)
	if err != nil {
		return false
	}
	seen := make(map[string]struct{}, len(page.Meta.Related))
	rebuilt := make([]string, 0, len(page.Meta.Related))
	changed := false
	for _, r := range page.Meta.Related {
		if r == oldRef {
			r = newRef
			changed = true
		}
		if r == relPath { // never self-reference
			changed = true
			continue
		}
		if _, dup := seen[r]; dup {
			continue
		}
		seen[r] = struct{}{}
		rebuilt = append(rebuilt, r)
	}
	if !changed {
		return false
	}
	page.Meta.Related = rebuilt
	page.Meta.Updated = time.Now().Format("2006-01-02")
	_ = s.writePageInternal(relPath, page, true) // best-effort: reference repoint is non-critical
	return true
}

// mergeFrontmatterInto folds src's frontmatter into dst while keeping dst's
// identity. Tags become a union; importance takes the max; due/created take the
// earlier date (a merged entity's history starts at the earliest); summary
// fills in from src only when dst's is empty. Title, Category, Type,
// Confidence, Archived, and ID stay dst's.
func mergeFrontmatterInto(dst *Frontmatter, src Frontmatter) {
	dst.Tags = unionStrings(dst.Tags, src.Tags)
	if src.Importance > dst.Importance {
		dst.Importance = src.Importance
	}
	if strings.TrimSpace(dst.Summary) == "" {
		dst.Summary = src.Summary
	}
	dst.Due = earlierDate(dst.Due, src.Due)
	dst.Created = earlierDate(dst.Created, src.Created)
}

// keysOf returns the keys of a set in arbitrary order.
func keysOf(set map[string]struct{}) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	return out
}

// unionRelated concatenates related-path lists in order, dropping blanks,
// excluded paths, and duplicates (first occurrence wins).
func unionRelated(exclude map[string]struct{}, lists ...[]string) []string {
	seen := make(map[string]struct{})
	var out []string
	for _, list := range lists {
		for _, r := range list {
			r = strings.TrimSpace(r)
			if r == "" {
				continue
			}
			if _, ex := exclude[r]; ex {
				continue
			}
			if _, dup := seen[r]; dup {
				continue
			}
			seen[r] = struct{}{}
			out = append(out, r)
		}
	}
	return out
}

// unionStrings merges two string slices preserving a-first order, trimming
// blanks and removing duplicates.
func unionStrings(a, b []string) []string {
	seen := make(map[string]struct{})
	var out []string
	for _, list := range [][]string{a, b} {
		for _, v := range list {
			v = strings.TrimSpace(v)
			if v == "" {
				continue
			}
			if _, dup := seen[v]; dup {
				continue
			}
			seen[v] = struct{}{}
			out = append(out, v)
		}
	}
	return out
}

// earlierDate returns the lexicographically smaller of two YYYY-MM-DD strings
// (ISO dates sort chronologically), ignoring empties.
func earlierDate(a, b string) string {
	a, b = strings.TrimSpace(a), strings.TrimSpace(b)
	switch {
	case a == "":
		return b
	case b == "":
		return a
	case a <= b:
		return a
	default:
		return b
	}
}

// ListPages returns all page paths in a category (e.g., "기술").
// If category is empty, returns all pages.
