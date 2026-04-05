// search.go — Importance-weighted hybrid search over the structured memory store.
// Combines FTS5 keyword search + cosine similarity with importance and recency scoring.
package memory

import (
	"context"
	"math"
	"sort"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ffi"
)

// RerankFunc is a function that reranks documents by query relevance.
// Returns (index, score) pairs sorted by descending relevance.
type RerankFunc func(ctx context.Context, query string, docs []string, topN int) ([]RerankResult, error)

// RerankResult holds a reranked document's original index and relevance score.
type RerankResult struct {
	Index          int
	RelevanceScore float64
}

// SearchOpts configures a memory search.
type SearchOpts struct {
	Limit         int      // max results (default 10)
	Category      string   // filter by category (empty = all)
	MinScore      float64  // minimum final score threshold
	MinImportance float64  // minimum importance to include (0 = all; use 0.7 for FTS-only mode)
	EntityFilter  string   // filter by entity name (empty = all)
	ExtraKeywords []string // additional keywords to include in FTS (e.g., from LLM expansion)
	SkipRerank    bool     // skip cross-encoder reranking (caller will rerank separately)
}

// SearchResult is a scored fact from a search query.
type SearchResult struct {
	Fact         Fact          `json:"fact"`
	Score        float64       `json:"score"`
	FTSScore     float64       `json:"fts_score,omitempty"`
	VecScore     float64       `json:"vec_score,omitempty"`
	RelatedFacts []RelatedFact `json:"related_facts,omitempty"`
}

// categoryImportanceMultiplier adjusts the importance weight by fact category.
// Decisions, context, and solutions are factual records of what happened → boost.
// User model and mutual are relational/personality data → keep but don't over-boost.
var categoryImportanceMultiplier = map[string]float64{
	CategoryDecision:   1.20,
	CategoryPreference: 1.05,
	CategorySolution:   1.10,
	CategoryContext:    0.95,
	CategoryUserModel:  1.00,
	CategoryMutual:     0.85,
}

// categorySteepnessDays controls the inverse-square recency decay per category.
// The steepness value is the number of days at which score drops to 0.5.
// Curve: score = 1 / (1 + (days/steepness)²)
//   - At steepness days:  0.5
//   - At 2×steepness:     0.2
//   - At 3×steepness:     0.1
//
// Lower values = faster decay. Factual records (decision, context, solution)
// get higher steepness so recent project history persists across sessions.
var categorySteepnessDays = map[string]float64{
	CategoryDecision:   14.0, // important decisions persist ~2 weeks at high score
	CategoryPreference: 10.0,
	CategorySolution:   10.0,
	CategoryContext:    5.0,  // project state: very fresh-biased (1-2 days dominant)
	CategoryUserModel:  14.0, // user traits: moderate persistence
	CategoryMutual:     7.0,  // relationship signals: 1-week window
}

// SearchFacts performs a hybrid FTS + semantic search over active facts,
// then applies importance and recency weighting.
func (s *Store) SearchFacts(ctx context.Context, query string, queryVec []float32, opts SearchOpts) ([]SearchResult, error) {
	if opts.Limit <= 0 {
		opts.Limit = 10
	}

	// Phase 1: FTS search.
	ftsResults, err := s.ftsSearch(ctx, query, opts)
	if err != nil {
		return nil, err
	}

	// Phase 1.5: Entity-based search enrichment.
	// If EntityFilter is set, add matching facts. Otherwise, try to find
	// entity matches from the query to enrich the candidate pool.
	if opts.EntityFilter != "" {
		entityFacts := s.entitySearch(ctx, opts.EntityFilter)
		for id, score := range entityFacts {
			if _, exists := ftsResults[id]; !exists {
				ftsResults[id] = score
			}
		}
	}

	// Phase 2: Vector search (if embedding provided).
	var vecResults map[int64]float64
	if len(queryVec) > 0 {
		vecResults, err = s.vectorSearch(ctx, queryVec)
		if err != nil {
			// Non-fatal: fall back to FTS-only.
			vecResults = nil
		}
	}

	// Phase 3: Merge and score (fetches extra candidates for dedup headroom).
	mergeOpts := opts
	mergeOpts.Limit = opts.Limit * 3
	results := s.mergeAndRank(ftsResults, vecResults, mergeOpts)

	// Phase 3.5: Content deduplication — remove near-duplicate facts so the
	// LLM context isn't wasted on 3-5 copies of the same information.
	results = dedupResults(results, s.searchParams().DedupJaccardThreshold)

	// Phase 4: Cross-encoder reranking (optional).
	// Skipped when caller will rerank separately (e.g., Recall after entity/relation expansion).
	if s.reranker != nil && len(results) > 1 && !opts.SkipRerank {
		results = s.rerankFacts(ctx, query, results)
	}

	// Final truncation after all post-processing.
	if len(results) > opts.Limit {
		results = results[:opts.Limit]
	}

	return results, nil
}

