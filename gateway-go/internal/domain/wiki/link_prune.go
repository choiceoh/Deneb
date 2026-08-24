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
// Body [[wikilinks]] get the repair half of the same ladder (PruneDeadWikiLinks):
// an unambiguous rename is rewritten in place, anything ambiguous or truly gone
// is left as prose for a human. Runs from the wiki-review task
// every cycle; cheap (index-driven) and idempotent — after the first sweep it
// only acts on fresh rot.
package wiki

import (
	"fmt"
	"log/slog"
	"path"
	"path/filepath"
	"strings"
)

// DeadWikiLink is one body [[link]] whose path-shaped target no page resolves.
// The review task tracks these across sweeps: a target can come back (a purged
// person gets re-seeded, a page returns under the same path), so a dead link is
// given a grace window to self-heal before it is unwrapped to prose.
type DeadWikiLink struct {
	Page   string
	Target string
}

// PruneStats summarizes one dead-link sweep.
type PruneStats struct {
	PagesChanged int
	Repointed    int
	Removed      int
	// Failed counts pages whose rebuilt Related list could not be written back
	// (the sweep is best-effort per page; failures must still be visible).
	Failed int
}

// linkResolver holds the lookup sets one sweep resolves against.
type linkResolver struct {
	// indexEntries is the snapshot size the title/ID maps were built from —
	// the sweep skips when the index is empty while pages exist (lost index:
	// title/ID repairs would silently degrade to drops).
	indexEntries int
	exists       map[string]bool
	byBasename   map[string][]string
	byTitle      map[string][]string
	byID         map[string][]string
	// codes holds every frozen project code carried by a 프로젝트/ page
	// (normalized). Code-form Related entries are first-class move-stable edges
	// (graph_query and projectOwnedRefs resolve them), not paths — the sweep
	// must preserve them while the code is alive. Built from page frontmatter
	// (the index doesn't carry Code) and including archived pages, so an edge
	// into a CLOSED project survives too.
	codes map[string]bool
}

