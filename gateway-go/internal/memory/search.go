// search.go — Importance-weighted hybrid search over the structured memory store.
// Combines FTS5 keyword search + cosine similarity with importance and recency scoring.
package memory

import (
	"context"
	"math"
	"sort"
	"time"
)

// SearchOpts configures a memory search.
type SearchOpts struct {
	Limit    int      // max results (default 10)
	Category string   // filter by category (empty = all)
	MinScore float64  // minimum final score threshold
}

// SearchResult is a scored fact from a search query.
type SearchResult struct {
	Fact       Fact    `json:"fact"`
	Score      float64 `json:"score"`
	FTSScore   float64 `json:"fts_score,omitempty"`
	VecScore   float64 `json:"vec_score,omitempty"`
}

// Scoring weights.
const (
	weightHybrid  = 0.60
	weightImportance = 0.25
	weightRecency = 0.15
	recencyHalfLifeDays = 30.0
)

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
	return s.mergeAndRank(ftsResults, vecResults, opts), nil
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
	return results, nil
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
		if sim > 0.3 { // min threshold
			results[factID] = sim
		}
	}
	return results, nil
}

// mergeAndRank combines FTS and vector scores with importance and recency.
func (s *Store) mergeAndRank(ftsResults map[int64]float64, vecResults map[int64]float64, opts SearchOpts) []SearchResult {
	// Collect all candidate fact IDs.
	candidates := make(map[int64]bool)
	for id := range ftsResults {
		candidates[id] = true
	}
	for id := range vecResults {
		candidates[id] = true
	}

	// Load facts for scoring.
	s.mu.RLock()
	defer s.mu.RUnlock()

	now := time.Now()
	var results []SearchResult

	for id := range candidates {
		fact, err := scanFactRow(s.db.QueryRow(`SELECT * FROM facts WHERE id = ? AND active = 1`, id))
		if err != nil {
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

		// Recency score based on last access or creation time.
		refTime := fact.CreatedAt
		if fact.LastAccessedAt != nil {
			refTime = *fact.LastAccessedAt
		}
		daysSince := now.Sub(refTime).Hours() / 24
		recencyScore := math.Exp(-math.Ln2 * daysSince / recencyHalfLifeDays)

		finalScore := weightHybrid*hybridScore + weightImportance*fact.Importance + weightRecency*recencyScore

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

// rankToScore converts FTS5 rank (negative, lower = better) to 0-1 score.
func rankToScore(rank float64) float64 {
	if rank >= 0 {
		return 0
	}
	// Use sigmoid-like transform: score = 1 / (1 + exp(rank))
	return 1.0 / (1.0 + math.Exp(rank))
}