// ftsSearch performs FTS5 search and returns scored fact IDs.
// Uses a two-stage strategy: AND first (all tokens must match) for precision,
// falling back to OR (any token matches) when AND yields < ftsAndMinResults.
func (s *Store) ftsSearch(ctx context.Context, query string, opts SearchOpts) (map[int64]float64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Use Rust keyword extraction for multilingual tokenization (stop-word removal,
	// Korean particle stripping, CJK bigrams). Falls back to naive whitespace split.
	tokens, err := ffi.MemoryExtractKeywords(query)
	if err != nil || len(tokens) == 0 {
		tokens = splitTokens(query)
	}

	// Append caller-supplied extra keywords (e.g., from Vega LLM expansion).
	if len(opts.ExtraKeywords) > 0 {
		seen := make(map[string]bool, len(tokens))
		for _, t := range tokens {
			seen[t] = true
		}
		for _, kw := range opts.ExtraKeywords {
			if !seen[kw] {
				tokens = append(tokens, kw)
				seen[kw] = true
			}
		}
	}

	// Stage 1: AND query (all tokens must match) for higher precision.
	andQuery := buildFTSQuery(tokens, "AND")
	results := s.runFTSQuery(ctx, andQuery, opts)

	// Stage 2: OR fallback when AND yields too few results.
	p := s.searchParams()
	if len(results) < p.FTSAndMinResults && len(tokens) > 1 {
		orQuery := buildFTSQuery(tokens, "OR")
		orResults := s.runFTSQuery(ctx, orQuery, opts)
		for id, score := range orResults {
			if _, exists := results[id]; !exists {
				// Slightly penalize OR-only results vs AND matches.
				results[id] = score * p.ORPenalty
			}
		}
	}

	// Korean/CJK fallback: if unicode61 found nothing, try trigram index.
	if len(results) == 0 {
		trigramResults := s.trigramSearch(ctx, query, opts.MinImportance)
		for id, score := range trigramResults {
			results[id] = score
		}
	}

	return results, nil
}

// runFTSQuery executes an FTS5 MATCH query with the given opts and returns scored IDs.
func (s *Store) runFTSQuery(ctx context.Context, ftsQuery string, opts SearchOpts) map[int64]float64 {
	if ftsQuery == "" {
		return make(map[int64]float64)
	}

	var rowsQuery string
	var args []any

	switch {
	case opts.Category != "" && opts.MinImportance > 0:
		rowsQuery = `SELECT f.id, fts.rank
			FROM facts_fts fts
			JOIN facts f ON f.id = fts.rowid
			WHERE facts_fts MATCH ? AND f.active = 1 AND f.category = ? AND f.importance >= ?
			ORDER BY fts.rank
			LIMIT 50`
		args = []any{ftsQuery, opts.Category, opts.MinImportance}
	case opts.Category != "":
		rowsQuery = `SELECT f.id, fts.rank
			FROM facts_fts fts
			JOIN facts f ON f.id = fts.rowid
			WHERE facts_fts MATCH ? AND f.active = 1 AND f.category = ?
			ORDER BY fts.rank
			LIMIT 50`
		args = []any{ftsQuery, opts.Category}
	case opts.MinImportance > 0:
		rowsQuery = `SELECT f.id, fts.rank
			FROM facts_fts fts
			JOIN facts f ON f.id = fts.rowid
			WHERE facts_fts MATCH ? AND f.active = 1 AND f.importance >= ?
			ORDER BY fts.rank
			LIMIT 50`
		args = []any{ftsQuery, opts.MinImportance}
	default:
		rowsQuery = `SELECT f.id, fts.rank
			FROM facts_fts fts
			JOIN facts f ON f.id = fts.rowid
			WHERE facts_fts MATCH ? AND f.active = 1
			ORDER BY fts.rank
			LIMIT 50`
		args = []any{ftsQuery}
	}

	rows, err := s.db.QueryContext(ctx, rowsQuery, args...)
	if err != nil {
		// FTS match can fail on malformed queries; return empty rather than error.
		return make(map[int64]float64)
	}
	defer rows.Close()

	results := make(map[int64]float64)
	for rows.Next() {
		var id int64
		var rank float64
		if err := rows.Scan(&id, &rank); err != nil {
			continue
		}
		results[id] = rankToScore(rank)
	}
	return results
}

// trigramSearch uses the trigram FTS5 index for CJK/Korean substring matching.
// Searches individual tokens with OR instead of the full query as a phrase,
// because short Korean tokens (< 3 chars) can't form trigrams as phrases.
// minImportance filters out low-importance facts when > 0 (e.g. 0.7 for FTS-only mode).
func (s *Store) trigramSearch(ctx context.Context, query string, minImportance float64) map[int64]float64 {
	// Split into individual tokens and keep only those long enough for trigram
	// matching (>= 3 Unicode characters).
	tokens := splitTokens(query)
	var trigramTokens []string
	for _, t := range tokens {
		charCount := 0
		for range t {
			charCount++
		}
		if charCount >= 3 {
			trigramTokens = append(trigramTokens, `"`+stripQuotes(t)+`"`)
		}
	}
	if len(trigramTokens) == 0 {
		return nil
	}
	trigramQuery := trigramTokens[0]
	for _, t := range trigramTokens[1:] {
		trigramQuery += " OR " + t
	}

	var rowsQuery string
	var args []any
	if minImportance > 0 {
		rowsQuery = `SELECT f.id, fts.rank
		 FROM facts_fts_trigram fts
		 JOIN facts f ON f.id = fts.rowid
		 WHERE facts_fts_trigram MATCH ? AND f.active = 1 AND f.importance >= ?
		 ORDER BY fts.rank
		 LIMIT 30`
		args = []any{trigramQuery, minImportance}
	} else {
		rowsQuery = `SELECT f.id, fts.rank
		 FROM facts_fts_trigram fts
		 JOIN facts f ON f.id = fts.rowid
		 WHERE facts_fts_trigram MATCH ? AND f.active = 1
		 ORDER BY fts.rank
		 LIMIT 30`
		args = []any{trigramQuery}
	}

	rows, err := s.db.QueryContext(ctx, rowsQuery, args...)
	if err != nil {
		return nil
	}
	defer rows.Close()

	results := make(map[int64]float64)
	for rows.Next() {
		var id int64
		var rank float64
		if err := rows.Scan(&id, &rank); err != nil {
			continue
		}
		// Slightly penalize trigram results vs unicode61.
		results[id] = rankToScore(rank) * s.searchParams().TrigramPenalty
	}
	return results
}

// entitySearch returns fact IDs linked to a named entity, with a baseline score.
func (s *Store) entitySearch(ctx context.Context, entityName string) map[int64]float64 {
	entityName = normalizeEntityName(entityName)
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.QueryContext(ctx,
		`SELECT f.id FROM facts f
		 JOIN fact_entities fe ON fe.fact_id = f.id
		 JOIN entities e ON e.id = fe.entity_id
		 WHERE e.name = ? AND f.active = 1
		 LIMIT 30`,
		entityName,
	)
	if err != nil {
		s.logger.Warn("entity search query failed", "entity", entityName, "error", err)
		return nil
	}
	defer rows.Close()

	results := make(map[int64]float64)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			continue
		}
		results[id] = s.searchParams().EntityMatchBaseline
	}
	return results
}