// newLinkResolver builds the sweep's lookup sets. pages is the ON-DISK page
// list the caller already walked (PruneDeadRelatedLinks) — existence and
// basename resolution key off it, NOT the index: the index can lag disk (a
// crash between page write and index save), and an index-based exists map
// deemed real pages dead and stripped their inbound references. Title/ID
// resolution stays index-based (only the index carries those) via a snapshot,
// so a stale index merely skips a repair, never prunes a live page.
func (s *Store) newLinkResolver(pages []string) *linkResolver {
	entries := s.snapshotEntries()
	r := &linkResolver{
		indexEntries: len(entries),
		exists:       make(map[string]bool, len(pages)),
		byBasename:   make(map[string][]string),
		byTitle:      make(map[string][]string),
		byID:         make(map[string][]string),
		codes:        make(map[string]bool),
	}
	for _, p := range pages {
		p = filepath.ToSlash(p)
		r.exists[p] = true
		r.byBasename[path.Base(p)] = append(r.byBasename[path.Base(p)], p)
	}
	for p, entry := range entries {
		if t := strings.TrimSpace(entry.Title); t != "" {
			r.byTitle[t] = append(r.byTitle[t], p)
		}
		if id := strings.TrimSpace(entry.ID); id != "" {
			r.byID[id] = append(r.byID[id], p)
		}
	}
	// Live project codes come from page frontmatter (bounded to 프로젝트/ pages —
	// only they carry codes; see dreamer_code.go).
	for _, p := range pages {
		p = filepath.ToSlash(p)
		if !strings.HasPrefix(p, projectCategoryPrefix+"/") {
			continue
		}
		if page, perr := s.ReadPage(p); perr == nil && page != nil {
			if c := normalizeProjectCode(page.Meta.Code); c != "" {
				r.codes[c] = true
			}
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
	ref = strings.TrimPrefix(ref, "w:")      // knowledge-router namespace leak
	ref = strings.Trim(ref, "[]")            // stray wikilink brackets
	ref = strings.ReplaceAll(ref, "\\", "/") // windows separators
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ""
	}
	// 0. Frozen project code — a first-class move-stable edge, not a path.
	// Preserved verbatim (repointing to the rep page would forfeit exactly the
	// move-stability the code form exists for).
	if c := normalizeProjectCode(ref); c != "" && r.codes[c] {
		return ref
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
// dropping dead entries. Hygiene-only writes: neither the swept page's Updated
// date nor (via backlink maintenance) any target page's is stamped — a
// metadata repair must not make a dormant page look active.
func (s *Store) PruneDeadRelatedLinks() (PruneStats, error) {
	pages, err := s.ListPages("")
	if err != nil {
		return PruneStats{}, fmt.Errorf("wiki: prune links: %w", err)
	}
	resolver := s.newLinkResolver(pages)
	if resolver.indexEntries == 0 && len(pages) > 0 {
		// Index lost/empty while pages exist on disk (e.g. index.md wiped, then
		// rebuilt empty on startup). Path-form refs would still resolve against
		// the on-disk exists map, but title/ID-form refs would silently turn
		// from "repairable" into "removed". Skip; the next index rebuild/write
		// self-heals and the sweep resumes.
		slog.Warn("wiki: prune links skipped — index empty but pages exist", "pages", len(pages))
		return PruneStats{}, nil
	}

	var stats PruneStats
	for _, rp := range pages {
		rp = strings.ReplaceAll(rp, "\\", "/")
		if isFactProjectionReservedPath(rp) {
			continue
		}
		repointed, removed := 0, 0
		if err := s.UpdatePageMetaOnly(rp, func(cur *Page) (*Page, error) {
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
			// Best-effort: one unwritable page must not stop the sweep — but a
			// silent skip hides real rot repair failing forever.
			stats.Failed++
			slog.Warn("wiki: prune links: page write failed", "page", rp, "error", err)
			continue
		}
		if repointed+removed > 0 {
			stats.PagesChanged++
			stats.Repointed += repointed
			stats.Removed += removed
		}
	}
	if stats.PagesChanged > 0 {
		_ = s.appendLog("prune-links", fmt.Sprintf("%d개 페이지 — 복구 %d건, 제거 %d건",
			stats.PagesChanged, stats.Repointed, stats.Removed))
	}
	return stats, nil
}

// PruneDeadWikiLinks rewrites body [[links]] whose target moved, using the same
// resolution ladder as the Related sweep, and reports what could not be
// resolved.
//
// Related entries were repaired from the start; inline links never were, so
// the 2026-07-19 rename of 61 project folders to deal codes left the bodies
// pointing at retired paths (about a third of the wiki's inline links are dead,
// most of them pre-rename project paths). Every one is a missing edge for
// graph_snapshot's inline-link pass and a dead end for a reader.
//
// Deliberately conservative: only a target that resolves to exactly one live
// page is rewritten, the link's alias/section suffix is preserved, and an
// unresolvable link is counted, never deleted — removing a link would destroy
// the author's statement that a relationship exists. Metadata-only in spirit,
// so it preserves `updated` like the Related sweep.
func (s *Store) PruneDeadWikiLinks() (PruneStats, []DeadWikiLink, error) {
	pages, err := s.ListPages("")
	if err != nil {
		return PruneStats{}, nil, fmt.Errorf("wiki: prune wikilinks: %w", err)
	}
	resolver := s.newLinkResolver(pages)
	if resolver.indexEntries == 0 && len(pages) > 0 {
		slog.Warn("wiki: wikilink prune skipped — index empty but pages exist", "pages", len(pages))
		return PruneStats{}, nil, nil
	}

	var stats PruneStats
	var deadLinks []DeadWikiLink
	for _, rp := range pages {
		rp = strings.ReplaceAll(rp, "\\", "/")
		if isFactProjectionReservedPath(rp) {
			continue
		}
		repointed, unresolved := 0, 0
		var deadHere []string
		if err := s.UpdatePage(rp, func(cur *Page) (*Page, error) {
			if cur == nil || !strings.Contains(cur.Body, "[[") {
				return nil, nil
			}
			body, rep, dead := rewriteWikiLinks(cur.Body, rp, resolver)
			repointed, unresolved, deadHere = rep, len(dead), dead
			if body == cur.Body {
				return nil, nil
			}
			// Rewriting a link to follow a rename says nothing new about the
			// subject, so the page keeps its date (UpdatePageMetaOnly refuses
			// body edits outright, which is right for frontmatter-only sweeps —
			// this one edits prose but carries the same "no new knowledge"
			// contract).
			updated := cur.Meta.Updated
			cur.Body = body
			cur.Meta.Updated = updated
			return cur, nil
		}); err != nil {
			stats.Failed++
			slog.Warn("wiki: wikilink prune write failed", "path", rp, "error", err)
			continue
		}
		stats.Repointed += repointed
		stats.Removed += unresolved // reported as "still dead", not deleted
		for _, target := range deadHere {
			deadLinks = append(deadLinks, DeadWikiLink{Page: rp, Target: target})
		}
		if repointed > 0 {
			stats.PagesChanged++
		}
	}
	return stats, deadLinks, nil
}

// rewriteWikiLinks rewrites [[target]] targets that resolve to a different live
// page, preserving |alias and #section suffixes. Returns the new body, how many
// links were repointed, and the path-shaped targets that stay unresolved.
func rewriteWikiLinks(body, self string, resolver *linkResolver) (string, int, []string) {
	repointed := 0
	var dead []string
	out := wikiLinkRe.ReplaceAllStringFunc(body, func(match string) string {
		inner := strings.TrimSuffix(strings.TrimPrefix(match, "[["), "]]")
		target, suffix := inner, ""
		if i := strings.IndexAny(inner, "|#"); i >= 0 {
			target, suffix = inner[:i], inner[i:]
		}
		trimmed := strings.TrimSpace(target)
		if trimmed == "" {
			return match
		}
		// Only PATH-shaped links are repaired. [[김부장]] is how a person writes
		// a reference, the graph resolvers already match it by title, and
		// expanding it to 인물/김부장.md would make the prose worse. A path, by
		// contrast, is a machine reference that a rename silently broke.
		if !strings.Contains(trimmed, "/") {
			return match
		}
		resolved := resolver.resolve(trimmed)
		if resolved == "" {
			dead = append(dead, trimmed)
			return match
		}
		// A link that already points at a live page — by path, title, or code —
		// is left byte-identical: prose forms like [[김부장]] are how people
		// write, and the graph resolvers already understand them.
		if resolved == trimmed || resolved == normalizePagePath(trimmed) || resolved == self {
			return match
		}
		if !resolver.exists[resolved] {
			return match
		}
		repointed++
		return "[[" + resolved + suffix + "]]"
	})
	return out, repointed, dead
}

// UnwrapWikiLinks turns the given still-dead [[targets]] in one page into plain
// prose — the alias when the author wrote one ([[path|label]] → label), else
// the target's readable basename. The author's statement survives as text; only
// the markup's promise of a destination is dropped.
//
// This is the terminal step of the dead-link ladder and deliberately NOT part
// of the sweep itself: a dead target can come back (a purged person re-seeded,
// a page recreated under the same path), and a still-wrapped link then heals on
// the next sweep for free. The review task therefore only condemns a link here
// after it has stayed dead across a grace window.
func (s *Store) UnwrapWikiLinks(relPath string, targets map[string]bool) (int, error) {
	if len(targets) == 0 {
		return 0, nil
	}
	unwrapped := 0
	err := s.UpdatePage(relPath, func(cur *Page) (*Page, error) {
		if cur == nil || !strings.Contains(cur.Body, "[[") {
			return nil, nil
		}
		body := wikiLinkRe.ReplaceAllStringFunc(cur.Body, func(match string) string {
			inner := strings.TrimSuffix(strings.TrimPrefix(match, "[["), "]]")
			target, alias := inner, ""
			if i := strings.IndexAny(inner, "|#"); i >= 0 {
				target = inner[:i]
				if inner[i] == '|' {
					alias = inner[i+1:]
				}
			}
			trimmed := strings.TrimSpace(target)
			if !targets[trimmed] {
				return match
			}
			unwrapped++
			if strings.TrimSpace(alias) != "" {
				return strings.TrimSpace(alias)
			}
			base := path.Base(strings.ReplaceAll(trimmed, "\\", "/"))
			return strings.TrimSuffix(base, ".md")
		})
		if body == cur.Body {
			return nil, nil
		}
		// Same "no new knowledge" contract as the repoint sweep: dropping rotten
		// markup says nothing new about the subject, so the page keeps its date.
		updated := cur.Meta.Updated
		cur.Body = body
		cur.Meta.Updated = updated
		return cur, nil
	})
	if err != nil {
		return 0, err
	}
	return unwrapped, nil
}
