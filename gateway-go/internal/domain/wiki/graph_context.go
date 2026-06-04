// graph_context.go — query-time traversal of the wiki knowledge graph.
//
// The wiki already IS a graph: pages are nodes and Frontmatter.Related[] are
// explicit edges, kept bidirectional by maintainBacklinks (store.go). Until now
// that graph was only ever *written* (mail analysis records sender→project as
// Related[]) and snapshotted to a static file (graph_snapshot.go); nothing read
// it back at analysis time — chat and mail analysis both used flat FTS search.
//
// GraphContext closes that gap. The hard part isn't recall (shared tags connect
// almost anything) but precision: a single broad shared tag is a spurious
// "similar" link, not a real relationship. So instead of including every page
// reachable by any signal, each candidate accumulates a SCORE from weighted
// signals — explicit Related[] links (and their backlinks) plus rarity-weighted
// shared tags — and only candidates clearing a threshold survive. A lone common
// tag scores below the bar; a rare tag, an explicit link, or several
// corroborating weak signals clear it. Recall without false positives.
//
// Edges are ground truth, so they must actually resolve: Related[] stores bare
// ids while the index is keyed by full path, so canonSlug bridges the two — that
// resolution, not body-text guessing, is what surfaces real relationships.
package wiki

import (
	"context"
	"sort"
	"strings"
	"unicode"
)

const (
	defaultGraphLimit = 20

	// Signal weights. Explicit links are ground truth; a verbatim body mention
	// is strong; a shared tag is worth 1/(n-1) where n is how many pages carry
	// it — a tag on 2 pages scores 1.0, on 6 pages 0.2, on 12 pages ~0.09.
	graphWeightExplicit = 1.0

	// graphTagMaxSpread drops degenerate tags spanning too much of the corpus
	// before they even get a (tiny) weight. graphScoreThreshold is the survival
	// bar: a single tag on >3 pages can't clear it alone, so it takes a rare
	// tag, a mention, an explicit link, or corroboration.
	graphTagMaxSpread = 12
	// Survival bar, calibrated on the graded benchmark: judged-strong pairs
	// (same project / explicit cross-ref) score ~1.0+, judged-weak ones
	// (shared vendor/site only) ~0.5, so 0.9 keeps the real relationships and
	// drops the superficially-similar band.
	graphScoreThreshold = 0.9
)

// GraphNeighbor is a page connected to the seed in the wiki graph.
type GraphNeighbor struct {
	Path       string  `json:"path"`
	Title      string  `json:"title"`
	Category   string  `json:"category"`
	Type       string  `json:"type"`
	Importance float64 `json:"importance"`
	Summary    string  `json:"summary"`
	// Score is the summed signal strength; higher = more strongly connected.
	Score float64 `json:"score"`
	// Edge is the single strongest signal category (related|backlink|mention|tag),
	// used for grouping. Reasons lists every contributing signal for display,
	// e.g. ["본문언급", "#비금도"].
	Edge    string   `json:"edge"`
	Reasons []string `json:"reasons"`
}

// GraphContextResult is the resolved seed plus its scored neighborhood.
type GraphContextResult struct {
	SeedPath  string
	SeedTitle string
	Found     bool
	Neighbors []GraphNeighbor
}

// GraphContext resolves a seed page from a free-form query and returns its
// scored one-hop neighborhood. Read-only — never mutates the store.
//
// The in-memory index is snapshotted under the read lock; tag inference and
// page-body reads (for mentions) run lock-free off that copy, so a graph query
// never blocks writers while doing disk IO.
func (s *Store) GraphContext(ctx context.Context, query string, limit int) (GraphContextResult, error) {
	if limit <= 0 {
		limit = defaultGraphLimit
	}
	seedPath := s.resolveSeedPath(ctx, query)
	if seedPath == "" {
		return GraphContextResult{Found: false}, nil
	}

	s.mu.RLock()
	if s.index == nil {
		s.mu.RUnlock()
		return GraphContextResult{Found: false}, nil
	}
	seed, ok := s.index.Entries[seedPath]
	if !ok {
		s.mu.RUnlock()
		return GraphContextResult{Found: false}, nil
	}
	entries := make(map[string]IndexEntry, len(s.index.Entries))
	for p, e := range s.index.Entries {
		entries[p] = e
	}
	s.mu.RUnlock()

	scores := s.graphScore(seedPath, seed, entries)

	neighbors := make([]GraphNeighbor, 0, len(scores))
	for path, a := range scores {
		if a.score < graphScoreThreshold {
			continue // lone weak signal — a spurious "similar", drop it
		}
		e := entries[path]
		neighbors = append(neighbors, GraphNeighbor{
			Path:       path,
			Title:      e.Title,
			Category:   e.Category,
			Type:       e.Type,
			Importance: e.Importance,
			Summary:    e.Summary,
			Score:      a.score,
			Edge:       a.primary,
			Reasons:    a.reasons,
		})
	}
	sort.SliceStable(neighbors, func(i, j int) bool {
		if neighbors[i].Score != neighbors[j].Score {
			return neighbors[i].Score > neighbors[j].Score
		}
		return neighbors[i].Importance > neighbors[j].Importance
	})
	if len(neighbors) > limit {
		neighbors = neighbors[:limit]
	}

	return GraphContextResult{
		SeedPath:  seedPath,
		SeedTitle: seed.Title,
		Found:     true,
		Neighbors: neighbors,
	}, nil
}

// graphAcc accumulates the weighted signals connecting one candidate page to
// the seed.
type graphAcc struct {
	score   float64
	reasons []string
	primary string // strongest single signal category
}

