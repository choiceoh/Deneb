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
	// graphMentionsEnabled gates the body-mention pass in production traversal.
	// graph_bench_test.go scores the graph against operator-graded pairs both
	// ways; set from that evidence, not intuition.
	graphMentionsEnabled = true
)

// graphRec is the per-page record used to build the in-memory graph.
type graphRec struct {
	relPath   string
	title     string
	normTitle string
	id        string
	code      string // frozen composite project code (move-stable identity)
	summary   string
	category  string
	due       string
	tags      []string // normalized
	related   []string // raw Related[] entries
	links     []string // inline [[wiki-link]] targets from the body
	bodyLower string
}

// GraphContext returns what the wiki knows is connected to `query` (a
// person/company/project name or page title): the matched page's summary plus
// its strongest one-hop neighbors, labeled by relation. Returns "" when no page
// matches. Pure in-process traversal — no LLM, no graphify subprocess.
func (s *Store) GraphContext(ctx context.Context, query string, maxNeighbors int) (string, error) {
	if maxNeighbors <= 0 {
		maxNeighbors = defaultGraphNeighbors
	}
	recs, seed, best, err := s.graphScoreMap(ctx, query, graphMentionsEnabled, "")
	if err != nil || seed < 0 {
		return "", err
	}
	// Fold in dense semantic similarity. The benchmark (graph_bench_test.go)
	// showed token structure + cosine ranks a seed's neighbors markedly better
	// than either alone; best-effort, so no embedder means the token-only ranking.
	s.applyEmbeddingRerank(ctx, recs, seed, best)

	neighbors := rankNeighbors(recs, best, maxNeighbors)
	return renderGraphContext(recs[seed], recs, neighbors), nil
}

// PageConnections returns a compact, one-line summary of a page's strongest
// graph neighbors (e.g. "홍길동(링크) · 비금도 케이블(유사) · 영광 발주(태그:케이블)"),
// seeded by the page's exact relPath rather than a free-text query. It powers
// the "연결된 항목" footer appended when a page is read on-demand, so the agent
// sees the connection web at the point of reading and can choose to follow it —
// graph self-exploration without forcing neighbors into every-turn recall.
// Returns "" when the page has no neighbors or cannot be resolved.
func (s *Store) PageConnections(ctx context.Context, relPath string, maxNeighbors int) (string, error) {
	if maxNeighbors <= 0 {
		maxNeighbors = defaultGraphNeighbors
	}
	if !strings.HasSuffix(relPath, ".md") {
		relPath += ".md"
	}
	recs, seed, best, err := s.graphScoreMap(ctx, "", graphMentionsEnabled, relPath)
	if err != nil || seed < 0 {
		return "", err
	}
	s.applyEmbeddingRerank(ctx, recs, seed, best)

	neighbors := rankNeighbors(recs, best, maxNeighbors)
	if len(neighbors) == 0 {
		return "", nil
	}
	parts := make([]string, 0, len(neighbors))
	for _, n := range neighbors {
		parts = append(parts, recs[n.idx].title+"("+n.relation+")")
	}
	return strings.Join(parts, " · "), nil
}

// rankNeighbors flattens the per-candidate best-edge map into a list ordered by
// score (title as the stable tiebreak) and truncated to maxNeighbors.
func rankNeighbors(recs []graphRec, best map[int]*graphNeighbor, maxNeighbors int) []graphNeighbor {
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
	return neighbors
}

