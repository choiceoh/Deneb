// search.go — In-memory full-text search for wiki pages.
// Replaces SQLite FTS5 with a pure Go textsearch index.
package wiki

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/pkg/textsearch"
)

// SearchResult is a single search hit.
type SearchResult struct {
	Path    string             `json:"path"`              // relative path within wiki dir
	Line    int                `json:"line,omitempty"`    // absolute 1-based start line
	EndLine int                `json:"endLine,omitempty"` // absolute 1-based inclusive end line
	Content string             `json:"content"`           // matching snippet
	Context []string           `json:"context,omitempty"` // hierarchical path/business context
	Score   float64            `json:"score"`             // relevance score (0-1)
	Explain *SearchExplanation `json:"explain,omitempty"` // populated only for explicit diagnostic queries
}

// SearchMode selects one retrieval stage for evaluation. The zero/auto mode is
// production behavior, including environment rollback knobs.
type SearchMode string

const (
	SearchModeAuto     SearchMode = "auto"
	SearchModeBM25     SearchMode = "bm25"
	SearchModeSemantic SearchMode = "semantic"
	SearchModeHybrid   SearchMode = "hybrid"
	SearchModeFull     SearchMode = "full"
)

// QueryOptions configures one search without mutating process-wide environment.
// Intent never admits a new document: it only reranks already-admitted
// candidates, and normally runs only when the base ranking is ambiguous.
type QueryOptions struct {
	Mode         SearchMode
	Explain      bool
	Intent       string
	ForceIntent  bool
	ForceRerank  bool
	SkipRerank   bool // when true, skip cross-encoder model rerank (agent lean path)
	skipMetadata bool
	skipValidity bool
}

type SearchSignalExplanation struct {
	Rank         int     `json:"rank"`
	BackendScore float64 `json:"backendScore,omitempty"`
	Contribution float64 `json:"contribution,omitempty"`
}

type SearchExplanation struct {
	Fusion         string                   `json:"fusion"`
	BM25           *SearchSignalExplanation `json:"bm25,omitempty"`
	Semantic       *SearchSignalExplanation `json:"semantic,omitempty"`
	Graph          *SearchSignalExplanation `json:"graph,omitempty"`
	Intent         *SearchSignalExplanation `json:"intent,omitempty"`
	Rerank         *SearchSignalExplanation `json:"rerank,omitempty"`
	BaseScore      float64                  `json:"baseScore"`
	IntentBonus    float64                  `json:"intentBonus,omitempty"`
	RerankWeight   float64                  `json:"rerankWeight,omitempty"`
	ValidityFactor float64                  `json:"validityFactor"`
	FinalScore     float64                  `json:"finalScore"`
}

type SearchDropSummary struct {
	Reason string `json:"reason"`
	Count  int    `json:"count"`
}

type SearchDiagnostics struct {
	Mode              SearchMode              `json:"mode"`
	Fusion            string                  `json:"fusion"`
	SemanticAvailable bool                    `json:"semanticAvailable"`
	GraphCandidates   int                     `json:"graphCandidates"`
	CommonOnlyQuery   bool                    `json:"commonOnlyQuery"`
	IntentApplied     bool                    `json:"intentApplied"`
	CandidateCount    int                     `json:"candidateCount"`
	ReturnedCount     int                     `json:"returnedCount"`
	Scopes            []string                `json:"scopes,omitempty"`
	Clauses           []QueryClauseDiagnostic `json:"clauses,omitempty"`
	Rerank            RerankDiagnostics       `json:"rerank"`
	Dropped           []SearchDropSummary     `json:"dropped,omitempty"`
}

type SearchReport struct {
	Results     []SearchResult    `json:"results"`
	Diagnostics SearchDiagnostics `json:"diagnostics"`
}

// searchDB manages the in-memory FTS index for wiki pages.
type searchDB struct {
	idx        *textsearch.Index
	mu         sync.RWMutex
	now        func() time.Time
	fieldBoost float64
	// validity holds the per-page staleness factor (see validityFactor),
	// computed when the page is (re)indexed. Search multiplies scores by it
	// so archived/superseded/aging facts stop outranking current ones.
	validity map[string]float64
}

func newSearchDB(now func() time.Time, fieldBoost float64) *searchDB {
	if now == nil {
		now = time.Now
	}
	if fieldBoost <= 0 {
		fieldBoost = wikiFieldBoostValue()
	}
	return &searchDB{idx: textsearch.New(), validity: make(map[string]float64), now: now, fieldBoost: fieldBoost}
}

// indexPage upserts a page into the search index.
func (s *searchDB) indexPage(relPath string, page *Page) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.idx.UpsertFields(relPath, searchablePageFieldsWithBoost(page, s.fieldBoost)...)
	if page != nil {
		s.validity[relPath] = validityFactor(page.Meta, s.now())
	}
}

// removePage removes a page from the search index.
func (s *searchDB) removePage(relPath string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.idx.Remove(relPath)
	delete(s.validity, relPath)
}

// validityFactor scores how current a page's facts are (0–1]. Archived and
// superseded pages keep working for direct reads but should not outrank
// living pages in recall; old "updated" stamps decay gently — operational
// facts (ports, prices, configs) rot, and recall presenting a year-old fact
// as current is exactly the failure this guards against.
func validityFactor(meta Frontmatter, now time.Time) float64 {
	f := 1.0
	if meta.Archived {
		f *= 0.3
	}
	if meta.SupersededBy != "" {
		f *= 0.5
	}
	if meta.Updated != "" {
		if t, err := time.Parse("2006-01-02", meta.Updated); err == nil {
			switch age := now.Sub(t); {
			case age > 365*24*time.Hour:
				f *= 0.7
			case age > 180*24*time.Hour:
				f *= 0.85
			}
		}
	}
	return f
}

// applyValidity multiplies result scores by each page's validity factor and
// re-sorts. Pages never indexed (factor missing) pass through unchanged.
func (s *searchDB) applyValidity(results []SearchResult) []SearchResult {
	if len(results) == 0 {
		return results
	}
	s.mu.RLock()
	for i := range results {
		if f, ok := s.validity[results[i].Path]; ok && f < 1.0 {
			results[i].Score *= f
		}
	}
	s.mu.RUnlock()
	sort.SliceStable(results, func(a, b int) bool {
		if results[a].Score != results[b].Score {
			return results[a].Score > results[b].Score
		}
		return results[a].Path < results[b].Path
	})
	return results
}