// graphScore computes the raw connection score of every candidate page to the
// seed — explicit Related[] links (read from frontmatter, both directions) plus
// rarity-weighted shared tags — with no threshold or cap applied. GraphContext
// filters/sorts/caps the result; the benchmark consumes the raw scores to
// measure ranking quality.
func (s *Store) graphScore(seedPath string, seed IndexEntry, entries map[string]IndexEntry) map[string]*graphAcc {
	scores := map[string]*graphAcc{}
	bump := func(path string, w float64, reason, cat string) {
		path = normalizePagePath(path)
		if path == "" || path == seedPath {
			return
		}
		if _, exists := entries[path]; !exists {
			return
		}
		a := scores[path]
		if a == nil {
			a = &graphAcc{}
			scores[path] = a
		}
		a.score += w
		a.reasons = append(a.reasons, reason)
		if catRank(cat) < catRank(a.primary) {
			a.primary = cat
		}
	}

	// Resolve a Related[] reference to an index key. Edges are stored as bare
	// ids (a page's `id`, e.g. "석문호-케이블-발주"), but the index is keyed by
	// full path ("프로젝트/석문호-케이블-발주.md"), and some stored ids even drop
	// punctuation the filename keeps ("...-ztt" vs "...-(ztt)"). Matching on a
	// canonical slug (letters and digits only) resolves all three; without it the
	// explicit graph — the entire point of this traversal — silently links to
	// nothing, which is exactly the gap that made fragile token cross-refs look
	// necessary.
	byCanon := make(map[string]string, len(entries))
	for path := range entries {
		byCanon[canonSlug(path)] = path
	}
	resolveRef := func(ref string) string {
		p := normalizePagePath(ref)
		if _, ok := entries[p]; ok {
			return p
		}
		return byCanon[canonSlug(ref)]
	}
	// The in-memory index is parsed from index.md, which carries title/tags/
	// summary but NOT Related[] edges — those live only in each page's
	// frontmatter. So read edges straight from disk: the seed's own Related[]
	// gives forward links, and a scan of every page's Related[] finds backlinks
	// pointing at the seed (frontmatter stores each edge in one direction only).
	relatedOf := func(path string) []string {
		p, err := s.ReadPage(path)
		if err != nil {
			return nil
		}
		return p.Meta.Related
	}

	// Explicit Related[] (both directions) — ground truth.
	for _, rel := range relatedOf(seedPath) {
		if p := resolveRef(rel); p != "" {
			bump(p, graphWeightExplicit, "직접연결", "related")
		}
	}
	for path := range entries {
		if path == seedPath {
			continue
		}
		for _, rel := range relatedOf(path) {
			if resolveRef(rel) == seedPath {
				bump(path, graphWeightExplicit, "역참조", "backlink")
				break
			}
		}
	}
	// Shared tags, rarity-weighted.
	tagPages := map[string][]string{}
	for path, e := range entries {
		for _, t := range e.Tags {
			if t = strings.ToLower(strings.TrimSpace(t)); t != "" {
				tagPages[t] = append(tagPages[t], path)
			}
		}
	}
	for _, t := range seed.Tags {
		t = strings.ToLower(strings.TrimSpace(t))
		n := len(tagPages[t])
		if n < 2 || n > graphTagMaxSpread {
			continue
		}
		w := 1.0 / float64(n-1)
		for _, path := range tagPages[t] {
			bump(path, w, "#"+t, "tag")
		}
	}
	return scores
}

// canonSlug reduces a page reference (full path, bare id, or a punctuation-
// variant id) to lowercase letters and digits only. Related[] edges are stored
// as ids that may lack the directory prefix, the ".md" suffix, or brackets the
// filename keeps, so canonical-slug equality is what lets an edge find its index
// key.
func canonSlug(ref string) string {
	ref = strings.ToLower(strings.TrimSpace(ref))
	ref = strings.TrimSuffix(ref, ".md")
	if i := strings.LastIndex(ref, "/"); i >= 0 {
		ref = ref[i+1:]
	}
	var b strings.Builder
	for _, r := range ref {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// catRank orders signal categories strongest-first so a neighbor's primary
// label reflects its best evidence.
func catRank(cat string) int {
	switch cat {
	case "related":
		return 0
	case "backlink":
		return 1
	case "mention":
		return 2
	case "tag":
		return 3
	default:
		return 99
	}
}

// resolveSeedPath maps a free-form query to a wiki page path. It never holds
// s.mu while calling Search (which takes the independent FTS lock), so the two
// read paths can't deadlock.
func (s *Store) resolveSeedPath(ctx context.Context, query string) string {
	q := strings.TrimSpace(query)
	if q == "" {
		return ""
	}
	norm := normalizePagePath(q)

	s.mu.RLock()
	if s.index != nil {
		if _, ok := s.index.Entries[norm]; ok {
			s.mu.RUnlock()
			return norm
		}
		ql := strings.ToLower(q)
		bestPath, bestImp := "", -1.0
		for path, e := range s.index.Entries {
			if strings.Contains(strings.ToLower(e.Title), ql) || strings.Contains(strings.ToLower(path), ql) {
				if e.Importance > bestImp {
					bestImp, bestPath = e.Importance, path
				}
			}
		}
		if bestPath != "" {
			s.mu.RUnlock()
			return bestPath
		}
	}
	s.mu.RUnlock()

	if results, err := s.Search(ctx, q, 1); err == nil && len(results) > 0 {
		return normalizePagePath(results[0].Path)
	}
	return ""
}