// graphScoreMap builds the in-memory wiki graph for `query` and returns every
// connected page's best one-hop neighbor (keyed by rec index), the resolved
// recs, and the seed index (seed<0 when nothing matches). includeMentions
// toggles the body-mention pass so its contribution can be measured and tuned
// (graph_bench_test.go) independently of explicit Related[] edges and tags.
// seedOverride, when non-empty, pins the seed to that exact page path instead of
// resolving it from the query — the benchmark scores from known seeds.
func (s *Store) graphScoreMap(ctx context.Context, query string, includeMentions bool, seedOverride string) ([]graphRec, int, map[int]*graphNeighbor, error) {
	if s == nil {
		return nil, -1, nil, nil
	}
	q := strings.ToLower(strings.TrimSpace(stripAngleEmail(query)))
	if q == "" && seedOverride == "" {
		return nil, -1, nil, nil
	}

	relPaths, err := s.ListPages("")
	if err != nil {
		return nil, -1, nil, err
	}

	recs := make([]graphRec, 0, len(relPaths))
	byNorm := make(map[string]int, len(relPaths)) // normTitle -> idx
	byID := make(map[string]int, len(relPaths))   // frontmatter id -> idx
	byCode := make(map[string]int, len(relPaths)) // frozen project code -> idx
	byPath := make(map[string]int, len(relPaths)) // relPath (with + without .md) -> idx
	for _, rp := range relPaths {
		if ctx.Err() != nil {
			return nil, -1, nil, ctx.Err()
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
			code:      page.Meta.Code,
			summary:   page.Meta.Summary,
			category:  page.Meta.Category,
			due:       page.Meta.Due,
			tags:      tags,
			related:   page.Meta.Related,
			links:     ExtractWikiLinks(page.Body),
			bodyLower: strings.ToLower(page.Body),
		})
		byNorm[recs[idx].normTitle] = idx
		if recs[idx].id != "" {
			byID[recs[idx].id] = idx
		}
		if recs[idx].code != "" {
			byCode[recs[idx].code] = idx
		}
		byPath[rp] = idx
		byPath[strings.TrimSuffix(rp, ".md")] = idx
	}
	if len(recs) == 0 {
		return recs, -1, nil, nil
	}

	// The benchmark scores from a known seed page (by path); production resolves
	// the seed from the free-text query.
	seed := -1
	if seedOverride != "" {
		if i, ok := byPath[seedOverride]; ok {
			seed = i
		}
	} else {
		seed = findSeed(recs, byNorm, byID, byCode, q)
	}
	if seed < 0 {
		return recs, -1, nil, nil
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
		// Code first among the identifier maps: it's the frozen, move-stable
		// identity, so a code ref keeps resolving after the target page moves.
		if i, ok := byCode[normalizeProjectCode(rel)]; ok {
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

	// Inline [[wiki-link]] edges in bodies — author-intended links, as
	// trustworthy as Related[]. The dreamer emits these into pages' "관련 문서"
	// sections and an agent can link in prose via wiki write; parsing them here
	// turns inline links into real graph edges instead of decoration. Forward
	// (seed body links X) and reverse (X body links seed).
	for _, l := range recs[seed].links {
		bump(resolve(l), 1.0, "링크")
	}
	for i := range recs {
		if i == seed {
			continue
		}
		for _, l := range recs[i].links {
			if resolve(l) == seed {
				bump(i, 1.0, "링크")
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
	// being talked about is a slightly stronger signal than talking). Gated
	// because the graded benchmark showed these mostly add false neighbors (a
	// page sharing a site name in prose is not a real relation).
	if includeMentions {
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
	}

	return recs, seed, best, nil
}

// graphEmbedWeight scales the dense-similarity term folded into each candidate's
// token/edge score. Tuned on the graded benchmark, where token structure + this
// term reached per-seed AUC ~0.87 versus ~0.80 for tokens alone and ~0.82 for
// cosine alone — and the result was insensitive to the exact weight, since
// explicit edges (score >= 1.0) still lead and cosine only orders the rest and
// breaks ties.
const graphEmbedWeight = 0.5

// applyEmbeddingRerank folds BGE-M3 cosine similarity into the token/edge scores
// in best, in place: every candidate gains graphEmbedWeight*cosine(seed, cand),
// and a strongly-similar page with no explicit edge enters as a "유사" neighbor —
// the case lexical signals miss (영광 and 비금도 are both cable projects but link
// to each other only in prose). Best-effort: a missing, unhealthy, or
// un-refreshable embedder leaves the token-only ranking untouched (no
// regression). Mirrors searchSemantic's safe access — refresh outside the index
// lock, snapshot vectors under it.
func (s *Store) applyEmbeddingRerank(ctx context.Context, recs []graphRec, seed int, best map[int]*graphNeighbor) {
	if s.sem == nil || s.sem.embedder == nil || !s.sem.embedder.IsHealthy() {
		return
	}
	s.sem.refreshAsync(s) // background re-embed; rerank on current vectors
	s.sem.mu.Lock()
	seedCV, ok := s.sem.vecs[recs[seed].relPath]
	if !ok {
		s.sem.mu.Unlock()
		return
	}
	seedVec := seedCV.vec
	vecByIdx := make(map[int][]float32, len(recs))
	for i := range recs {
		if cv, ok := s.sem.vecs[recs[i].relPath]; ok {
			vecByIdx[i] = cv.vec
		}
	}
	s.sem.mu.Unlock()

	for i := range recs {
		if i == seed {
			continue
		}
		cv, ok := vecByIdx[i]
		if !ok {
			continue
		}
		cs := graphEmbedWeight * cosine(seedVec, cv)
		if n := best[i]; n != nil {
			n.score += cs
		} else {
			best[i] = &graphNeighbor{idx: i, score: cs, relation: "유사"}
		}
	}
}

// findSeed picks the page that best matches the query: exact normalized title,
// then frontmatter id, then a title that contains the query or is contained by
// it (longest such title wins, so "탑솔라 거래" beats a bare substring hit).
func findSeed(recs []graphRec, byNorm, byID, byCode map[string]int, q string) int {
	if i, ok := byCode[normalizeProjectCode(q)]; ok {
		return i
	}
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