func (s *searchDB) validityFor(path string) float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if factor, ok := s.validity[path]; ok {
		return factor
	}
	return 1
}

// search runs a full-text query and returns scored results.
func (s *searchDB) search(_ context.Context, query string, limit int) ([]SearchResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	hits := s.idx.Search(query, limit)
	results := make([]SearchResult, len(hits))
	for i, h := range hits {
		results[i] = SearchResult{
			Path:    h.ID,
			Content: h.Snippet,
			Score:   scoreToNormalized(h.Score),
		}
	}
	return results, nil
}

// queryMaxRarity returns the highest corpus-rarity (0–1) among the query's
// tokens that exist in the index (see textsearch.QueryMaxRarity). Used to gate
// the lexical (BM25) recall leak: a query whose only matchable tokens are
// corpus-common (rarity below bm25RarityFloor) cannot anchor a trustworthy
// BM25-only hit.
func (s *searchDB) queryMaxRarity(query string) float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.idx.QueryMaxRarity(query)
}

// docCount returns the number of indexed pages (corpus size N). The lexical
// rarity gate needs it to stay disabled for corpora too small to estimate
// "common in corpus" — in a tiny wiki every term looks common (df is a large
// fraction of N) and gating would drop legitimate hits.
func (s *searchDB) docCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.idx.Len()
}

// rebuildIndex clears and rebuilds the index from all .md files in dir.
func (s *searchDB) rebuildIndex(dir string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.idx.Clear()
	// Rebuild the validity map alongside the FTS index. This is the ONLY
	// index path a restart takes, and archived/superseded pages are never
	// rewritten afterwards — leaving validity empty here made every staleness
	// demotion a silent no-op for the whole process lifetime (year-old
	// archived facts outranked current pages until the next in-process write
	// of that exact page, i.e. never).
	s.validity = make(map[string]float64)

	return filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil //nolint:nilerr // skip inaccessible entries in walk
		}
		if info.IsDir() {
			// Prune backup/hidden dirs so their pages never enter the search
			// index — same rule as ListPages (store.go), so search and the
			// person/project lists agree on what a real page is.
			if path != dir && isNonPageDir(info.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".md" {
			return nil
		}
		base := filepath.Base(path)
		if base == "index.md" || base == "_index.md" || base == "log.md" {
			return nil
		}
		rel, _ := filepath.Rel(dir, path)
		page, err := ParsePageFile(path)
		if err != nil {
			// An unparseable page is functionally deleted: it stays on disk but
			// never appears in search again. Surface it instead of hiding it.
			slog.Warn("wiki: unparseable page skipped during search index rebuild",
				"path", rel, "error", err)
			return nil //nolint:nilerr // skip unparseable files
		}
		s.idx.UpsertFields(rel, searchablePageFieldsWithBoost(page, s.fieldBoost)...)
		s.validity[rel] = validityFactor(page.Meta, s.now())
		return nil
	})
}

// wikiFieldBoost is the BM25F-lite term-frequency weight for a page's IDENTITY
// fields (title, summary, id, tags, cue anchors) relative to prose (body,
// related, category — weight 1). Rationale: the measured crowding failure —
// a common noun repeated across long log bodies outranks the canonical page
// whose *title/summary* carries the term, pushing it out of recall's top-N.
// A match on what a page IS should outweigh the same word buried in what a
// page SAYS. 2.5 flips the bench crowding case while leaving every
// body-fact case (attr-not-in-title 등) intact; override via
// DENEB_WIKI_FIELD_BOOST (1.0 = flat legacy ranking, the rollback lever).
const wikiFieldBoost = 2.5

// wikiFieldBoostValue returns the identity-field boost, honoring the
// DENEB_WIKI_FIELD_BOOST override (mirrors semanticOnlyFloorValue). Malformed
// or non-positive overrides fall back to the default. Weights are baked at
// index time, so a changed override takes effect on the next index rebuild
// (gateway restart) — same lifecycle as the other search knobs.
func wikiFieldBoostValue() float64 {
	if v := os.Getenv("DENEB_WIKI_FIELD_BOOST"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 {
			return f
		}
	}
	return wikiFieldBoost
}

func searchablePageFields(page *Page) []textsearch.Field {
	return searchablePageFieldsWithBoost(page, wikiFieldBoostValue())
}

func searchablePageFieldsWithBoost(page *Page, boost float64) []textsearch.Field {
	if page == nil {
		return nil
	}
	return []textsearch.Field{
		{Text: page.Meta.Title, Weight: boost},
		{Text: page.Meta.Summary, Weight: boost},
		{Text: page.Meta.ID, Weight: boost},
		// Category names are generic bucket words (프로젝트/인물/…) — boosting them
		// would make every page in a bucket a strong match for the bucket word.
		{Text: page.Meta.Category, Weight: 1},
		{Text: strings.Join(page.Meta.Tags, " "), Weight: boost},
		// Related holds NEIGHBOR page paths — their vocabulary is not this page's
		// identity, so it stays at prose weight.
		{Text: strings.Join(page.Meta.Related, " "), Weight: 1},
		// Cue anchors: alternate phrasings a future query may use (Memora-style
		// entry points) — indexed so a paraphrased question reaches a page whose
		// own vocabulary differs (예: cue "계약금" ↔ 본문 "선수금"). Hidden: cues
		// are retrieval anchors, never content — without it a cue-only match
		// surfaced the raw cue text as the result "match" snippet (recall
		// evidence, memory search), presenting index metadata as page content.
		{Text: strings.Join(page.Meta.Cues, " "), Weight: boost, Hidden: true},
		{Text: page.Body, Weight: 1},
	}
}

// close is a no-op (in-memory index, nothing to close).
func (s *searchDB) close() error {
	return nil
}

// Search runs a search across wiki pages. With no embedder configured it is
// pure BM25 (exact prior behavior). When a semantic index is attached and the
// embedding server is healthy, it blends BM25 with dense-vector neighbors so a
// query also finds pages by meaning. Semantic degradation (server down, embed
// error) silently falls back to the BM25 result.
func (s *Store) Search(ctx context.Context, query string, limit int) ([]SearchResult, error) {
	report, err := s.SearchWithOptions(ctx, query, limit, QueryOptions{})
	return report.Results, err
}

