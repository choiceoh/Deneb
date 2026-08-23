// meta_cap.go — hard caps on related/tags so apply cannot grow a page into a
// BM25 magnet. Rank prefers 대표 → 인물 → 업무 → 사용자/시스템 → other → 기타 → 메일분석.
package wiki

import (
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	// MaxRelatedPerPage is the apply-time hard cap (wiki 규칙 1–3).
	MaxRelatedPerPage = 3
	// MaxTagsPerPage bounds tag lists. Existing tags keep priority; overflow
	// from a single update is dropped.
	MaxTagsPerPage = 8
)

// mergeUnique concatenates two string lists, first-seen order, no empties.
func mergeUnique(existing, added []string) []string {
	seen := make(map[string]struct{}, len(existing)+len(added))
	out := make([]string, 0, len(existing)+len(added))
	for _, t := range append(append([]string{}, existing...), added...) {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		if _, ok := seen[t]; ok {
			continue
		}
		seen[t] = struct{}{}
		out = append(out, t)
	}
	return out
}

func capStringList(in []string, n int) []string {
	if n <= 0 || len(in) <= n {
		return in
	}
	return append([]string{}, in[:n]...)
}

func sameStringSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func relatedRank(path string) int {
	p := filepath.ToSlash(strings.TrimSpace(path))
	p = strings.TrimPrefix(p, "[[")
	p = strings.TrimSuffix(p, "]]")
	if i := strings.IndexAny(p, "|#"); i >= 0 {
		p = p[:i]
	}
	p = strings.TrimSpace(p)
	switch {
	case strings.HasSuffix(p, "/"+RepPageFile):
		return 0
	case strings.HasPrefix(p, "인물/"):
		return 1
	case strings.HasPrefix(p, "업무/"):
		return 2
	case strings.HasPrefix(p, "사용자/") || strings.HasPrefix(p, "시스템/"):
		return 3
	case strings.Contains(p, "/"+mailAnalysisDir+"/") || strings.Contains(p, "/"+legacyMailAnalysisDir+"/"):
		return 9
	case strings.HasPrefix(p, "기타/"):
		return 5
	default:
		return 4
	}
}

// RankRelated keeps the highest-ranked related paths, stable on ties (input
// order). Empty / whitespace entries are dropped first.
func RankRelated(paths []string, n int) []string {
	clean := mergeUnique(nil, paths)
	if n <= 0 || len(clean) <= n {
		return clean
	}
	type item struct {
		path  string
		rank  int
		order int
	}
	items := make([]item, len(clean))
	for i, p := range clean {
		items[i] = item{path: p, rank: relatedRank(p), order: i}
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].rank != items[j].rank {
			return items[i].rank < items[j].rank
		}
		return items[i].order < items[j].order
	})
	out := make([]string, n)
	for i := 0; i < n; i++ {
		out[i] = items[i].path
	}
	return out
}

// CapPageRelations walks every page and trims Related/Tags that already
// overflow the apply caps. Idempotent. Returns how many pages changed.
func (s *Store) CapPageRelations() (int, error) {
	if s == nil {
		return 0, nil
	}
	pages, err := s.ListPages("")
	if err != nil {
		return 0, err
	}
	today := time.Now().Format("2006-01-02")
	changed := 0
	for _, rp := range pages {
		did := false
		if err := s.UpdatePage(rp, func(cur *Page) (*Page, error) {
			if cur == nil {
				return nil, nil
			}
			nextRel := RankRelated(cur.Meta.Related, MaxRelatedPerPage)
			nextTags := capStringList(mergeUnique(nil, cur.Meta.Tags), MaxTagsPerPage)
			if sameStringSlice(nextRel, cur.Meta.Related) && sameStringSlice(nextTags, cur.Meta.Tags) {
				return nil, nil
			}
			cur.Meta.Related = nextRel
			cur.Meta.Tags = nextTags
			cur.Meta.Updated = today
			did = true
			return cur, nil
		}); err != nil {
			return changed, err
		}
		if did {
			changed++
		}
	}
	return changed, nil
}
