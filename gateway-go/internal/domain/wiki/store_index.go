package wiki

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ListPages returns wiki page paths in the requested category.
func (s *Store) ListPages(category string) ([]string, error) {
	var searchDir string
	if category != "" {
		searchDir = filepath.Join(s.dir, category)
	} else {
		searchDir = s.dir
	}

	var pages []string
	err := filepath.Walk(searchDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil //nolint:nilerr // skip inaccessible entries in walk
		}
		if info.IsDir() {
			// Prune non-page directories entirely so their pages never leak into
			// a listing — the wiki .git repo, and operator-made backup copies
			// (인물/backup/, 백업/, foo-backup/), which otherwise surfaced as
			// phantom 인물/프로젝트 rows. SkipDir on the search root itself would
			// abort the whole walk, so never prune it. The legit project
			// subfolders (기자재/메일분석/자료/회의록) never match.
			if path != searchDir && isNonPageDir(info.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".md" {
			return nil
		}
		// Skip index and log files.
		base := filepath.Base(path)
		if base == "index.md" || base == "_index.md" || base == "log.md" {
			return nil
		}
		rel, _ := filepath.Rel(s.dir, path)
		pages = append(pages, rel)
		return nil
	})
	return pages, err
}

// isNonPageDir reports whether a directory holds derived/backup state rather
// than live wiki pages, so ListPages can prune it. Matches hidden dirs (.git,
// .trash, …) and any name signalling a backup copy — no real 인물/프로젝트 page
// directory (대표/로그/기자재/메일분석/자료/회의록/거래) is named this way.
//
// The operator/migration backup convention is "<category>.bak-<tag>-<unix>"
// (인물.bak-code-1783485301, 프로젝트.bak-mailcode-…) — a ".bak" INFIX, not a
// prefix or the word "backup", so it must be matched explicitly. Missing this
// was the original leak: PR #3345 only caught "backup"/"백업"/bare "bak", so the
// real .bak-* snapshots surfaced as phantom 인물/프로젝트 rows anyway.
func isNonPageDir(name string) bool {
	if strings.HasPrefix(name, ".") {
		return true
	}
	lower := strings.ToLower(name)
	return strings.Contains(lower, "backup") ||
		strings.Contains(lower, ".bak") ||
		strings.Contains(name, "백업") ||
		lower == "bak"
}

// SnapshotIndex returns a deep copy of the cached master index, safe to walk,
// render, or marshal without holding any lock.
//
// There is deliberately NO accessor returning the live *Index: writers mutate
// the entry map in place under s.mu (writePageInternal → updateEntry), so any
// caller iterating the live map without that lock races them — the exact
// "concurrent map iteration and map write" fatal the old Index() accessor
// caused across a dozen walkers (Tier1Pages, FindSimilarPages, verify, the
// wiki RPC handler, …). Mutations must go through Store methods
// (WritePage/UpdatePage/DeletePage/rebuildIndex/setLastProcessedAndSave).
func (s *Store) SnapshotIndex() *Index {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.index.clone()
}

// snapshotEntries returns a deep copy of the master index's entry map (slice
// fields included, so no aliasing with live entries). The read primitive for
// every index walker outside the store's own locked internals.
func (s *Store) snapshotEntries() map[string]IndexEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneIndexEntries(s.index.Entries)
}

// LastProcessed returns the master index's diary high-water cursor.
func (s *Store) lastProcessedDate() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.index.LastProcessed
}

// setLastProcessedAndSave advances the master index's LastProcessed cursor
// (when date is non-empty; empty keeps the current cursor) and persists the
// index. The locked write path for the dreamer's end-of-cycle cursor save —
// mutating the cursor through a raw index pointer and calling Save (whose
// Render walks the entry map) would race concurrent page writers.
func (s *Store) setLastProcessedAndSave(date string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if date != "" {
		s.index.LastProcessed = date
	}
	return s.index.Save(filepath.Join(s.dir, "index.md"))
}