// SearchWithOptions runs a diagnosable or stage-specific search while keeping
// Search's production defaults unchanged.
func (s *Store) SearchWithOptions(ctx context.Context, query string, limit int, options QueryOptions) (SearchReport, error) {
	var empty SearchReport
	if s.fts == nil {
		return empty, nil
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return empty, nil
	}
	if limit <= 0 {
		limit = 10
	}
	mode := normalizeSearchMode(options.Mode)
	// Over-fetch, demote, THEN truncate. validityFactor (archived/superseded/
	// aging demotion) multiplies scores after ranking — truncating at the
	// caller's limit first could return only stale pages while the current page
	// sat just outside the window. With identity-field boosts an archived
	// title match outranks a current body match, so at small limits the stale
	// page would be the ONLY result. Fetch a wider candidate set, apply
	// validity, and cut to the caller's limit at the very end. The +50 floor
	// keeps small limits meaningful in a mature wiki: at limit=1 the old +10
	// floor fetched 11 rows, and a project with a dozen boosted-but-archived
	// pages could fill all of them, leaving applyValidity nothing current to
	// rescue (stale crowd-out). Still bounded — this is one in-memory BM25 pass.
	fetchLimit := limit * 3
	if fetchLimit < limit+50 {
		fetchLimit = limit + 50
	}
	var bm25 []SearchResult
	var err error
	if mode != SearchModeSemantic {
		bm25, err = s.fts.search(ctx, query, fetchLimit)
		if err != nil {
			return empty, err
		}
	}
	// Lexical-leak gate: a query whose only matchable tokens are corpus-common
	// (no rare anchor term) produces BM25 hits that matched on a frequent word
	// and are likely off-topic — the measured recall leak. commonOnlyQuery is
	// true for exactly those; it gates UNCONFIRMED lexical hits below.
	//
	// DISABLED below bm25GateMinCorpus: in a tiny wiki every term's df is a large
	// fraction of N, so even a distinctive term scores "common" and the gate
	// would drop legitimate hits (a single-page corpus searched for its one rare
	// word, a 2-page corpus where both pages share the query terms). The leak it
	// guards needs many pages for a common word to collide with off-topic ones;
	// at small N there is little to leak and the conservative choice is gate-off.
	rarityFloor := s.bm25RarityFloor
	if rarityFloor <= 0 {
		rarityFloor = bm25RarityFloorValue()
	}
	commonOnlyQuery := len(bm25) > 0 && s.fts.docCount() >= bm25GateMinCorpus &&
		s.fts.queryMaxRarity(query) < rarityFloor

	var sem []SearchResult
	if mode != SearchModeBM25 {
		sem = s.searchSemantic(ctx, query, max(fetchLimit, semanticBlendK))
	}
	loadIntent := func() []SearchResult {
		intentResults, _ := s.fts.search(ctx, strings.TrimSpace(options.Intent), fetchLimit)
		return intentResults
	}
	return s.composeSearchReport(ctx, query, limit, fetchLimit, bm25, sem, commonOnlyQuery, options, loadIntent), nil
}

// graphBoostPaths returns the graph-proximity ranking for query (the third RRF
// signal), or nil when disabled (DENEB_WIKI_GRAPH_BOOST=off). Default ON: on the
// wiki-qa gold set it added +4.6pt P@1 / +4.5pt R@8 over RRF-alone for ~17ms per
// query (cmd/recall-bench), bringing the graph signal — previously only in chat
// recall anchors — into wiki.Store.Search / miniapp.memory.search. Bounded to
// semanticBlendK neighbors, the same window the fusion already over-fetches.
func (s *Store) graphBoostPaths(ctx context.Context, query string) []string {
	// The additive rollback path discards graphPaths (see fuseSearchResults), so
	// building the graph + embedding-reranking it there is pure waste — and this
	// runs per query inside SearchBatch under the ~1.5s recall budget. Skip it.
	if os.Getenv("DENEB_WIKI_GRAPH_BOOST") == "off" || os.Getenv("DENEB_WIKI_FUSION") == "additive" {
		return nil
	}
	return s.graphRankedPaths(ctx, query, semanticBlendK)
}

// truncateResults cuts a validity-adjusted, re-sorted result list down to the
// caller's limit — the final step of the over-fetch → demote → truncate order.
func truncateResults(results []SearchResult, limit int) []SearchResult {
	if len(results) > limit {
		return results[:limit]
	}
	return results
}

// SearchBatch runs Search for several queries while embedding them all in ONE
// request, so the embedding server fans the query vectors across its context
// pool instead of a per-query round-trip serializing on one. Each query's
// BM25/rarity/blend/validity path is IDENTICAL to Search — only the semantic
// query embed is shared. Returns one result slice per query, index-aligned with
// queries (a query too short to embed, or the embedder being down, transparently
// degrades that query to pure BM25, exactly like Search). The recall preflight
// is the caller: it issues 2-3 wiki queries per turn.
func (s *Store) SearchBatch(ctx context.Context, queries []string, limit int) ([][]SearchResult, error) {
	reports, err := s.SearchBatchWithOptions(ctx, queries, limit, QueryOptions{})
	if err != nil {
		return nil, err
	}
	out := make([][]SearchResult, len(reports))
	for i := range reports {
		out[i] = reports[i].Results
	}
	return out, nil
}

// SearchBatchWithOptions preserves SearchBatch's single embedding request while
// enabling the same explain/mode/intent behavior as SearchWithOptions.
func (s *Store) SearchBatchWithOptions(ctx context.Context, queries []string, limit int, options QueryOptions) ([]SearchReport, error) {
	if s.fts == nil || len(queries) == 0 {
		return nil, nil
	}
	if limit <= 0 {
		limit = 10
	}
	mode := normalizeSearchMode(options.Mode)
	// One embed round-trip for all queries. nil (whole slice) when the embedder
	// is unavailable; per-entry nil for a query too short to embed — both leave
	// that query on the pure-BM25 path below.
	var qvecs [][]float32
	if mode != SearchModeBM25 {
		qvecs = s.embedQueriesBatch(ctx, queries)
	}
	intentFetchLimit := limit * 3
	if intentFetchLimit < limit+50 {
		intentFetchLimit = limit + 50
	}
	var intentOnce sync.Once
	var intentResults []SearchResult
	loadIntent := func() []SearchResult {
		intentOnce.Do(func() {
			intentResults, _ = s.fts.search(ctx, strings.TrimSpace(options.Intent), intentFetchLimit)
		})
		return intentResults
	}

	out := make([]SearchReport, len(queries))
	for i, query := range queries {
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
		query = strings.TrimSpace(query)
		if query == "" {
			continue
		}
		fetchLimit := limit * 3
		if fetchLimit < limit+50 {
			fetchLimit = limit + 50
		}
		var bm25 []SearchResult
		var err error
		if mode != SearchModeSemantic {
			bm25, err = s.fts.search(ctx, query, fetchLimit)
			if err != nil {
				return nil, err
			}
		}
		rarityFloor := s.bm25RarityFloor
		if rarityFloor <= 0 {
			rarityFloor = bm25RarityFloorValue()
		}
		commonOnlyQuery := len(bm25) > 0 && s.fts.docCount() >= bm25GateMinCorpus &&
			s.fts.queryMaxRarity(query) < rarityFloor

		var sem []SearchResult
		if qvecs != nil && len(qvecs[i]) > 0 {
			sem = s.searchSemanticWithVec(qvecs[i], max(fetchLimit, semanticBlendK))
		}
		out[i] = s.composeSearchReport(ctx, query, limit, fetchLimit, bm25, sem, commonOnlyQuery, options, loadIntent)
	}
	return out, nil
}

