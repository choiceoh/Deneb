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
	seen := map[string]bool{self: true, "": true}
	var hits []SimilarHit
	add := func(path, reason string) bool {
		if seen[path] {
			return len(hits) >= limit
		}
		seen[path] = true
		hit := SimilarHit{Path: path, Reason: reason}
		if page, err := s.ReadPage(path); err == nil && page != nil {
			hit.Title = strings.TrimSpace(page.Meta.Title)
			hit.Summary = strings.TrimSpace(page.Meta.Summary)
		}
		hits = append(hits, hit)
		return len(hits) >= limit
	}

	idx := s.Index()

	// 1. Exact frontmatter-ID match (strongest author-intended identity).
	if id := strings.TrimSpace(q.ID); id != "" {
		for path, entry := range idx.Entries {
			if entry.ID == id && add(path, "id") {
				return hits
			}
		}
	}

	// 2. Slug-normalized path equality ("프로젝트 A" / "프로젝트-a" / "프로젝트_A").
	if self != "" {
		proposed := normalizeSlug(self)
		for path := range idx.Entries {
			if normalizeSlug(path) == proposed && add(path, "slug") {
				return hits
			}
		}
	}

	// 3. FTS title match, bounded to the same category so a common word can't
	// cross-link unrelated buckets.
	if title := strings.TrimSpace(q.Title); title != "" && s.fts != nil {
		results, err := s.fts.search(ctx, title, limit+2)
		if err == nil {
			for _, r := range results {
				if r.Score < similarTitleFloor {
					continue
				}
				if q.Category != "" && !strings.HasPrefix(r.Path, q.Category+"/") {
					continue
				}
				if add(r.Path, "title") {
					return hits
				}
			}
		}
	}
	return hits
}

// ChooseDuplicateKeeper picks which of two duplicate pages survives a fold:
// the higher-importance page, with a later Updated date breaking ties (the
// same policy the dream cycle's exact-duplicate auto-merge uses).
func (s *Store) ChooseDuplicateKeeper(a, b string) (keep, fold string) {
	if dupKeepSecond(s.Index(), a, b) {
		return b, a
	}
	return a, b
}