// rebuildIndex rescans every page from disk and replaces the cached master index
// with a fresh one built from that scan, preserving LastProcessed.
//
// It holds writeMu across the whole scan + swap. That makes the disk state it
// reads a consistent snapshot: because every page writer
// (WritePage/UpdatePage/DeletePage/MarkSuperseded/MergePage/splitPage/...)
// serializes on writeMu, none can land a file-write + index-update in the window
// between this scan and its swap. Without the lock, a write completing mid-scan
// would have its index entry silently dropped by the wholesale swap — the
// on-disk page stays correct, but the cached index goes transiently stale until
// the next write/rebuild self-heals it. That is the same "index agrees with
// disk" invariant writeMu exists to hold.
//
// The critical section spans reading all pages from disk, so it blocks other
// writers for the rebuild's duration. That tradeoff is acceptable here: the wiki
// is a single-user store of at most a few hundred small pages (tens of ms to
// parse), and rebuildIndex runs only from the background dream cycle, never the
// chat hot path. (The pre-existing swap already blocked all index readers under
// s.mu for the same instant; this only extends serialization to the rarer
// writers, and only while a background dream is mid-rebuild.)
func (s *Store) rebuildIndex() error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	pages, err := s.ListPages("")
	if err != nil {
		return fmt.Errorf("list pages: %w", err)
	}

	newIdx := newIndex()
	for _, relPath := range pages {
		page, err := s.ReadPage(relPath)
		if err != nil {
			continue // unreadable/parse error: skip, leave it out of the index
		}
		newIdx.updateEntry(relPath, page)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	// Carry forward the diary high-water cursor from the index being replaced.
	newIdx.LastProcessed = s.index.LastProcessed
	s.index = newIdx
	return newIdx.Save(filepath.Join(s.dir, "index.md"))
}

// Tier1Pages returns all current, non-archived pages with importance >=
// minImportance, sorted by importance descending. Each result includes the page
// path and content. Superseded pages are historical evidence and must never be
// frozen into the Tier1 system prompt.
func (s *Store) Tier1Pages(minImportance float64) []Tier1Result {
	// Snapshot, then walk without the lock — the page reads below do disk I/O
	// and must not iterate the live map writers mutate in place.
	entries := s.snapshotEntries()

	var results []Tier1Result
	for path, entry := range entries {
		if entry.Importance < minImportance {
			continue
		}
		page, err := s.ReadPage(path)
		if err != nil || page.Meta.Archived || strings.TrimSpace(page.Meta.SupersededBy) != "" {
			continue
		}
		// Generated current-facts are injected through the revision-live fact
		// tail. A pre-cutover manual page at the same path remains ordinary user
		// content until it has been byte-preserved and replaced with the marker.
		if isGeneratedFactProjectionPage(path, page) {
			continue
		}
		results = append(results, Tier1Result{
			Path: path,
			Page: page,
		})
	}

	// Sort by importance descending.
	sort.Slice(results, func(i, j int) bool {
		return results[i].Page.Meta.Importance > results[j].Page.Meta.Importance
	})
	return results
}

// Tier1Result is a high-importance wiki page for auto-injection.
type Tier1Result struct {
	Path string
	Page *Page
}

// Stats returns wiki statistics.
func (s *Store) Stats() StoreStats {
	pages, _ := s.ListPages("")
	var totalBytes int64
	catCount := map[string]int{}
	for _, p := range pages {
		abs := filepath.Join(s.dir, p)
		if info, err := os.Stat(abs); err == nil {
			totalBytes += info.Size()
		}
		cat := filepath.Dir(p)
		if cat == "." {
			cat = "(root)"
		}
		catCount[cat]++
	}

	return StoreStats{
		TotalPages:    len(pages),
		TotalBytes:    totalBytes,
		CategoryCount: catCount,
	}
}

// StoreStats holds wiki statistics.
type StoreStats struct {
	TotalPages    int
	TotalBytes    int64
	CategoryCount map[string]int
}