const (
	intentAmbiguousGap   = 0.08
	intentWeakTopScore   = 0.55
	intentRerankMaxBonus = 0.12
)

func normalizeSearchMode(mode SearchMode) SearchMode {
	switch mode {
	case "", SearchModeAuto:
		return SearchModeAuto
	case SearchModeBM25, SearchModeSemantic, SearchModeHybrid, SearchModeFull:
		return mode
	default:
		return SearchModeAuto
	}
}

func (s *Store) composeSearchReport(
	ctx context.Context,
	query string,
	limit, fetchLimit int,
	bm25, sem []SearchResult,
	commonOnlyQuery bool,
	options QueryOptions,
	loadIntent func() []SearchResult,
) SearchReport {
	mode := normalizeSearchMode(options.Mode)
	diagnostics := SearchDiagnostics{
		Mode:              mode,
		SemanticAvailable: len(sem) > 0,
		CommonOnlyQuery:   commonOnlyQuery,
	}
	var graphPaths []string
	var results []SearchResult

	switch mode {
	case SearchModeBM25:
		diagnostics.Fusion = "bm25"
		if commonOnlyQuery {
			diagnostics.Dropped = appendDrop(diagnostics.Dropped, "common_lexical_without_semantic", len(bm25))
		} else {
			results = append([]SearchResult(nil), bm25...)
		}
	case SearchModeSemantic:
		diagnostics.Fusion = "semantic"
		floor := semanticOnlyFloorValue()
		for _, result := range sem {
			if result.Score < floor {
				diagnostics.Dropped = appendDrop(diagnostics.Dropped, "semantic_floor", 1)
				continue
			}
			results = append(results, result)
		}
	case SearchModeHybrid, SearchModeFull, SearchModeAuto:
		if len(sem) == 0 {
			diagnostics.Fusion = "bm25-fallback"
			if commonOnlyQuery {
				diagnostics.Dropped = appendDrop(diagnostics.Dropped, "common_lexical_without_semantic", len(bm25))
			} else {
				results = append([]SearchResult(nil), bm25...)
			}
			break
		}
		if mode == SearchModeFull {
			graphPaths = s.graphRankedPaths(ctx, query, semanticBlendK)
		} else if mode == SearchModeAuto {
			graphPaths = s.graphBoostPaths(ctx, query)
		}
		diagnostics.GraphCandidates = len(graphPaths)
		if mode == SearchModeAuto && os.Getenv("DENEB_WIKI_FUSION") == "additive" {
			diagnostics.Fusion = "additive"
			results = mergeSearchResults(bm25, sem, fetchLimit, commonOnlyQuery)
		} else {
			diagnostics.Fusion = "rrf"
			results = mergeSearchResultsRRF(bm25, sem, graphPaths, fetchLimit, commonOnlyQuery)
		}
		diagnostics.Dropped = append(diagnostics.Dropped, admissionDropSummary(bm25, sem, graphPaths, commonOnlyQuery)...)
	}

	diagnostics.CandidateCount = searchCandidateCount(bm25, sem, graphPaths)
	baseScores := make(map[string]float64, len(results))
	for _, result := range results {
		baseScores[result.Path] = result.Score
	}
	if !options.skipValidity {
		results = s.fts.applyValidity(results)
	}
	var intentResults []SearchResult
	if shouldIntentRerank(results, options) && loadIntent != nil {
		intentResults = loadIntent()
	}
	intentBonuses, applied := s.applyIntentRerank(results, intentResults, options)
	diagnostics.IntentApplied = applied
	// Rerank uses BM25/semantic Content snippets already on the candidates —
	// do not attach disk metadata before rerank (avoids a second ReadPage pass
	// over up to rerankCandidateLimit hits). Enrich once after truncate.
	var rerankScores, rerankWeights map[string]float64
	rerankDiagnostics := RerankDiagnostics{Reason: "deferred_to_query_plan"}
	if !options.SkipRerank {
		rerankScores, rerankWeights, rerankDiagnostics = s.applyModelRerank(ctx, query, results, options.ForceRerank)
	}
	diagnostics.Rerank = rerankDiagnostics

	admitted := len(results)
	results = truncateResults(results, limit)
	if !options.skipMetadata {
		s.attachResultMetadata(query, results)
	}
	if admitted > len(results) {
		diagnostics.Dropped = appendDrop(diagnostics.Dropped, "result_limit", admitted-len(results))
	}
	diagnostics.ReturnedCount = len(results)
	if options.Explain {
		s.attachSearchExplanations(results, diagnostics.Fusion, bm25, sem, graphPaths, intentResults, baseScores, intentBonuses, applied)
		attachRerankExplanations(results, rerankScores, rerankWeights)
	}
	return SearchReport{Results: results, Diagnostics: diagnostics}
}

func (s *Store) attachResultMetadata(query string, results []SearchResult) {
	for i := range results {
		page, err := s.ReadPage(results[i].Path)
		if err != nil || page == nil {
			continue
		}
		results[i].Context = hierarchicalPageContext(results[i].Path, page)
		if results[i].Line > 0 && results[i].Content != "" {
			if results[i].EndLine < results[i].Line {
				results[i].EndLine = results[i].Line
			}
			continue
		}
		source := page.Body
		offset := pageBodyStartLine(page) - 1
		if strings.TrimSpace(source) == "" {
			results[i].Content = visiblePageIdentity(page)
			results[i].Line = 1
			results[i].EndLine = 1
			continue
		}
		snippet, start, end := textsearch.LocateSnippet(source, query, 5)
		if snippet == "" {
			snippet = visiblePageIdentity(page)
		}
		results[i].Content = snippet
		results[i].Line = start + offset
		results[i].EndLine = end + offset
	}
}

