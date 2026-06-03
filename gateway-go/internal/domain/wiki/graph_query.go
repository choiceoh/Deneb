// graph_query.go — in-process wiki graph traversal.
//
// extractWikiGraphContext (gmailpoll) used to shell out to the external
// `graphify query` CLI against a pre-built ~/.deneb/wiki-graph/graphify-out/
// graph.json. That left sender/topic context EMPTY whenever the CLI wasn't
// installed or the graph had never been snapshotted — the common case on a
// fresh deploy — so analysis lost the "who is this person to us" signal.
//
// GraphContext answers the same question entirely in-process: it builds an
// in-memory adjacency from live wiki state using the same edge model as
// graph_snapshot.go (explicit Related[], shared tags, body mentions) and
// returns a short human-readable summary of what's connected to a query name.
// No subprocess, no file dependency, always current.
package wiki

import (
	"context"
	"sort"
	"strings"
)

const (
	defaultGraphNeighbors = 8
	// maxGraphContextChars bounds the rendered summary so it stays a compact
	// context block in the analysis prompt.
	maxGraphContextChars = 2000
	// minMentionTitleLen avoids matching very short titles inside unrelated
	// prose ("AI", "PR"), mirroring graph_snapshot.go's len<3 guard.
	minMentionTitleLen = 3
)

// graphRec is the per-page record used to build the in-memory graph.
type graphRec struct {
	relPath   string
	title     string
	normTitle string
	id        string
	summary   string
	category  string
	due       string
	tags      []string // normalized
	related   []string // raw Related[] entries
	bodyLower string
}

// GraphContext returns what the wiki knows is connected to `query` (a
// person/company/project name or page title): the matched page's summary plus
// its strongest one-hop neighbors, labeled by relation. Returns "" when no page
// matches. Pure in-process traversal — no LLM, no graphify subprocess.
func (s *Store) GraphContext(ctx context.Context, query string, maxNeighbors int) (string, error) {
	if s == nil {
		return "", nil
	}
	q := strings.ToLower(strings.TrimSpace(stripAngleEmail(query)))
	if q == "" {
		return "", nil
	}
	if maxNeighbors <= 0 {
		maxNeighbors = defaultGraphNeighbors
	}

	relPaths, err := s.ListPages("")
	if err != nil {
		return "", err
	}

	recs := make([]graphRec, 0, len(relPaths))
	byNorm := make(map[string]int, len(relPaths)) // normTitle -> idx
	byID := make(map[string]int, len(relPaths))   // frontmatter id -> idx
	byPath := make(map[string]int, len(relPaths)) // relPath (with + without .md) -> idx
	for _, rp := range relPaths {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		page, perr := s.ReadPage(rp)
		if perr != nil || page == nil {
			continue
		}
		title := page.Meta.Title
		if title == "" {
			title = strings.TrimSuffix(rp, ".md")
		}
		tags := make([]string, 0, len(page.Meta.Tags))
		for _, t := range page.Meta.Tags {
			if t = strings.ToLower(strings.TrimSpace(t)); t != "" {
				tags = append(tags, t)
			}
		}
		idx := len(recs)
		recs = append(recs, graphRec{
			relPath:   rp,
			title:     title,
			normTitle: strings.ToLower(strings.TrimSpace(title)),
			id:        page.Meta.ID,
			summary:   page.Meta.Summary,
			category:  page.Meta.Category,
			due:       page.Meta.Due,
			tags:      tags,
			related:   page.Meta.Related,
			bodyLower: strings.ToLower(page.Body),
		})
		byNorm[recs[idx].normTitle] = idx
		if recs[idx].id != "" {
			byID[recs[idx].id] = idx
		}
		byPath[rp] = idx
		byPath[strings.TrimSuffix(rp, ".md")] = idx
	}
	if len(recs) == 0 {
		return "", nil
	}

	seed := findSeed(recs, byNorm, byID, q)
	if seed < 0 {
		return "", nil
	}

	// Tag frequency for the shared-tag pass (skip degenerate tags that span
	// too much of the corpus — same 2..12 window as graph_snapshot.go).
	tagFreq := make(map[string]int)
	for i := range recs {
		for _, t := range recs[i].tags {
			tagFreq[t]++
		}
	}

	best := make(map[int]*graphNeighbor)
	bump := func(idx int, score float64, relation string) {
		if idx < 0 || idx == seed {
			return
		}
		n := best[idx]
		if n == nil {
			best[idx] = &graphNeighbor{idx: idx, score: score, relation: relation}
			return
		}
		if score > n.score {
			n.score = score
			n.relation = relation
		}
	}

	resolve := func(rel string) int {
		rel = strings.TrimSpace(rel)
		rel = strings.TrimPrefix(rel, "[[")
		rel = strings.TrimSuffix(rel, "]]")
		if i, ok := byPath[rel]; ok {
			return i
		}
		if i, ok := byPath[strings.TrimSuffix(rel, ".md")]; ok {
			return i
		}
		if i, ok := byID[rel]; ok {
			return i
		}
		if i, ok := byNorm[strings.ToLower(strings.TrimSpace(rel))]; ok {
			return i
		}
		return -1
	}

	// Explicit Related[] edges, forward (seed -> X) and reverse (X -> seed).
	for _, rel := range recs[seed].related {
		bump(resolve(rel), 1.0, "관련")
	}
	for i := range recs {
		if i == seed {
			continue
		}
		for _, rel := range recs[i].related {
			if resolve(rel) == seed {
				bump(i, 1.0, "관련")
			}
		}
	}

	// Shared-tag edges.
	seedTags := make(map[string]bool, len(recs[seed].tags))
	for _, t := range recs[seed].tags {
		if f := tagFreq[t]; f >= 2 && f <= 12 {
			seedTags[t] = true
		}
	}
	if len(seedTags) > 0 {
		for i := range recs {
			if i == seed {
				continue
			}
			for _, t := range recs[i].tags {
				if seedTags[t] {
					bump(i, 0.5, "태그:"+t)
					break
				}
			}
		}
	}

	// Body-mention edges: seed mentions X (0.7), or X mentions seed (0.8 —
	// being talked about is a slightly stronger signal than talking).
	seedTitle := recs[seed].normTitle
	for i := range recs {
		if i == seed {
			continue
		}
		other := &recs[i]
		if len(other.normTitle) >= minMentionTitleLen && strings.Contains(recs[seed].bodyLower, other.normTitle) {
			bump(i, 0.7, "언급")
		}
		if len(seedTitle) >= minMentionTitleLen && strings.Contains(other.bodyLower, seedTitle) {
			bump(i, 0.8, "언급")
		}
	}

	neighbors := make([]graphNeighbor, 0, len(best))
	for _, n := range best {
		neighbors = append(neighbors, *n)
	}
	sort.Slice(neighbors, func(a, b int) bool {
		if neighbors[a].score != neighbors[b].score {
			return neighbors[a].score > neighbors[b].score
		}
		return recs[neighbors[a].idx].title < recs[neighbors[b].idx].title
	})
	if len(neighbors) > maxNeighbors {
		neighbors = neighbors[:maxNeighbors]
	}

	return renderGraphContext(recs[seed], recs, neighbors), nil
}

