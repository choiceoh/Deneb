// link_prune.go — dead-reference hygiene for the knowledge graph.
//
// Related[] entries accumulate rot: pages get merged/deleted/moved, LLM writes
// occasionally emit category-less paths, "w:" prefixes, or titles instead of
// paths. Dangling entries silently degrade every graph consumer (backlinks,
// projectOwnedRefs, PageConnections). PruneDeadRelatedLinks repairs what it
// can — deterministically — and drops what it can't:
//
//	repair: exact path (extension/prefix normalization) → legacy flat 대표페이지
//	        → unique basename → unique title → unique frontmatter ID
//	drop:   unresolvable, duplicates, self-references
//
// Body [[wikilinks]] are deliberately untouched (prose is the author's; graph
// resolvers already handle title-form links). Runs from the wiki-review task
// every cycle; cheap (index-driven) and idempotent — after the first sweep it
// only acts on fresh rot.
package wiki

import (
	"fmt"
	"path"
	"strings"
)

// PruneStats summarizes one dead-link sweep.
type PruneStats struct {
	PagesChanged int
	Repointed    int
	Removed      int
}

// linkResolver holds the lookup sets one sweep resolves against.
type linkResolver struct {
	exists     map[string]bool
	byBasename map[string][]string
	byTitle    map[string][]string
	byID       map[string][]string
}

func (s *Store) newLinkResolver() *linkResolver {
	idx := s.Index()
	r := &linkResolver{
		exists:     make(map[string]bool, len(idx.Entries)),
		byBasename: make(map[string][]string),
		byTitle:    make(map[string][]string),
		byID:       make(map[string][]string),
	}
	for p, entry := range idx.Entries {
		r.exists[p] = true
		r.byBasename[path.Base(p)] = append(r.byBasename[path.Base(p)], p)
		if t := strings.TrimSpace(entry.Title); t != "" {
			r.byTitle[t] = append(r.byTitle[t], p)
		}
		if id := strings.TrimSpace(entry.ID); id != "" {
			r.byID[id] = append(r.byID[id], p)
		}
	}
	return r
}

// resolve maps one Related entry to its canonical existing path, or "" when
// the reference is dead beyond repair. Repairs are applied only when
// unambiguous — a guess that could point at the wrong page is worse than a
// dropped edge.
func (r *linkResolver) resolve(ref string) string {
	ref = strings.TrimSpace(ref)
	ref = strings.TrimPrefix(ref, "w:") // knowledge-router namespace leak
	ref = strings.Trim(ref, "[]")       // stray wikilink brackets
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ""
	}
	// 1. Exact path, with the .md extension normalized on.
	if p := normalizePagePath(ref); r.exists[p] {
		return p
	}
	p := normalizePagePath(ref)
	// 2. Legacy flat 대표페이지 form → the in-folder slot.
	if name, ok := ProjectNameOf(p); ok && len(strings.Split(p, "/")) == 2 {
		if rep := RepPagePath(name); r.exists[rep] {
			return rep
		}
	}
	// 3. Unique basename (catches category-less and moved paths).
	if cands := r.byBasename[path.Base(p)]; len(cands) == 1 {
		return cands[0]
	}
	// 4. Unique exact title (LLMs sometimes write titles into related).
	if cands := r.byTitle[ref]; len(cands) == 1 {
		return cands[0]
	}
	// 5. Unique frontmatter ID.
	if cands := r.byID[ref]; len(cands) == 1 {
		return cands[0]
	}
	return ""
}

// PruneDeadRelatedLinks sweeps every page's Related list, repairing or
// dropping dead entries. Hygiene-only writes: the page's Updated date is NOT
// stamped (a metadata repair must not make a dormant page look active).
func (s *Store) PruneDeadRelatedLinks() (PruneStats, error) {
	pages, err := s.ListPages("")
	if err != nil {
		return PruneStats{}, fmt.Errorf("wiki: prune links: %w", err)
	}
	resolver := s.newLinkResolver()

	var stats PruneStats
	for _, rp := range pages {
		rp = strings.ReplaceAll(rp, "\\", "/")
		repointed, removed := 0, 0
		if err := s.UpdatePage(rp, func(cur *Page) (*Page, error) {
			if cur == nil || len(cur.Meta.Related) == 0 {
				return nil, nil
			}
			seen := make(map[string]bool, len(cur.Meta.Related))
			rebuilt := make([]string, 0, len(cur.Meta.Related))
			changed := false
			for _, ref := range cur.Meta.Related {
				target := resolver.resolve(ref)
				if target == "" { // dead beyond repair
					removed++
					changed = true
					continue
				}
				if target == rp || seen[target] { // self-reference / duplicate
					removed++
					changed = true
					continue
				}
				if target != ref { // repaired: normalized, repointed, or trimmed
					repointed++
					changed = true
				}
				seen[target] = true
				rebuilt = append(rebuilt, target)
			}
			if !changed {
				return nil, nil // no-op — keep Updated and the index untouched
			}
			cur.Meta.Related = rebuilt
			return cur, nil
		}); err != nil {
			continue // best-effort: one unwritable page must not stop the sweep
		}
		if repointed+removed > 0 {
			stats.PagesChanged++
			stats.Repointed += repointed
			stats.Removed += removed
		}
	}
	if stats.PagesChanged > 0 {
		_ = s.AppendLog("prune-links", fmt.Sprintf("%d개 페이지 — 복구 %d건, 제거 %d건",
			stats.PagesChanged, stats.Repointed, stats.Removed))
	}
	return stats, nil
}