func hierarchicalPageContext(relPath string, page *Page) []string {
	clean := filepath.ToSlash(strings.TrimSuffix(strings.TrimSpace(relPath), ".md"))
	parts := strings.Split(clean, "/")
	context := make([]string, 0, len(parts)+3)
	for i := range parts {
		if strings.TrimSpace(parts[i]) != "" {
			context = append(context, strings.Join(parts[:i+1], "/"))
		}
	}
	if page != nil {
		if client := strings.TrimSpace(page.Meta.Client); client != "" {
			context = append(context, "거래처: "+client)
		}
		if title := strings.TrimSpace(page.Meta.Title); title != "" && (len(context) == 0 || context[len(context)-1] != title) {
			context = append(context, "문서: "+title)
		}
	}
	return context
}

func appendDrop(drops []SearchDropSummary, reason string, count int) []SearchDropSummary {
	if count <= 0 {
		return drops
	}
	for i := range drops {
		if drops[i].Reason == reason {
			drops[i].Count += count
			return drops
		}
	}
	return append(drops, SearchDropSummary{Reason: reason, Count: count})
}

func admissionDropSummary(bm25, sem []SearchResult, graphPaths []string, commonOnlyQuery bool) []SearchDropSummary {
	type candidate struct {
		bm25   bool
		semCos float64
		graph0 bool
	}
	byPath := make(map[string]*candidate, len(bm25)+len(sem)+len(graphPaths))
	for _, result := range bm25 {
		item := byPath[result.Path]
		if item == nil {
			item = &candidate{}
			byPath[result.Path] = item
		}
		item.bm25 = true
	}
	for _, result := range sem {
		item := byPath[result.Path]
		if item == nil {
			item = &candidate{}
			byPath[result.Path] = item
		}
		item.semCos = max(item.semCos, result.Score)
	}
	if len(graphPaths) > 0 {
		item := byPath[graphPaths[0]]
		if item == nil {
			item = &candidate{}
			byPath[graphPaths[0]] = item
		}
		item.graph0 = true
	}
	var drops []SearchDropSummary
	for _, item := range byPath {
		if !item.bm25 && !(item.graph0 && !commonOnlyQuery) && item.semCos < semanticOnlyFloorValue() {
			drops = appendDrop(drops, "semantic_floor", 1)
			continue
		}
		if commonOnlyQuery && item.bm25 && item.semCos < semSupportThreshold {
			drops = appendDrop(drops, "common_lexical_without_semantic", 1)
		}
	}
	return drops
}

func searchCandidateCount(bm25, sem []SearchResult, graphPaths []string) int {
	seen := make(map[string]struct{})
	for _, results := range [][]SearchResult{bm25, sem} {
		for _, result := range results {
			seen[result.Path] = struct{}{}
		}
	}
	for _, path := range graphPaths {
		seen[path] = struct{}{}
	}
	return len(seen)
}

func (s *Store) applyIntentRerank(results, intent []SearchResult, options QueryOptions) (map[string]float64, bool) {
	bonuses := make(map[string]float64)
	if !shouldIntentRerank(results, options) || len(intent) == 0 {
		return bonuses, false
	}
	intentRanks := resultRankMap(intent)
	k := rrfKValue()
	for i := range results {
		rank, ok := intentRanks[results[i].Path]
		if !ok {
			continue
		}
		bonus := intentRerankMaxBonus * (k + 1) / (k + float64(rank))
		bonus *= s.fts.validityFor(results[i].Path)
		results[i].Score += bonus
		bonuses[results[i].Path] = bonus
	}
	if len(bonuses) == 0 {
		return bonuses, false
	}
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].Score != results[j].Score {
			return results[i].Score > results[j].Score
		}
		return results[i].Path < results[j].Path
	})
	return bonuses, true
}

func shouldIntentRerank(results []SearchResult, options QueryOptions) bool {
	if strings.TrimSpace(options.Intent) == "" || len(results) < 2 {
		return false
	}
	if options.ForceIntent {
		return true
	}
	gap := results[0].Score - results[1].Score
	return gap < intentAmbiguousGap || results[0].Score < intentWeakTopScore
}

func resultRankMap(results []SearchResult) map[string]int {
	ranks := make(map[string]int, len(results))
	for i, result := range results {
		ranks[result.Path] = i + 1
	}
	return ranks
}

func resultScoreMap(results []SearchResult) map[string]float64 {
	scores := make(map[string]float64, len(results))
	for _, result := range results {
		scores[result.Path] = result.Score
	}
	return scores
}

func (s *Store) attachSearchExplanations(
	results []SearchResult,
	fusion string,
	bm25, sem []SearchResult,
	graphPaths []string,
	intent []SearchResult,
	baseScores, intentBonuses map[string]float64,
	intentApplied bool,
) {
	bm25Ranks, semRanks := resultRankMap(bm25), resultRankMap(sem)
	bm25Scores, semScores := resultScoreMap(bm25), resultScoreMap(sem)
	graphRanks := make(map[string]int, len(graphPaths))
	for i, path := range graphPaths {
		graphRanks[path] = i + 1
	}
	intentRanks, intentScores := resultRankMap(intent), resultScoreMap(intent)
	k := rrfKValue()
	scale := 0.4 * (k + 1)
	signal := func(source string, rank int, score float64) *SearchSignalExplanation {
		if rank <= 0 {
			return nil
		}
		contribution := 0.0
		switch {
		case fusion == "rrf":
			contribution = scale / (k + float64(rank))
		case source == "bm25" && (fusion == "bm25" || fusion == "bm25-fallback"):
			contribution = score
		case source == "semantic" && fusion == "semantic":
			contribution = score
		}
		return &SearchSignalExplanation{Rank: rank, BackendScore: score, Contribution: contribution}
	}
	for i := range results {
		path := results[i].Path
		explanation := &SearchExplanation{
			Fusion:         fusion,
			BM25:           signal("bm25", bm25Ranks[path], bm25Scores[path]),
			Semantic:       signal("semantic", semRanks[path], semScores[path]),
			Graph:          signal("graph", graphRanks[path], 0),
			BaseScore:      baseScores[path],
			IntentBonus:    intentBonuses[path],
			ValidityFactor: s.fts.validityFor(path),
			FinalScore:     results[i].Score,
		}
		if intentApplied {
			explanation.Intent = signal("intent", intentRanks[path], intentScores[path])
			if explanation.Intent != nil {
				explanation.Intent.Contribution = intentBonuses[path]
			}
		}
		results[i].Explain = explanation
	}
}