// vectorSearch computes cosine similarity against all active fact embeddings.
func (s *Store) vectorSearch(ctx context.Context, queryVec []float32) (map[int64]float64, error) {
	embeddings, err := s.LoadEmbeddings(ctx)
	if err != nil {
		return nil, err
	}

	results := make(map[int64]float64, len(embeddings))
	for factID, vec := range embeddings {
		sim := cosineSimilarity(queryVec, vec)
		if sim > s.searchParams().VectorMinThreshold {
			results[factID] = sim
		}
	}
	return results, nil
}

// mergeAndRank combines FTS and vector scores with importance and recency.
func (s *Store) mergeAndRank(ftsResults map[int64]float64, vecResults map[int64]float64, opts SearchOpts) []SearchResult {
	// Collect all candidate fact IDs.
	ids := make([]int64, 0, len(ftsResults)+len(vecResults))
	seen := make(map[int64]bool)
	for id := range ftsResults {
		if !seen[id] {
			ids = append(ids, id)
			seen[id] = true
		}
	}
	for id := range vecResults {
		if !seen[id] {
			ids = append(ids, id)
			seen[id] = true
		}
	}

	// Load all candidate facts in one pass (lock held briefly).
	factMap := make(map[int64]*Fact, len(ids))
	s.mu.RLock()
	for _, id := range ids {
		f, err := scanFactRow(s.db.QueryRow(`SELECT * FROM facts WHERE id = ? AND active = 1`, id))
		if err == nil {
			factMap[id] = f
		}
	}
	s.mu.RUnlock()

	now := time.Now()
	p := s.searchParams()
	var results []SearchResult

	for _, id := range ids {
		fact, ok := factMap[id]
		if !ok {
			continue
		}

		ftsScore := ftsResults[id]
		vecScore := 0.0
		if vecResults != nil {
			vecScore = vecResults[id]
		}

		// Hybrid score: max of FTS and vector (or weighted combination if both present).
		var hybridScore float64
		if vecScore > 0 && ftsScore > 0 {
			hybridScore = p.HybridFTSWeight*ftsScore + p.HybridVecWeight*vecScore
		} else {
			hybridScore = math.Max(ftsScore, vecScore)
		}

		// Category-adjusted importance: boost decisions/preferences, attenuate context.
		adjustedImportance := fact.Importance
		if mult, ok := p.CategoryImportanceMultiplier[fact.Category]; ok {
			adjustedImportance = math.Min(1.0, adjustedImportance*mult)
		}

		// Content freshness: use UpdatedAt (last verified/corrected) rather than
		// LastAccessedAt. Access time ≠ content staleness — a stale decision
		// accessed yesterday is still stale.
		refTime := fact.UpdatedAt
		if refTime.IsZero() {
			refTime = fact.CreatedAt
		}
		daysSince := now.Sub(refTime).Hours() / 24

		// Recency scoring: steep initial drop → gradual long tail.
		// Uses inverse-square decay: score = 1 / (1 + (days/steepness)²)
		// For context (steepness=5): day 0→1.0, day 2→0.86, day 5→0.5, day 10→0.2
		steepness := p.DefaultSteepnessDays
		if st, ok := p.CategorySteepnessDays[fact.Category]; ok {
			steepness = st
		}
		ratio := daysSince / steepness
		recencyScore := 1.0 / (1.0 + math.Pow(ratio, p.DecayPower))

		// Verification score: dreaming-verified facts are more trustworthy.
		verificationScore := p.VerificationUnverified
		if fact.VerifiedAt != nil && !fact.VerifiedAt.IsZero() {
			verificationScore = p.VerificationVerified
		}

		finalScore := p.WeightHybrid*hybridScore +
			p.WeightImportance*adjustedImportance +
			p.WeightRecency*recencyScore +
			p.WeightVerification*verificationScore

		if opts.MinScore > 0 && finalScore < opts.MinScore {
			continue
		}

		results = append(results, SearchResult{
			Fact:     *fact,
			Score:    finalScore,
			FTSScore: ftsScore,
			VecScore: vecScore,
		})
	}

	// Sort by final score descending.
	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	if len(results) > opts.Limit {
		results = results[:opts.Limit]
	}

	return results
}

