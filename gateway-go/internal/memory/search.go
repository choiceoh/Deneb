// search.go — Importance-weighted FTS search over the structured memory store.
// Combines FTS5 keyword search with importance and recency scoring.
package memory

import (
	"context"
	"math"
	"sort"
	"time"
)

// SearchOpts configures a memory search.
type SearchOpts struct {
	Limit         int      // max results (default 10)
	Category      string   // filter by category (empty = all)
	MinScore      float64  // minimum final score threshold
	MinImportance float64  // minimum importance to include (0 = all; use 0.7 for FTS-only mode)
	EntityFilter  string   // filter by entity name (empty = all)
	ExtraKeywords []string // additional keywords to include in FTS (e.g., from LLM expansion)
}

// SearchResult is a scored fact from a search query.
type SearchResult struct {
	Fact         Fact          `json:"fact"`
	Score        float64       `json:"score"`
	FTSScore     float64       `json:"fts_score,omitempty"`
	RelatedFacts []RelatedFact `json:"related_facts,omitempty"`
}

// SearchFacts performs FTS search over active facts with importance and recency weighting.
func (s *Store) SearchFacts(ctx context.Context, query string, opts SearchOpts) ([]SearchResult, error) {
	if opts.Limit <= 0 {
		opts.Limit = 10
	}

	// Phase 1: FTS search.
	ftsResults, err := s.ftsSearch(ctx, query, opts)
	if err != nil {
		return nil, err
	}

	// Phase 1.5: Entity-based search enrichment.
	if opts.EntityFilter != "" {
		entityFacts := s.entitySearch(ctx, opts.EntityFilter)
		for id, score := range entityFacts {
			if _, exists := ftsResults[id]; !exists {
				ftsResults[id] = score
			}
		}
	}

	// Phase 2: Score and rank.
	mergeOpts := opts
	mergeOpts.Limit = opts.Limit * 3
	results := s.scoreAndRank(ftsResults, mergeOpts)

	// Phase 2.5: Content deduplication.
	results = dedupResults(results, s.searchParams().DedupJaccardThreshold)

	// Final truncation.
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

// scoreAndRank scores FTS results with importance and recency weighting.
func (s *Store) scoreAndRank(ftsResults map[int64]float64, opts SearchOpts) []SearchResult {
	ids := make([]int64, 0, len(ftsResults))
	for id := range ftsResults {
		ids = append(ids, id)
	}

	// Load all candidate facts in one pass.
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

		// Category-adjusted importance.
		adjustedImportance := fact.Importance
		if mult, ok := p.CategoryImportanceMultiplier[fact.Category]; ok {
			adjustedImportance = math.Min(1.0, adjustedImportance*mult)
		}

		// Recency scoring via inverse-power decay.
		refTime := fact.UpdatedAt
		if refTime.IsZero() {
			refTime = fact.CreatedAt
		}
		daysSince := now.Sub(refTime).Hours() / 24
		steepness := p.DefaultSteepnessDays
		if st, ok := p.CategorySteepnessDays[fact.Category]; ok {
			steepness = st
		}
		ratio := daysSince / steepness
		recencyScore := 1.0 / (1.0 + math.Pow(ratio, p.DecayPower))

		// Verification score.
		verificationScore := p.VerificationUnverified
		if fact.VerifiedAt != nil && !fact.VerifiedAt.IsZero() {
			verificationScore = p.VerificationVerified
		}

		finalScore := p.WeightFTS*ftsScore +
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
		})
	}

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

// rankToScore converts FTS5 rank (negative, lower = better) to 0-1 score.
func rankToScore(rank float64) float64 {
	if rank >= 0 {
		return 0
	}
	// Use sigmoid-like transform: score = 1 / (1 + exp(rank))
	return 1.0 / (1.0 + math.Exp(rank))
}