const (
	// semAgreementBonus rewards a BM25 hit confirmed by real semantic similarity
	// (cosine >= semSupportThreshold) — the two signals agreeing is strong
	// evidence of relevance.
	semAgreementBonus = 0.1
	// bm25OnlyPenalty demotes a BM25 hit whose semantic similarity to the query
	// is weak/absent (cosine < semSupportThreshold) — a common query word that
	// matched an otherwise-unrelated page. Without it, max(bm25,cosine) lets such
	// a lexical false positive keep its full BM25 score even when it is
	// semantically off-topic — e.g. "리눅스 파일 권한" matching a "트리나솔라 모듈
	// 계약" page on the shared word "파일". No-op when semantic did not run.
	bm25OnlyPenalty = 0.7
	// semSupportThreshold is the cosine above which a page counts as genuinely
	// related, so a BM25 hit is confirmed rather than a lexical accident.
	// On-topic pages measure ~0.6-0.76; off-topic lexical matches ~0.2-0.3.
	semSupportThreshold = 0.4
	// semanticBlendK widens the semantic neighbor set used for the blend beyond
	// the result limit, so a BM25 hit's cosine is known even when the page is not
	// in the semantic top-N — otherwise a relevant page just outside the top-N
	// would be wrongly demoted as having "no semantic support".
	semanticBlendK = 30
	// semanticOnlyFloor is the cosine a SEMANTIC-ONLY hit (no BM25/lexical match
	// at all) must clear to be admitted. Before this floor the semantic-only
	// branch had NO admission gate: searchSemantic keeps any cosine > 0
	// (semantic.go) and mergeSearchResults' bonus/penalty cases both require
	// inBM25, so a page BM25 never touched kept its full raw cosine and could
	// fill a BM25-empty recall query with an off-topic page (measured: an
	// unrelated wiki page injected at score 0.6302 == raw cosine, no gate).
	//
	// The floor applies ONLY to semantic-only hits. A page with any lexical
	// match (inBM25) is left to the existing bonus/penalty logic — the floor is
	// purely the missing gate on the floorless branch, so lexically-relevant
	// pages (including the bm25OnlyPenalty-demoted ones) are unaffected.
	//
	// Value 0.70 (override via DENEB_WIKI_SEM_FLOOR). Rationale: BGE-M3 packs
	// Korean text into a high, narrow cosine band — even a totally unrelated
	// Korean (query,doc) pair scores ~0.58–0.69 and a genuinely relevant one
	// ~0.77–0.86 (filestore/semindex.go:80-82, measured on the live srv4 :8001).
	// The floor must sit INSIDE that separation band, not at the generic-cosine
	// level. filestore's office-doc corpus has a clean window [0.689, 0.772]
	// (midpoint 0.73), but wiki pages are SHORT curated summaries (title +
	// summary + body), so a genuinely relevant page's cosine can land a notch
	// lower than a full document's — 0.70 is the conservative choice: still above
	// the ~0.69 irrelevant-band ceiling (rejecting the off-topic leak) while
	// leaving more headroom under the relevant band so a terse on-topic summary
	// is not dropped. An srv4 sweep over the real wiki corpus is the final
	// confirmation of the exact value; 0.70 is the measurement-grounded default.
	semanticOnlyFloor = 0.70

	// bm25RarityFloor is the SYMMETRIC counterpart to semanticOnlyFloor, for the
	// lexical (BM25) path. It is the minimum corpus-rarity (textsearch
	// NormalizedRarity, 0–1) the rarest matchable token of a query must clear for
	// a BM25-ONLY hit (no semantic confirmation) to be admitted. A query whose
	// every matchable token is corpus-common (rarity below the floor) is a weak
	// lexical query: its hits matched on a frequent word and are likely off-topic
	// (the measured recall leak — a single common noun like "보고"/"일정" matched
	// unrelated pages at confidence high/medium). Hits from such a query are kept
	// ONLY when semantic similarity independently confirms them.
	//
	// This gate is deliberately MORE conservative than semanticOnlyFloor, by
	// design: semantic similarity scores EVERY page against the query, so a weak
	// semantic-only match is always noise; but a single lexical match CAN be a
	// strong signal when the term is rare (a 거래처명/고유명사 appearing in one
	// page is a legitimate one-term recall). So the gate keys on the term's
	// rarity, not the hit's score — a rare anchor survives, only common-only
	// queries are floored, and it never touches a semantically-confirmed hit.
	//
	// Value 0.55 (override via DENEB_WIKI_BM25_RARITY_FLOOR). Rationale, measured
	// across corpus sizes N∈{22,30,100,200,263} (NormalizedRarity is N-stable,
	// unlike raw IDF or normalized BM25 which both drift with N): a rare term
	// (df 1–3) scores 0.69–1.0 at every N — comfortably above the floor; a noun
	// in ~10–20% of pages (the leak band, e.g. "보고" at df 40/263) scores
	// 0.31–0.52 at realistic N — below it. 0.55 sits in that valley, leaving
	// headroom so a df=2–3 term in even a small corpus (rarity ≥0.69) is never
	// dropped (conservative: when unsure, keep).
	bm25RarityFloor = 0.55

	// bm25GateMinCorpus is the smallest corpus (page count N) at which the
	// lexical rarity gate engages. Below it the gate is OFF: NormalizedRarity is
	// a ratio of IDFs, and when N is small even a df of 2–3 is a large fraction
	// of N so every term reads as "common" (measured: at N=2 a query both pages
	// share scores rarity ~0.26; at N=22 a df=3 term is ~0.69, right at the
	// boundary). The leak the gate guards is a scale phenomenon — a common word
	// needs many pages to collide with off-topic ones — so disabling it for small
	// wikis costs no real protection while preventing false drops in a young or
	// test corpus. 30 is comfortably below the production wiki (~260 pages) and
	// above the band where the rarity ratio is too coarse to trust.
	bm25GateMinCorpus = 30
)

// bm25RarityFloorValue returns the lexical-query rarity floor, honoring the
// DENEB_WIKI_BM25_RARITY_FLOOR override (mirrors semanticOnlyFloorValue). A
// malformed or out-of-(0,1] override is ignored in favor of the default.
func bm25RarityFloorValue() float64 {
	if v := os.Getenv("DENEB_WIKI_BM25_RARITY_FLOOR"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 && f <= 1 {
			return f
		}
	}
	return bm25RarityFloor
}