// findSeed picks the page that best matches the query: exact normalized title,
// then frontmatter id, then a title that contains the query or is contained by
// it (longest such title wins, so "탑솔라 거래" beats a bare substring hit).
func findSeed(recs []graphRec, byNorm, byID map[string]int, q string) int {
	if i, ok := byNorm[q]; ok {
		return i
	}
	if i, ok := byID[q]; ok {
		return i
	}
	bestIdx, bestLen := -1, 0
	for i := range recs {
		nt := recs[i].normTitle
		if nt == "" {
			continue
		}
		if strings.Contains(q, nt) || strings.Contains(nt, q) {
			if len(nt) > bestLen {
				bestIdx, bestLen = i, len(nt)
			}
		}
	}
	return bestIdx
}

// graphNeighbor is one ranked one-hop relation from the seed page.
type graphNeighbor struct {
	idx      int
	score    float64
	relation string
}

func renderGraphContext(seed graphRec, recs []graphRec, neighbors []graphNeighbor) string {
	var sb strings.Builder
	sb.WriteString(seed.title)
	if seed.category != "" {
		sb.WriteString(" [" + seed.category + "]")
	}
	if seed.summary != "" {
		sb.WriteString(" — " + seed.summary)
	}
	if seed.due != "" {
		sb.WriteString(" (기한 " + seed.due + ")")
	}
	if len(neighbors) > 0 {
		sb.WriteString("\n관련 항목:")
		for _, n := range neighbors {
			r := recs[n.idx]
			sb.WriteString("\n- " + r.title + " (" + n.relation + ")")
			if r.summary != "" {
				sb.WriteString(": " + r.summary)
			}
		}
	}
	out := strings.TrimSpace(sb.String())
	if len(out) > maxGraphContextChars {
		out = out[:maxGraphContextChars] + "\n...(생략)"
	}
	return out
}

// stripAngleEmail drops a trailing "<email@host>" so a raw From header
// ("홍길동 <a@b.com>") reduces to the display name the wiki indexes by.
func stripAngleEmail(s string) string {
	if i := strings.IndexByte(s, '<'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return strings.TrimSpace(s)
}
