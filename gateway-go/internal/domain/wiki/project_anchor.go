// project_anchor.go — resolve project mentions in free text to their 대표페이지.
//
// Two consumers:
//   - recall (chat pipeline): a turn that names a project gets that project's
//     대표페이지 anchored into the recall evidence, so "기아 화성 근황?" surfaces
//     the curated 현재 상태 even when BM25 ranks detail pages first;
//   - mail reclassification: an unlinked mail whose title names exactly one
//     project files into that project's 메일분석 slot.
package wiki

import (
	"sort"
	"strings"
	"unicode/utf8"
)

// minProjectKeyRunes guards containment matching against short generic names
// ("바로" would match everywhere); names shorter than this never match by text.
const minProjectKeyRunes = 3

// MatchProjectsInText returns ACTIVE projects whose name (display title or
// folder name) appears in text, most-specific (longest key) first, capped at
// limit. Matching is normalized (lowercase, letters+digits only) so "기아 화성"
// matches the 기아-화성 folder. Deterministic — no LLM.
func (s *Store) MatchProjectsInText(text string, limit int) []ProjectRef {
	if s == nil || limit <= 0 {
		return nil
	}
	hay := normalizeTitleKey(text)
	if hay == "" {
		return nil
	}
	type scored struct {
		ref ProjectRef
		key string
	}
	var hits []scored
	for _, ref := range s.knownProjects() {
		key := bestProjectKeyIn(hay, ref)
		if key == "" {
			continue
		}
		hits = append(hits, scored{ref: ref, key: key})
	}
	sort.SliceStable(hits, func(i, j int) bool { return len(hits[i].key) > len(hits[j].key) })
	out := make([]ProjectRef, 0, limit)
	for _, h := range hits {
		out = append(out, h.ref)
		if len(out) >= limit {
			break
		}
	}
	return out
}

// bestProjectKeyIn returns the longest normalized identity key of ref (display
// name or folder name) contained in hay, or "" when none matches.
func bestProjectKeyIn(hay string, ref ProjectRef) string {
	best := ""
	name, _ := ProjectNameOf(ref.Path)
	for _, cand := range []string{ref.Name, name} {
		key := normalizeTitleKey(cand)
		if utf8.RuneCountInString(key) < minProjectKeyRunes {
			continue
		}
		if strings.Contains(hay, key) && len(key) > len(best) {
			best = key
		}
	}
	return best
}