// semanticOnlyFloorValue returns the cosine admission floor for semantic-only
// hits, honoring the DENEB_WIKI_SEM_FLOOR override (mirrors filestore's
// minSemanticScore default-plus-override pattern). A malformed or out-of-(0,1]
// override is ignored in favor of the default.
func semanticOnlyFloorValue() float64 {
	if v := os.Getenv("DENEB_WIKI_SEM_FLOOR"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 && f <= 1 {
			return f
		}
	}
	return semanticOnlyFloor
}

// mergeSearchResults blends lexical (BM25) and semantic hits, scoring each page
// by max(bm25, cosine). A BM25 hit confirmed by real semantic similarity
// (cosine >= semSupportThreshold) gets an agreement bonus; a BM25 hit with
// weak/absent semantic support is demoted (bm25OnlyPenalty) as a likely lexical
// false positive. A semantic-only hit (no lexical match) is admitted only above
// semanticOnlyFloor and then keeps its cosine — the floor is the admission gate
// the floorless semantic-only branch lacked. BM25 snippets are preserved. Order
// is by blended score, descending; ties broken by path.
//
// commonOnlyQuery is the lexical counterpart to that floor: when true (the query
// has no rare anchor term — every matchable token is corpus-common), a BM25 hit
// that semantic does NOT independently confirm (cosine < semSupportThreshold) is
// DROPPED, not merely demoted — it matched only on a frequent word and is the
// measured leak. Semantically-confirmed lexical hits and genuine semantic-only
// hits are unaffected, so a relevant page that happens to share a common word
// still surfaces via its meaning.
func mergeSearchResults(bm25, sem []SearchResult, limit int, commonOnlyQuery bool) []SearchResult {
	type merged struct {
		res       SearchResult
		bm25Score float64
		semCos    float64
		final     float64
		inBM25    bool
		inSem     bool
	}
	byPath := make(map[string]*merged, len(bm25)+len(sem))
	for _, r := range bm25 {
		if m := byPath[r.Path]; m != nil {
			if r.Score > m.bm25Score {
				m.bm25Score = r.Score
			}
			m.inBM25 = true
			if m.res.Content == "" && r.Content != "" {
				m.res.Content = r.Content
			}
			continue
		}
		byPath[r.Path] = &merged{res: r, bm25Score: r.Score, inBM25: true}
	}
	for _, r := range sem {
		if m := byPath[r.Path]; m != nil {
			if r.Score > m.semCos {
				m.semCos = r.Score
			}
			m.inSem = true
			continue
		}
		byPath[r.Path] = &merged{res: r, semCos: r.Score, inSem: true}
	}

	semAvailable := len(sem) > 0
	floor := semanticOnlyFloorValue()
	out := make([]merged, 0, len(byPath))
	for _, m := range byPath {
		// Semantic-only admission floor: a hit with NO lexical match (!inBM25)
		// reaches the result only on its cosine, which the searchSemantic stage
		// gates at >0 only. Without a floor here an off-topic page in the
		// Korean irrelevant cosine band (~0.6) is admitted and can fill a
		// BM25-empty recall query (the measured leak). BM25 hits are never
		// floored — they took a lexical path and keep the existing
		// bonus/penalty treatment below.
		if !m.inBM25 && m.semCos < floor {
			continue
		}
		// Lexical-leak gate (symmetric to the floor above): for a common-only
		// query (no rare anchor term), a lexical hit semantic does not confirm
		// (cosine < semSupportThreshold) matched only on a frequent word — drop
		// it. A semantically-confirmed lexical hit (>= threshold) survives, so a
		// genuinely relevant page sharing a common word is not lost.
		if commonOnlyQuery && m.inBM25 && m.semCos < semSupportThreshold {
			continue
		}
		m.final = m.bm25Score
		if m.semCos > m.final {
			m.final = m.semCos
		}
		switch {
		case m.inBM25 && m.semCos >= semSupportThreshold:
			m.final += semAgreementBonus // lexical hit confirmed by semantic similarity
		case m.inBM25 && semAvailable:
			m.final *= bm25OnlyPenalty // lexical hit with weak/no semantic support
		}
		m.res.Score = m.final
		out = append(out, *m)
	}
	sort.Slice(out, func(a, b int) bool {
		if out[a].final != out[b].final {
			return out[a].final > out[b].final
		}
		return out[a].res.Path < out[b].res.Path
	})
	if len(out) > limit {
		out = out[:limit]
	}
	results := make([]SearchResult, len(out))
	for i := range out {
		results[i] = out[i].res
	}
	return results
}

// rrfK is the Reciprocal Rank Fusion damping constant (Cormack et al. 2009). The
// field-standard 60 was tuned on TREC-scale collections (huge corpora, many
// relevant docs/query) where a flat, high-k curve rewards CONSENSUS across many
// noisy rankers. Deneb is the opposite: a small curated wiki (~400 pages) with
// one relevant page/query and strong identity-field matching (title/summary/cues
// at a 2.5 boost), so the rank-1 hit is usually correct and a lower k — which
// peaks the top rank — helps. A sweep on the 100-case wiki-qa gold set
// (cmd/recall-bench) confirms a smooth monotonic trend (60→…→5), and R@8 (the
// metric that matters for recall evidence injection) actually PEAKS in the mid
// range: rrfK 15–20 hits R@8 98% vs 97% at both 10 and 60.
//
// 20 is chosen deliberately over the current-snapshot optimum (~5–10, P@1 78–79%):
// it maximizes R@8, keeps P@1 at the 60-baseline, sits BELOW the field default
// (our structured corpus wants that) yet ABOVE 10 — so as the wiki grows and its
// optimum drifts back toward the standard, 20 ages well instead of overfitting
// today's 400 pages. Re-sweep with cmd/recall-bench as the corpus grows; override
// via DENEB_WIKI_RRF_K (rrfKValue), 60 restores the field default.
const rrfK = 20.0

// rrfKValue honors a DENEB_WIKI_RRF_K override for tuning sweeps (cmd/recall-bench)
// over the default rrfK.
func rrfKValue() float64 {
	if v := os.Getenv("DENEB_WIKI_RRF_K"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 {
			return f
		}
	}
	return rrfK
}

