// graph_query.go — in-process wiki graph traversal.
//
// extractWikiGraphContext (mailanalysis) used to shell out to the external
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
	"path/filepath"
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
	archived  bool // archived pages never surface as neighbors (rotated 로그-보관 etc.)
}

// GraphContext returns what the wiki knows is connected to `query` (a
// person/company/project name or page title): the matched page's summary plus
// its strongest one-hop neighbors, labeled by what each target IS
// (거래처/프로젝트/기자재/인물/… — see neighborLabel; unclassifiable pages keep
// the edge-mechanism label). Returns "" when no page matches. Pure in-process
// traversal — no LLM, no graphify subprocess.
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

// graphRankedPaths returns the wiki pages graph-connected to the entity named in
// query, ordered by graph proximity: the seed (the matched entity's page) first,
// then its strongest one-hop neighbors, capped at limit. Empty when the query
// names no known page. Unlike GraphContext (which renders a prompt string), this
// returns raw relPaths so Search can fold graph proximity into RRF fusion as a
// third ranking — bringing the graph signal (today only in chat recall anchors)
// into wiki.Store.Search / miniapp.memory.search.
func (s *Store) graphRankedPaths(ctx context.Context, query string, limit int) []string {
	if limit <= 0 {
		return nil
	}
	out := make([]string, 0, limit)
	seen := make(map[string]struct{})
	add := func(p string) {
		if p == "" {
			return
		}
		if _, ok := seen[p]; ok {
			return
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}

	// Graph traversal from the query's seed first — when findSeed resolves the
	// entity, its own page keeps graph rank 0 (so a correctly-seeded query's
	// ranking is unchanged).
	recs, seed, best, err := s.graphScoreMap(ctx, query, graphMentionsEnabled, "")
	if err == nil && seed >= 0 {
		s.applyEmbeddingRerank(ctx, recs, seed, best)
		add(recs[seed].relPath)
		for _, n := range rankNeighbors(recs, best, limit) {
			add(recs[n.idx].relPath)
		}
	}

	// Deterministic entity anchors as a RESCUE. MatchProjectsInText /
	// MatchCounterpartiesInText are rune-based and particle-robust — they resolve
	// "아르고에너지랑은…" and "아르고에너지 진행 상황" (common-word context) that
	// graphScoreMap's findSeed misses, and are what chat recall already uses to pin
	// a named entity. Appended (deduped) so they only ADD a missed entity page to
	// the graph ranking rather than override a correctly-resolved seed — bringing
	// that reliable anchor to wiki.Store.Search / miniapp.memory.search without
	// displacing the top result when the graph already found the entity.
	for _, ref := range s.MatchProjectsInText(query, 2) {
		add(ref.Path)
	}
	for _, ref := range s.MatchCounterpartiesInText(query, 2) {
		add(ref.Path)
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

// PageConnections returns a compact, one-line summary of a page's strongest
// graph neighbors labeled by their kind (e.g. "홍길동(인물) · XLPE 케이블(기자재) ·
// 영광 발주(프로젝트)" — mechanism labels only for unclassifiable pages),
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
		parts = append(parts, recs[n.idx].title+"("+neighborLabel(recs[n.idx], n.relation)+")")
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
// connected page's best one-hop neighbor (keyed by rec Index), the resolved
// recs, and the seed Index (seed<0 when nothing matches). includeMentions
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

	corpus, err := s.loadGraphCorpus(ctx)
	if err != nil {
		return nil, -1, nil, err
	}
	if len(corpus.recs) == 0 {
		return corpus.recs, -1, nil, nil
	}

	seed := corpus.seedIndex(q, seedOverride)
	if seed < 0 {
		return corpus.recs, -1, nil, nil
	}

	scorer := newGraphEdgeScorer(&corpus, seed)
	scorer.addRelatedEdges()
	scorer.addInlineLinkEdges()
	scorer.addProjectFamilyEdges()
	scorer.addSharedTagEdges()
	if includeMentions {
		scorer.addMentionEdges()
	}

	return corpus.recs, seed, scorer.best, nil
}

// graphCorpus is the live-page snapshot plus its deterministic lookup indexes.
// Building it is separate from edge scoring so cancellation/read failures never
// leave graphScoreMap with a partially scored graph.
type graphCorpus struct {
	recs   []graphRec
	byNorm map[string]int
	byID   map[string]int
	byCode map[string]int
	byPath map[string]int
}

func newGraphCorpus(capacity int) graphCorpus {
	return graphCorpus{
		recs:   make([]graphRec, 0, capacity),
		byNorm: make(map[string]int, capacity),
		byID:   make(map[string]int, capacity),
		byCode: make(map[string]int, capacity),
		byPath: make(map[string]int, capacity),
	}
}

// loadGraphCorpus snapshots readable pages in ListPages order. Individual page
// read failures remain best-effort skips, while context cancellation aborts the
// whole build exactly as the former inline loop did.
func (s *Store) loadGraphCorpus(ctx context.Context) (graphCorpus, error) {
	relPaths, err := s.ListPages("")
	if err != nil {
		return graphCorpus{}, err
	}
	corpus := newGraphCorpus(len(relPaths))
	for _, relPath := range relPaths {
		if ctx.Err() != nil {
			return graphCorpus{}, ctx.Err()
		}
		page, readErr := s.ReadPage(relPath)
		if readErr != nil || page == nil {
			continue
		}
		corpus.add(graphRecord(relPath, page))
	}
	return corpus, nil
}

func graphRecord(relPath string, page *Page) graphRec {
	title := page.Meta.Title
	if title == "" {
		title = strings.TrimSuffix(relPath, ".md")
	}
	return graphRec{
		relPath:   relPath,
		title:     title,
		normTitle: strings.ToLower(strings.TrimSpace(title)),
		id:        page.Meta.ID,
		code:      page.Meta.Code,
		summary:   page.Meta.Summary,
		category:  page.Meta.Category,
		due:       page.Meta.Due,
		tags:      normalizeGraphTags(page.Meta.Tags),
		related:   page.Meta.Related,
		links:     ExtractWikiLinks(page.Body),
		bodyLower: strings.ToLower(page.Body),
		archived:  page.Meta.Archived,
	}
}

func normalizeGraphTags(tags []string) []string {
	normalized := make([]string, 0, len(tags))
	for _, tag := range tags {
		if tag = strings.ToLower(strings.TrimSpace(tag)); tag != "" {
			normalized = append(normalized, tag)
		}
	}
	return normalized
}

func (c *graphCorpus) add(rec graphRec) {
	idx := len(c.recs)
	c.recs = append(c.recs, rec)
	c.byNorm[rec.normTitle] = idx
	if rec.id != "" {
		c.byID[rec.id] = idx
	}
	if rec.code != "" {
		c.byCode[rec.code] = idx
	}
	c.byPath[rec.relPath] = idx
	c.byPath[strings.TrimSuffix(rec.relPath, ".md")] = idx
}

// seedIndex keeps benchmark path seeding and production query seeding as two
// explicit modes. A non-empty override never falls back to fuzzy query lookup.
func (c *graphCorpus) seedIndex(query, override string) int {
	if override != "" {
		if idx, ok := c.byPath[override]; ok {
			return idx
		}
		return -1
	}
	return findSeed(c.recs, c.byNorm, c.byID, c.byCode, query)
}

func (c *graphCorpus) resolveReference(raw string) int {
	ref := strings.TrimSpace(raw)
	ref = strings.TrimPrefix(ref, "[[")
	ref = strings.TrimSuffix(ref, "]]")
	if idx, ok := c.byPath[ref]; ok {
		return idx
	}
	if idx, ok := c.byPath[strings.TrimSuffix(ref, ".md")]; ok {
		return idx
	}
	// Code first among the identifier maps: it is the frozen, move-stable
	// identity, so a code ref keeps resolving after the target page moves.
	if idx, ok := c.byCode[normalizeProjectCode(ref)]; ok {
		return idx
	}
	if idx, ok := c.byID[ref]; ok {
		return idx
	}
	if idx, ok := c.byNorm[strings.ToLower(strings.TrimSpace(ref))]; ok {
		return idx
	}
	return -1
}

// graphEdgeScorer applies the edge phases in graphScoreMap's declared order.
// bump replaces only a strictly weaker edge, so equal-score phases keep the
// earlier authored relation label.
type graphEdgeScorer struct {
	corpus *graphCorpus
	seed   int
	best   map[int]*graphNeighbor
}

func newGraphEdgeScorer(corpus *graphCorpus, seed int) *graphEdgeScorer {
	return &graphEdgeScorer{
		corpus: corpus,
		seed:   seed,
		best:   make(map[int]*graphNeighbor),
	}
}

func (g *graphEdgeScorer) bump(idx int, score float64, relation string) {
	if idx < 0 || idx == g.seed || g.corpus.recs[idx].archived {
		return
	}
	neighbor := g.best[idx]
	if neighbor == nil {
		g.best[idx] = &graphNeighbor{idx: idx, score: score, relation: relation}
		return
	}
	if score > neighbor.score {
		neighbor.score = score
		neighbor.relation = relation
	}
}

// addRelatedEdges scores explicit Related[] edges in both directions.
func (g *graphEdgeScorer) addRelatedEdges() {
	for _, ref := range g.corpus.recs[g.seed].related {
		g.bump(g.corpus.resolveReference(ref), 1.0, "관련")
	}
	for idx := range g.corpus.recs {
		if idx == g.seed {
			continue
		}
		for _, ref := range g.corpus.recs[idx].related {
			if g.corpus.resolveReference(ref) == g.seed {
				g.bump(idx, 1.0, "관련")
			}
		}
	}
}

// addInlineLinkEdges scores authored [[wiki-link]] edges after Related[], so
// equal scores retain the explicit frontmatter relation.
func (g *graphEdgeScorer) addInlineLinkEdges() {
	for _, ref := range g.corpus.recs[g.seed].links {
		g.bump(g.corpus.resolveReference(ref), 1.0, "링크")
	}
	for idx := range g.corpus.recs {
		if idx == g.seed {
			continue
		}
		for _, ref := range g.corpus.recs[idx].links {
			if g.corpus.resolveReference(ref) == g.seed {
				g.bump(idx, 1.0, "링크")
			}
		}
	}
}

// addProjectFamilyEdges connects pages whose canonical project folder already
// encodes ownership. Raw mail-analysis pages intentionally receive less weight.
func (g *graphEdgeScorer) addProjectFamilyEdges() {
	folder, ok := ProjectFolderOf(g.corpus.recs[g.seed].relPath)
	if !ok {
		return
	}
	for idx := range g.corpus.recs {
		if idx == g.seed {
			continue
		}
		candidateFolder, belongs := ProjectFolderOf(g.corpus.recs[idx].relPath)
		if !belongs || candidateFolder != folder {
			continue
		}
		score := 1.0
		if IsMailAnalysisPath(g.corpus.recs[idx].relPath) {
			score = 0.6
		}
		g.bump(idx, score, "같은 프로젝트")
	}
}

func graphTagFrequencies(recs []graphRec) map[string]int {
	frequencies := make(map[string]int)
	for idx := range recs {
		for _, tag := range recs[idx].tags {
			frequencies[tag]++
		}
	}
	return frequencies
}

// addSharedTagEdges excludes corpus-wide and singleton tags using the same
// 2..12 frequency window as graph_snapshot.go.
func (g *graphEdgeScorer) addSharedTagEdges() {
	frequencies := graphTagFrequencies(g.corpus.recs)
	seedTags := make(map[string]bool, len(g.corpus.recs[g.seed].tags))
	for _, tag := range g.corpus.recs[g.seed].tags {
		if frequency := frequencies[tag]; frequency >= 2 && frequency <= 12 {
			seedTags[tag] = true
		}
	}
	if len(seedTags) == 0 {
		return
	}
	for idx := range g.corpus.recs {
		if idx == g.seed {
			continue
		}
		for _, tag := range g.corpus.recs[idx].tags {
			if seedTags[tag] {
				g.bump(idx, 0.5, "태그:"+tag)
				break
			}
		}
	}
}

// addMentionEdges scores seed→candidate mentions at 0.7 and reverse mentions
// at 0.8. graphScoreMap calls it only when the benchmark-backed gate is on.
func (g *graphEdgeScorer) addMentionEdges() {
	seed := &g.corpus.recs[g.seed]
	for idx := range g.corpus.recs {
		if idx == g.seed {
			continue
		}
		candidate := &g.corpus.recs[idx]
		if len(candidate.normTitle) >= minMentionTitleLen &&
			strings.Contains(seed.bodyLower, candidate.normTitle) {
			g.bump(idx, 0.7, "언급")
		}
		if len(seed.normTitle) >= minMentionTitleLen &&
			strings.Contains(candidate.bodyLower, seed.normTitle) {
			g.bump(idx, 0.8, "언급")
		}
	}
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
// regression). Mirrors searchSemantic's safe access — refresh outside the Index
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

// neighborLabel labels a rendered neighbor with what the target page IS
// (거래처/프로젝트/기자재/메일/인물/규정/…) instead of how the edge was found
// (관련/링크/태그/언급/유사). The layout schema and category frontmatter already
// know every page's kind, so the meaning label is deterministic and free — no
// LLM classification, no authored relation types. Pages with no classifiable
// kind keep the mechanism label, which also preserves the provenance hint
// exactly where the graph is guessing.
func neighborLabel(rec graphRec, mechanism string) string {
	if l := semanticNeighborLabel(rec); l != "" {
		return l
	}
	return mechanism
}

// semanticNeighborLabel derives the page-kind label. Under 프로젝트/ the layout
// slot is authoritative (거래 원장 → 거래처, 기자재/ → 기자재, 메일분석 buckets →
// 메일, 로그.md → 로그, everything else → 프로젝트); other pages label as their
// normalized category, falling back to the top-level folder name. Returns ""
// when neither carries meaning (root files, 기타), leaving the mechanism label.
func semanticNeighborLabel(rec graphRec) string {
	p := filepath.ToSlash(strings.TrimSpace(rec.relPath))
	if seg := splitProjectPath(p); len(seg) > 0 {
		switch {
		case seg[0] == dealDir:
			return "거래처"
		case IsMailAnalysisPath(p):
			return "메일"
		case len(seg) >= 2 && seg[1] == equipmentDir:
			return "기자재"
		case len(seg) == 2 && seg[1] == LogPageFile:
			return "로그"
		default:
			return "프로젝트"
		}
	}
	if c := normalizeCategory(rec.category); c != "" && c != "기타" {
		// Path categories ("프로젝트/영산고") keep labels short: first segment only.
		if i := strings.IndexByte(c, '/'); i > 0 {
			c = c[:i]
		}
		return c
	}
	if cat, rest, ok := strings.Cut(p, "/"); ok && cat != "" && rest != "" && cat != "기타" {
		return cat
	}
	return ""
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
			sb.WriteString("\n- " + r.title + " (" + neighborLabel(r, n.relation) + ")")
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
