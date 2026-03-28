// search.go — Importance-weighted hybrid search over the structured memory store.
// Combines FTS5 keyword search + cosine similarity with importance and recency scoring.
package memory

import (
	"context"
	"math"
	"sort"
	"time"
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
	Limit    int     // max results (default 10)
	Category string  // filter by category (empty = all)
	MinScore float64 // minimum final score threshold
}

// SearchResult is a scored fact from a search query.
type SearchResult struct {
	Fact     Fact    `json:"fact"`
	Score    float64 `json:"score"`
	FTSScore float64 `json:"fts_score,omitempty"`
	VecScore float64 `json:"vec_score,omitempty"`
}

// Scoring weights.
const (
	weightHybrid     = 0.45
	weightImportance = 0.25
	weightRecency    = 0.15
	weightFrequency  = 0.15
)

// categoryImportanceMultiplier adjusts the importance weight by fact category.
// Decisions and preferences constrain future behavior → boost.
// Context is time-sensitive → attenuate.
var categoryImportanceMultiplier = map[string]float64{
	CategoryDecision:   1.20,
	CategoryPreference: 1.10,
	CategorySolution:   1.00,
	CategoryContext:    0.80,
	CategoryUserModel:  1.15,
	CategoryMutual:     0.90,
}

// categoryHalfLifeDays controls recency decay speed per category.
// Long-lived categories (user_model, decision) decay slowly;
// ephemeral categories (context) decay fast.
var categoryHalfLifeDays = map[string]float64{
	CategoryDecision:   90.0,
	CategoryPreference: 60.0,
	CategorySolution:   45.0,
	CategoryContext:    14.0,
	CategoryUserModel:  120.0,
	CategoryMutual:     60.0,
}

const defaultHalfLifeDays = 30.0

// SearchFacts performs a hybrid FTS + semantic search over active facts,
// then applies importance and recency weighting.
func (s *Store) SearchFacts(ctx context.Context, query string, queryVec []float32, opts SearchOpts) ([]SearchResult, error) {
	if opts.Limit <= 0 {
		opts.Limit = 10
	}

	// Phase 1: FTS search.
	ftsResults, err := s.ftsSearch(ctx, query, opts.Category)
	if err != nil {
		return nil, err
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

	// Phase 3: Merge and score.
	results := s.mergeAndRank(ftsResults, vecResults, opts)

	// Phase 4: Cross-encoder reranking (optional).
	if s.reranker != nil && len(results) > 1 {
		results = s.rerankFacts(ctx, query, results)
	}

	return results, nil
}

// ftsSearch performs FTS5 search and returns scored fact IDs.
func (s *Store) ftsSearch(ctx context.Context, query string, category string) (map[int64]float64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var rows_query string
	var args []any

	if category != "" {
		rows_query = `SELECT f.id, fts.rank
			FROM facts_fts fts
			JOIN facts f ON f.id = fts.rowid
			WHERE facts_fts MATCH ? AND f.active = 1 AND f.category = ?
			ORDER BY fts.rank
			LIMIT 50`
		args = []any{escapeFTS(query), category}
	} else {
		rows_query = `SELECT f.id, fts.rank
			FROM facts_fts fts
			JOIN facts f ON f.id = fts.rowid
			WHERE facts_fts MATCH ? AND f.active = 1
			ORDER BY fts.rank
			LIMIT 50`
		args = []any{escapeFTS(query)}
	}

	rows, err := s.db.QueryContext(ctx, rows_query, args...)
	if err != nil {
		// FTS match can fail on malformed queries; return empty rather than error.
		return make(map[int64]float64), nil
	}
	defer rows.Close()

	results := make(map[int64]float64)
	for rows.Next() {
		var id int64
		var rank float64
		if err := rows.Scan(&id, &rank); err != nil {
			continue
		}
		// FTS5 rank is negative (lower = better). Normalize to 0-1.
		results[id] = rankToScore(rank)
	}

	// Korean/CJK fallback: if unicode61 found nothing, try trigram index.
	if len(results) == 0 {
		trigramResults := s.trigramSearch(ctx, query)
		for id, score := range trigramResults {
			results[id] = score
		}
	}

	return results, nil
}

// trigramSearch uses the trigram FTS5 index for CJK/Korean substring matching.
func (s *Store) trigramSearch(ctx context.Context, query string) map[int64]float64 {
	rows, err := s.db.QueryContext(ctx,
		`SELECT f.id, fts.rank
		 FROM facts_fts_trigram fts
		 JOIN facts f ON f.id = fts.rowid
		 WHERE facts_fts_trigram MATCH ? AND f.active = 1
		 ORDER BY fts.rank
		 LIMIT 30`,
		`"`+query+`"`,
	)
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
		results[id] = rankToScore(rank) * 0.8
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
		if sim > 0.20 { // min threshold (lowered to catch thematically related facts)
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
	var results []SearchResult

	// Find max access count across candidates for frequency normalization.
	maxAccessCount := 1
	for _, f := range factMap {
		if f.AccessCount > maxAccessCount {
			maxAccessCount = f.AccessCount
		}
	}
	logMaxAccess := math.Log2(1 + float64(maxAccessCount))

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
			hybridScore = 0.4*ftsScore + 0.6*vecScore
		} else {
			hybridScore = math.Max(ftsScore, vecScore)
		}

		// Category-adjusted importance: boost decisions/preferences, attenuate context.
		adjustedImportance := fact.Importance
		if mult, ok := categoryImportanceMultiplier[fact.Category]; ok {
			adjustedImportance = math.Min(1.0, adjustedImportance*mult)
		}

		// Category-adaptive recency: different half-lives per category.
		halfLife := defaultHalfLifeDays
		if hl, ok := categoryHalfLifeDays[fact.Category]; ok {
			halfLife = hl
		}
		refTime := fact.CreatedAt
		if fact.LastAccessedAt != nil {
			refTime = *fact.LastAccessedAt
		}
		daysSince := now.Sub(refTime).Hours() / 24
		recencyScore := math.Exp(-math.Ln2 * daysSince / halfLife)

		// Frequency score: logarithmic scaling of access count.
		frequencyScore := math.Log2(1+float64(fact.AccessCount)) / logMaxAccess

		finalScore := weightHybrid*hybridScore +
			weightImportance*adjustedImportance +
			weightRecency*recencyScore +
			weightFrequency*frequencyScore

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

// escapeFTS escapes special characters for FTS5 MATCH queries.
func escapeFTS(query string) string {
	// FTS5 reserved: AND, OR, NOT, NEAR, double quotes.
	// For safety, wrap each token in double quotes.
	tokens := splitTokens(query)
	if len(tokens) == 0 {
		return query
	}
	var escaped []string
	for _, t := range tokens {
		// Strip existing quotes and re-wrap.
		t = stripQuotes(t)
		if t != "" {
			escaped = append(escaped, `"`+t+`"`)
		}
	}
	if len(escaped) == 0 {
		return `"` + query + `"`
	}
	return joinOr(escaped)
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

func joinOr(parts []string) string {
	result := parts[0]
	for _, p := range parts[1:] {
		result += " OR " + p
	}
	return result
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

	reranked := make([]SearchResult, 0, len(ranked))
	for _, r := range ranked {
		if r.Index >= 0 && r.Index < len(results) {
			res := results[r.Index]
			// Blend reranker score with existing score to preserve importance/recency signal.
			res.Score = 0.7*r.RelevanceScore + 0.3*res.Score
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