// rrfSemWeightValue scales the SEMANTIC ranking's RRF contribution relative to
// BM25 (and graph, both fixed at 1.0). Default 1.0 = the historical equal
// weighting. Introduced for the Nemotron cutover: the hard gold set showed
// equal-weight fusion diluting a much stronger semantic ranker with a weak BM25
// (33% p@1) — sweep via DENEB_WIKI_RRF_SEM_WEIGHT with cmd/recall-bench, same
// runtime-override pattern as the floor/K knobs.
func rrfSemWeightValue() float64 {
	if v := os.Getenv("DENEB_WIKI_RRF_SEM_WEIGHT"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 {
			return f
		}
	}
	return 1.0
}

// fuseSearchResults dispatches BM25×semantic fusion. Reciprocal Rank Fusion is
// the default (DENEB_WIKI_FUSION=additive rolls back to the historical additive
// max(bm25,cosine)+bonus/penalty blend). RRF measured +11.3pt P@1 / +4.5pt R@8 /
// +0.07 MRR over additive on the 44-case wiki-qa gold set (cmd/recall-bench),
// with no wiki-test regression — it drops the fragile cross-signal score
// normalization the additive priors accrue as sources grow. The env read is the
// same runtime-override pattern as the floor knobs, so a bench scores both from
// one binary and an operator can roll back without a rebuild.
func fuseSearchResults(bm25, sem []SearchResult, graphPaths []string, limit int, commonOnlyQuery bool) []SearchResult {
	if os.Getenv("DENEB_WIKI_FUSION") == "additive" {
		return mergeSearchResults(bm25, sem, limit, commonOnlyQuery)
	}
	return mergeSearchResultsRRF(bm25, sem, graphPaths, limit, commonOnlyQuery)
}

// mergeSearchResultsRRF fuses the lexical and semantic rankings by Reciprocal
// Rank Fusion: a page's score is Σ 1/(rrfK + rank) over the rankings it appears
// in (rank is 1-based position in each already-sorted candidate list). Unlike
// the additive blend it needs NO cross-signal score normalization — BM25
// magnitudes and cosine bands never share an axis, only their ranks do, which is
// exactly the fragility the hand-tuned source priors accrue as sources grow. The
// SAME admission gates as mergeSearchResults apply (semantic-only cosine floor,
// common-only lexical drop), so RRF changes ordering, not what is admitted.
func mergeSearchResultsRRF(bm25, sem []SearchResult, graphPaths []string, limit int, commonOnlyQuery bool) []SearchResult {
	k := rrfKValue()
	// scale maps the tiny raw RRF sum (a rank-1-in-both hit is 2/(k+1)) back into
	// the ~0–1 band SearchResult.Score MUST live in: recall_evidence consumes it as
	// normalized relevance (0.80 + r.Score, high-confidence ≥1.10) to merge wiki
	// hits against diary/file/session on one axis. Left raw, every wiki hit capped
	// near 0.83 and could never be high-confidence — a regression the internal
	// hit@K bench can't see. 0.4·(k+1) puts the canonical strong hybrid hit (rank 1
	// in both bm25 and semantic) at 0.8 → 1.60 in recall, above 1.10 and below the
	// 2.2 project anchor; a three-signal hit (+graph seed) tops ≈1.2 → 2.0.
	scale := 0.4 * (k + 1)
	type merged struct {
		res    SearchResult
		semCos float64
		inBM25 bool
		graph0 bool // seed of the graph ranking (the named entity's own page)
		rrf    float64
	}
	byPath := make(map[string]*merged, len(bm25)+len(sem))
	for rank, r := range bm25 {
		m := byPath[r.Path]
		if m == nil {
			m = &merged{res: r}
			byPath[r.Path] = m
		}
		m.inBM25 = true
		m.rrf += 1.0 / (k + float64(rank+1))
		if m.res.Content == "" && r.Content != "" {
			m.res.Content = r.Content
		}
	}
	semWeight := rrfSemWeightValue()
	for rank, r := range sem {
		m := byPath[r.Path]
		if m == nil {
			m = &merged{res: r}
			byPath[r.Path] = m
		}
		if r.Score > m.semCos {
			m.semCos = r.Score
		}
		m.rrf += semWeight / (k + float64(rank+1))
	}
	// Graph proximity as a third RRF ranking: pages connected to the entity named
	// in the query (seed first, then neighbors by graph score). A page already
	// retrieved lexically/semantically gets its rank reinforced; a graph-only page
	// is admitted only if it is the SEED (the named entity's own page, rank 0) —
	// a high-confidence "the user asked about this exact thing" signal — so
	// arbitrary neighbors never inject themselves off a graph edge alone.
	for rank, p := range graphPaths {
		m := byPath[p]
		if m == nil {
			m = &merged{res: SearchResult{Path: p}}
			byPath[p] = m
		}
		if rank == 0 {
			m.graph0 = true
		}
		m.rrf += 1.0 / (k + float64(rank+1))
	}

	floor := semanticOnlyFloorValue()
	// The graph seed is resolved by substring match (graphRankedPaths → findSeed),
	// so on a common-only query — one with no rare anchor term — a corpus-common
	// noun in a page title can mark that page as the "seed" and wrongly exempt it
	// from the gates the rarity leak-guard exists to enforce. Trust the graph
	// signal for admission only when the query HAS a rare anchor (its entity match
	// is meaningful); otherwise both gates apply strictly.
	graphTrusted := !commonOnlyQuery
	out := make([]merged, 0, len(byPath))
	for _, m := range byPath {
		if !m.inBM25 && !(m.graph0 && graphTrusted) && m.semCos < floor {
			continue // semantic-only floor; only a trusted graph seed is injected below it
		}
		if commonOnlyQuery && m.inBM25 && m.semCos < semSupportThreshold {
			continue // lexical-leak drop — semantic must confirm; graph grants no exemption here
		}
		// Order is by raw m.rrf (below); Score carries the scaled ~0–1 relevance
		// cross-source recall needs. The scale is constant so the two never
		// disagree on ordering.
		m.res.Score = m.rrf * scale
		out = append(out, *m)
	}
	sort.Slice(out, func(a, b int) bool {
		if out[a].rrf != out[b].rrf {
			return out[a].rrf > out[b].rrf
		}
		return out[a].res.Path < out[b].res.Path
	})
	if len(out) > limit {
		out = out[:limit]
	}
	results := make([]SearchResult, len(out))
	for i := range out {
		results[i] = out[i].res
	}
	return results
}

// scoreToNormalized converts a raw BM25 score to a 0-1 range.
func scoreToNormalized(score float64) float64 {
	if score <= 0 {
		return 0
	}
	// Sigmoid normalization: maps [0, +inf) to (0, 1).
	return score / (score + 1)
}
