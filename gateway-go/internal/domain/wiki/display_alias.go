// display_alias.go — Korean display aliases for code-keyed project folders.
//
// Folder-naming pivot (operator decision 2026-07-19): project folder names
// move from descriptive Korean strings to the frozen deal code ("프로젝트/
// nde-ztt-cbl-001/") so folder identity survives business evolution — the
// descriptive name rots ("케이블 조달"이 EPC로 커져도 경로 불변), and renames
// stop breaking every related: link and ledger ref. The HUMAN surface keeps
// Korean: wherever a path reaches the operator (recall notes, wiki tool
// output, digests), the code segment is annotated with the rep page's title.
//
// The mapping is rep-title-driven (no second source of truth): the alias of a
// folder IS its 대표.md title. Cached against the graph generation (bumped on
// every page write/delete) so chat-path lookups stay O(1).
package wiki

import (
	"strings"
	"sync"
)

// projectAliasCache memoizes folder→title lookups per store write generation.
type projectAliasCache struct {
	mu     sync.RWMutex
	gen    uint64
	filled bool
	byName map[string]string // project folder name → rep title
}

func (s *Store) aliasGeneration() uint64 {
	s.graphMu.RLock()
	defer s.graphMu.RUnlock()
	return s.graphGen
}

// ProjectDisplayAlias returns the Korean display title for a project folder
// name ("nde-ztt-cbl-001" → "비금도 154kV 해저케이블 (ZTT)"), or "" when the
// folder has no rep page, no title, or a title equal to the folder name (a
// descriptive legacy folder needs no annotation).
func (s *Store) ProjectDisplayAlias(folderName string) string {
	if s == nil || folderName == "" {
		return ""
	}
	gen := s.aliasGeneration()
	s.aliasCache.mu.RLock()
	if s.aliasCache.filled && s.aliasCache.gen == gen {
		alias := s.aliasCache.byName[folderName]
		s.aliasCache.mu.RUnlock()
		return alias
	}
	s.aliasCache.mu.RUnlock()

	s.aliasCache.mu.Lock()
	defer s.aliasCache.mu.Unlock()
	if !s.aliasCache.filled || s.aliasCache.gen != gen {
		byName := make(map[string]string)
		for _, ref := range s.KnownProjects() {
			folder, ok := ProjectNameOf(ref.Path)
			if !ok {
				continue
			}
			title := strings.TrimSpace(ref.Name)
			if title == "" || title == folder {
				continue
			}
			byName[folder] = title
		}
		s.aliasCache.byName = byName
		s.aliasCache.gen = gen
		s.aliasCache.filled = true
	}
	return s.aliasCache.byName[folderName]
}

// ProjectDisplayLabel returns the Korean alias of the project that OWNS a path
// ("프로젝트/nde-ztt-cbl-001/메일분석" → "비금도 154kV 해저케이블 (ZTT)"), or ""
// for non-project paths and folders that need no annotation. Accepts a page
// path or a bare folder path — the category browser lists folders, not pages.
//
// The LABEL form of the alias, for surfaces that render the name in a field of
// its own while the raw path stays the navigation key (miniapp.memory.categories
// → the clients' 위키 카테고리 browser). Prefer it over DisplayPath wherever the
// string could be fed back into a read: an annotated path is not openable
// (ReadPage misses), so DisplayPath's output must stay purely decorative.
func (s *Store) ProjectDisplayLabel(path string) string {
	if s == nil {
		return ""
	}
	name, ok := projectFolderForDisplay(path)
	if !ok {
		return ""
	}
	return s.ProjectDisplayAlias(name)
}

// projectFolderForDisplay resolves the owning project folder for display, from
// either a page path or a bare folder path. ProjectNameOf alone rejects the
// single-segment folder form ("프로젝트/pl2-kia-epc-001") because at the storage
// layer it can't be told apart from a not-yet-created page — but the category
// browser lists exactly that form, and for display the ambiguity is harmless:
// an unknown folder simply has no alias and renders as-is.
func projectFolderForDisplay(relPath string) (string, bool) {
	if name, ok := ProjectNameOf(relPath); ok {
		return name, true
	}
	seg := splitProjectPath(relPath)
	if len(seg) != 1 || strings.HasSuffix(seg[0], ".md") || isReservedProjectDir(seg[0]) {
		return "", false
	}
	return seg[0], true
}

// DisplayPath annotates a wiki path's project folder segment with its Korean
// alias for HUMAN-facing rendering: "프로젝트/nde-ztt-cbl-001/로그.md" →
// "프로젝트/nde-ztt-cbl-001(비금도 154kV 해저케이블)/로그.md". Non-project
// paths and folders without an alias pass through unchanged. Agent-internal
// consumers keep raw paths — only presentation layers call this.
func (s *Store) DisplayPath(path string) string {
	if s == nil {
		return path
	}
	name, ok := ProjectNameOf(path)
	if !ok {
		return path
	}
	alias := s.ProjectDisplayAlias(name)
	if alias == "" || strings.Contains(path, "("+alias+")") {
		return path
	}
	if idx := strings.Index(path, "/"+name); idx >= 0 {
		return path[:idx] + "/" + name + "(" + alias + ")" + path[idx+1+len(name):]
	}
	return path
}
