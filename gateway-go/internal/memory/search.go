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
	Limit         int     // max results (default 10)
	Category      string  // filter by category (empty = all)
	MinScore      float64 // minimum final score threshold
	MinImportance float64 // minimum importance to include (0 = all; use 0.7 for FTS-only mode)
	EntityFilter  string  // filter by entity name (empty = all)
}

// SearchResult is a scored fact from a search query.
type SearchResult struct {
	Fact         Fact          `json:"fact"`
	Score        float64       `json:"score"`
	FTSScore     float64       `json:"fts_score,omitempty"`
	VecScore     float64       `json:"vec_score,omitempty"`
	RelatedFacts []RelatedFact `json:"related_facts,omitempty"`
}

// Scoring weights.
const (
	weightHybrid       = 0.50
	weightImportance   = 0.25
	weightRecency      = 0.15
	weightVerification = 0.10
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
	results = dedupResults(results, dedupJaccardThreshold)

	// Phase 4: Cross-encoder reranking (optional).
	if s.reranker != nil && len(results) > 1 {
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

	tokens := splitTokens(query)

	// Stage 1: AND query (all tokens must match) for higher precision.
	andQuery := buildFTSQuery(tokens, "AND")
	results := s.runFTSQuery(ctx, andQuery, opts)

	// Stage 2: OR fallback when AND yields too few results.
	if len(results) < ftsAndMinResults && len(tokens) > 1 {
		orQuery := buildFTSQuery(tokens, "OR")
		orResults := s.runFTSQuery(ctx, orQuery, opts)
		for id, score := range orResults {
			if _, exists := results[id]; !exists {
				// Slightly penalize OR-only results vs AND matches.
				results[id] = score * 0.85
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

// ftsAndMinResults is the minimum number of AND-query results before falling
// back to OR. Below this threshold, AND was too restrictive.
const ftsAndMinResults = 3

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
// minImportance filters out low-importance facts when > 0 (e.g. 0.7 for FTS-only mode).
func (s *Store) trigramSearch(ctx context.Context, query string, minImportance float64) map[int64]float64 {
	var rows_query string
	var args []any
	if minImportance > 0 {
		rows_query = `SELECT f.id, fts.rank
		 FROM facts_fts_trigram fts
		 JOIN facts f ON f.id = fts.rowid
		 WHERE facts_fts_trigram MATCH ? AND f.active = 1 AND f.importance >= ?
		 ORDER BY fts.rank
		 LIMIT 30`
		args = []any{`"` + query + `"`, minImportance}
	} else {
		rows_query = `SELECT f.id, fts.rank
		 FROM facts_fts_trigram fts
		 JOIN facts f ON f.id = fts.rowid
		 WHERE facts_fts_trigram MATCH ? AND f.active = 1
		 ORDER BY fts.rank
		 LIMIT 30`
		args = []any{`"` + query + `"`}
	}

	rows, err := s.db.QueryContext(ctx, rows_query, args...)
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

// entitySearch returns fact IDs linked to a named entity, with a baseline score.
func (s *Store) entitySearch(ctx context.Context, entityName string) map[int64]float64 {
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
		return nil
	}
	defer rows.Close()

	results := make(map[int64]float64)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			continue
		}
		results[id] = 0.6 // baseline score for entity match
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
		if sim > 0.35 { // min threshold — filters out thematically unrelated facts
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
		// Content freshness: use UpdatedAt (last verified/corrected) rather than
		// LastAccessedAt. Access time ≠ content staleness — a stale decision
		// accessed yesterday is still stale.
		refTime := fact.UpdatedAt
		if refTime.IsZero() {
			refTime = fact.CreatedAt
		}
		daysSince := now.Sub(refTime).Hours() / 24
		recencyScore := math.Exp(-math.Ln2 * daysSince / halfLife)

		// Verification score: dreaming-verified facts are more trustworthy.
		// Replaces the old frequency score which amplified noise by boosting
		// frequently accessed (but not necessarily accurate) facts.
		verificationScore := 0.3 // base score for unverified facts
		if fact.VerifiedAt != nil && !fact.VerifiedAt.IsZero() {
			verificationScore = 1.0
		}

		finalScore := weightHybrid*hybridScore +
			weightImportance*adjustedImportance +
			weightRecency*recencyScore +
			weightVerification*verificationScore

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
func buildFTSQuery(tokens []string, op string) string {
	if len(tokens) == 0 {
		return ""
	}
	var escaped []string
	for _, t := range tokens {
		t = stripQuotes(t)
		if t != "" {
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