// buildFTSQuery constructs an FTS5 MATCH expression from tokens joined by op ("AND" or "OR").
// Each token is double-quoted to escape FTS5 reserved words (AND, OR, NOT, NEAR).
// Korean tokens use prefix matching (token*) to handle agglutinative particles:
// "배포" matches "배포는", "배포를", etc. in the unicode61 FTS index.
func buildFTSQuery(tokens []string, op string) string {
	if len(tokens) == 0 {
		return ""
	}
	var escaped []string
	for _, t := range tokens {
		t = stripQuotes(t)
		if t == "" {
			continue
		}
		if containsHangul(t) {
			// Korean prefix match: 배포* matches 배포는, 사용자* matches 사용자는.
			// Korean is agglutinative — particles attach to stems at any length,
			// so prefix matching is needed for all Korean tokens.
			escaped = append(escaped, t+"*")
		} else {
			escaped = append(escaped, `"`+t+`"`)
		}
	}
	if len(escaped) == 0 {
		return ""
	}
	result := escaped[0]
	for _, p := range escaped[1:] {
		result += " " + op + " " + p
	}
	return result
}

// escapeFTS escapes special characters for FTS5 MATCH queries (OR mode).
// Kept for backward compatibility with trigram search and other callers.
func escapeFTS(query string) string {
	tokens := splitTokens(query)
	return buildFTSQuery(tokens, "OR")
}

