// project_refs.go — server-side resolution of each project's "owned" wiki pages.
//
// The project corner (안드로메다 ProjectHomePane) links related items (mail,
// calendar, todo, work-feed, notebook) to a project by intersecting each item's
// ref fields with the project's identity. A bare project identity is just its
// name + 대표페이지 path + frozen code, so an item that references a page that
// *belongs* to the project — a deal/sub page that shares the project's folder
// code, or any page the dreamer/agent explicitly linked to it — never matches.
//
// projectOwnedRefs resolves that one hop the client can't: it walks the live wiki
// graph (the gateway's, not the client's) and returns, per project, the paths of
// pages that resolve to it. Shipped in the digest as ProjectStatus.Refs, the
// client adds them as match keys, so an item referencing any owned page links to
// the project by exact ref instead of a read-time guess.
//
// Strong edges only — shared frozen code (folder inheritance) and explicit
// Related[]/[[wiki-link]] edges (author/dreamer-intended). Tags, body mentions,
// and embedding similarity (graph_query.go's fuzzy passes) are deliberately
// excluded: they rank neighbors for recall, but here a false edge would wrongly
// pull an unrelated item into a project, so precision wins over reach.
package wiki

import (
	"path/filepath"
	"strings"
)

// projectOwnedRefs returns each project's owned page paths, keyed by the project
// 대표페이지 path. One pass over the whole corpus: a page is owned by a project when
// it carries that project's exact frozen code (sub-pages inherit it by folder) or
// when its Related[]/[[links]] resolve to the project (by path or code). The
// project's own page is never listed as its own ref. Best-effort: unreadable
// pages are skipped; a nil/!projects input yields an empty map.
func (s *Store) projectOwnedRefs(projects []ProjectStatus) map[string][]string {
	out := make(map[string][]string)
	if s == nil || len(projects) == 0 {
		return out
	}

	// Project lookups: exact code → 대표페이지 path, and the set of project paths
	// (normalized with and without .md so a ref resolves either way).
	projectByCode := make(map[string]string, len(projects))
	projectByPath := make(map[string]string, len(projects)*2)
	for _, p := range projects {
		if c := normalizeProjectCode(p.Code); c != "" {
			projectByCode[c] = p.Path
		}
		projectByPath[p.Path] = p.Path
		projectByPath[strings.TrimSuffix(p.Path, ".md")] = p.Path
	}

	paths, err := s.ListPages("")
	if err != nil {
		return out
	}

	seen := make(map[string]map[string]bool) // project path → owned ref set (dedup)
	addOwned := func(projectPath, ref string) {
		ref = strings.TrimSpace(ref)
		if projectPath == "" || ref == "" || ref == projectPath {
			return
		}
		set := seen[projectPath]
		if set == nil {
			set = make(map[string]bool)
			seen[projectPath] = set
		}
		if set[ref] {
			return
		}
		set[ref] = true
		out[projectPath] = append(out[projectPath], ref)
	}

	for _, rp := range paths {
		rp = filepath.ToSlash(rp) // ListPages uses the OS separator; project paths are slash-keyed
		if _, isProject := projectByPath[rp]; isProject {
			continue // a 대표페이지 isn't an owned page of itself
		}
		page, perr := s.ReadPage(rp)
		if perr != nil || page == nil {
			continue
		}
		// (1) Shared frozen code: this page sits under a project's folder and
		// inherited its code, so it belongs to that project.
		if proj, ok := projectByCode[normalizeProjectCode(page.Meta.Code)]; ok {
			addOwned(proj, rp)
		}
		// (2) Explicit edges: this page points at a project via Related[] or an
		// inline [[wiki-link]]. Author/dreamer-intended, so it's a real ownership
		// signal — the item that references this page belongs to that project.
		for _, target := range page.Meta.Related {
			addOwned(s.resolveProjectTarget(target, projectByPath, projectByCode), rp)
		}
		for _, target := range ExtractWikiLinks(page.Body) {
			addOwned(s.resolveProjectTarget(target, projectByPath, projectByCode), rp)
		}
	}
	return out
}

// resolveProjectTarget maps a Related[]/[[link]] target to a project 대표페이지 path,
// or "" when the target isn't a project. Tries the page path (with/without .md)
// first, then the frozen code — mirroring graph_query.go's resolve order so a
// code ref keeps resolving after the target page moves.
func (s *Store) resolveProjectTarget(target string, projectByPath, projectByCode map[string]string) string {
	target = strings.TrimSpace(target)
	target = strings.TrimPrefix(target, "[[")
	target = strings.TrimSuffix(target, "]]")
	target = strings.TrimSpace(target)
	if target == "" {
		return ""
	}
	if p, ok := projectByPath[target]; ok {
		return p
	}
	if p, ok := projectByPath[strings.TrimSuffix(target, ".md")]; ok {
		return p
	}
	if p, ok := projectByCode[normalizeProjectCode(target)]; ok {
		return p
	}
	return ""
}
