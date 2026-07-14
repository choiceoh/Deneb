// similar.go — shared near-duplicate page detection: the one "does a page like
// this already exist?" primitive behind (1) the dreamer's create-dedup, (2) the
// wiki tool's pre-write guard, and (3) the background wiki reviewer. One
// implementation so all three defenses agree on what "similar" means.
package wiki

import (
	"context"
	"strings"
)

// SimilarQuery describes a page identity being checked for existing near-matches.
type SimilarQuery struct {
	Path     string // proposed page path (used for slug comparison; may be "")
	ID       string // proposed frontmatter ID (exact match signal)
	Code     string // frozen project code (pl3-kia-mod-001) — same code = same project
	Title    string // proposed title (FTS signal)
	Category string // taxonomy category to bound FTS matches ("" = any)
}

// SimilarHit is one existing page that likely covers the same subject.
type SimilarHit struct {
	Path    string
	Title   string
	Summary string
	Reason  string // "id" | "slug" | "title"
}

// similarTitleFloor is the FTS score under which a title match is ignored.
// Tuned with the dreamer's historical threshold (see findExistingPage).
const similarTitleFloor = 0.6

// FindSimilarPages returns up to limit existing pages that likely cover the
// same subject as q, strongest signal first: exact frontmatter-ID match, then
// slug-normalized path equality, then a high-scoring FTS title match inside the
// same category. q.Path itself is never returned. Read-only and cheap — the
// FTS query runs only when the title is present.
func (s *Store) FindSimilarPages(ctx context.Context, q SimilarQuery, limit int) []SimilarHit {
	if s == nil || limit <= 0 {
		return nil
	}
	self := normalizePagePath(q.Path)
	c := &similarCollector{
		store: s,
		seen:  map[string]bool{self: true, "": true},
		limit: limit,
	}

	// Snapshot: the walks below must not iterate the live index map while
	// writers mutate it in place (concurrent map iteration and write is fatal).
	entries := s.snapshotEntries()

	if c.matchByID(entries, q.ID) ||
		c.matchByProjectCode(q.Code) ||
		c.matchBySlug(entries, self) ||
		c.matchByTitle(ctx, q) {
		return c.hits
	}
	return c.hits
}

// similarCollector accumulates FindSimilarPages hits, deduplicated by path and
// capped at limit. Each match* stage reports true once the limit is reached so
// the caller can stop early — hit order stays strongest-signal-first.
type similarCollector struct {
	store *Store
	seen  map[string]bool
	hits  []SimilarHit
	limit int
}

// add records path as a hit (unless already seen) and reports whether the hit
// limit has been reached.
func (c *similarCollector) add(path, reason string) bool {
	if c.seen[path] {
		return len(c.hits) >= c.limit
	}
	c.seen[path] = true
	hit := SimilarHit{Path: path, Reason: reason}
	if page, err := c.store.ReadPage(path); err == nil && page != nil {
		hit.Title = strings.TrimSpace(page.Meta.Title)
		hit.Summary = strings.TrimSpace(page.Meta.Summary)
	}
	c.hits = append(c.hits, hit)
	return len(c.hits) >= c.limit
}

// matchByID collects stage 1: exact frontmatter-ID match (strongest
// author-intended identity).
func (c *similarCollector) matchByID(entries map[string]IndexEntry, rawID string) bool {
	id := strings.TrimSpace(rawID)
	if id == "" {
		return false
	}
	for path, entry := range entries {
		if entry.ID == id && c.add(path, "id") {
			return true
		}
	}
	return false
}

// matchByProjectCode collects stage 1.5: frozen project code. Two 대표페이지
// sharing a code ARE the same project no matter how differently they're named —
// the move-stable identity is exactly what survives title/path splintering. Rep
// pages only (children inherit the code by folder, so matching them would flag
// every sub-page).
func (c *similarCollector) matchByProjectCode(rawCode string) bool {
	code := normalizeProjectCode(rawCode)
	if code == "" {
		return false
	}
	for _, ref := range c.store.knownProjects() {
		if normalizeProjectCode(ref.Code) == code && c.add(ref.Path, "code") {
			return true
		}
	}
	return false
}

// matchBySlug collects stage 2: slug-normalized path equality
// ("프로젝트 A" / "프로젝트-a" / "프로젝트_A").
func (c *similarCollector) matchBySlug(entries map[string]IndexEntry, self string) bool {
	if self == "" {
		return false
	}
	proposed := normalizeSlug(self)
	for path := range entries {
		if normalizeSlug(path) == proposed && c.add(path, "slug") {
			return true
		}
	}
	return false
}

// matchByTitle collects stage 3: FTS title match, bounded to the same category
// so a common word can't cross-link unrelated buckets.
func (c *similarCollector) matchByTitle(ctx context.Context, q SimilarQuery) bool {
	title := strings.TrimSpace(q.Title)
	if title == "" || c.store.fts == nil {
		return false
	}
	results, err := c.store.fts.search(ctx, title, c.limit+2)
	if err != nil {
		return false
	}
	for _, r := range results {
		if r.Score < similarTitleFloor {
			continue
		}
		if q.Category != "" && !strings.HasPrefix(r.Path, q.Category+"/") {
			continue
		}
		if c.add(r.Path, "title") {
			return true
		}
	}
	return false
}

// ChooseDuplicateKeeper picks which of two duplicate pages survives a fold:
// the higher-importance page, with a later Updated date breaking ties (the
// same policy the dream cycle's exact-duplicate auto-merge uses).
func (s *Store) ChooseDuplicateKeeper(a, b string) (keep, fold string) {
	if dupKeepSecond(s.snapshotEntries(), a, b) {
		return b, a
	}
	return a, b
}