func splitTokens(s string) []string {
	var tokens []string
	for _, part := range splitWhitespace(s) {
		if part != "" {
			tokens = append(tokens, part)
		}
	}
	return tokens
}

func splitWhitespace(s string) []string {
	var result []string
	current := ""
	for _, r := range s {
		if r == ' ' || r == '\t' || r == '\n' {
			if current != "" {
				result = append(result, current)
				current = ""
			}
		} else {
			current += string(r)
		}
	}
	if current != "" {
		result = append(result, current)
	}
	return result
}

func stripQuotes(s string) string {
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return s[1 : len(s)-1]
	}
	return s
}

// rerankFacts reorders memory search results using the cross-encoder reranker.
// On failure, returns the original results unchanged (graceful fallback).
func (s *Store) rerankFacts(ctx context.Context, query string, results []SearchResult) []SearchResult {
	docs := make([]string, len(results))
	for i, r := range results {
		docs[i] = r.Fact.Content
	}

	ranked, err := s.reranker(ctx, query, docs, len(results))
	if err != nil {
		if s.logger != nil {
			s.logger.Debug("memory: reranking failed, using hybrid order", "error", err)
		}
		return results
	}

	p := s.searchParams()
	reranked := make([]SearchResult, 0, len(ranked))
	for _, r := range ranked {
		if r.Index >= 0 && r.Index < len(results) {
			res := results[r.Index]
			// Blend reranker score with existing score to preserve importance/recency signal.
			res.Score = p.RerankBlendReranker*r.RelevanceScore + p.RerankBlendHybrid*res.Score
			reranked = append(reranked, res)
		}
	}

	if len(reranked) == 0 {
		return results
	}
	return reranked
}

// rankToScore converts FTS5 rank (negative, lower = better) to 0-1 score.
func rankToScore(rank float64) float64 {
	if rank >= 0 {
		return 0
	}
	// Use sigmoid-like transform: score = 1 / (1 + exp(rank))
	return 1.0 / (1.0 + math.Exp(rank))
}
