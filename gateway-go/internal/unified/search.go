// Unified search across all memory tiers (messages, summaries, facts).
//
// Uses the memory_index + memory_fts tables to provide a single search
// interface. Results are ranked by a combination of FTS relevance,
// importance, recency, and tier boost.
package unified

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
)

// SearchResult holds a single result from unified search.
type SearchResult struct {
	ItemType   string  `json:"itemType"`   // "message", "summary", "fact"
	ItemID     string  `json:"itemId"`     // source table PK
	Tier       string  `json:"tier"`       // "short", "medium", "long"
	Content    string  `json:"content"`    // text content
	Importance float64 `json:"importance"` // 0.0-1.0
	Score      float64 `json:"score"`      // final ranked score
	CreatedAt  int64   `json:"createdAt"`  // epoch ms
}

// SearchOpts configures a unified search query.
type SearchOpts struct {
	Limit    int      // max results (default 10)
	Tiers    []string // filter by tier; empty = all
	MinScore float64  // minimum final score threshold
}

// Search performs a unified full-text search across all memory tiers.
// Results are ranked by FTS relevance, importance, recency, and tier boost.
func (s *Store) Search(ctx context.Context, query string, opts SearchOpts) ([]SearchResult, error) {
	if opts.Limit <= 0 {
		opts.Limit = 10
	}
	if query == "" {
		return nil, nil
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	// Escape FTS5 special characters for safe MATCH.
	escaped := escapeFTS(query)

	// Query memory_fts joined with memory_index and source tables.
	// Use rank to get FTS relevance score.
	sqlQuery := `
		SELECT mi.id, mi.item_type, mi.item_id, mi.tier, mi.importance, mi.created_at,
		       memory_fts.rank
		FROM memory_fts
		JOIN memory_index mi ON mi.id = memory_fts.rowid
		WHERE memory_fts MATCH ?
	`
	args := []any{escaped}

	if len(opts.Tiers) > 0 {
		placeholders := make([]string, len(opts.Tiers))
		for i, t := range opts.Tiers {
			placeholders[i] = "?"
			args = append(args, t)
		}
		sqlQuery += " AND mi.tier IN (" + strings.Join(placeholders, ",") + ")"
	}

	sqlQuery += " ORDER BY memory_fts.rank LIMIT ?"
	args = append(args, opts.Limit*3) // fetch extra for scoring/filtering

	rows, err := s.db.QueryContext(ctx, sqlQuery, args...)
	if err != nil {
		// FTS MATCH can fail on bad syntax; try trigram fallback.
		return s.trigramSearch(ctx, query, opts)
	}
	defer rows.Close()

	var candidates []searchCandidate
	for rows.Next() {
		var c searchCandidate
		if err := rows.Scan(&c.id, &c.itemType, &c.itemID, &c.tier,
			&c.importance, &c.createdAt, &c.ftsRank); err != nil {
			continue
		}
		candidates = append(candidates, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("unified search row iteration: %w", err)
	}

	// If no FTS results, try trigram (Korean/CJK).
	if len(candidates) == 0 {
		return s.trigramSearch(ctx, query, opts)
	}

	return s.scoreCandidates(ctx, candidates, opts)
}

// Tier1Facts returns high-importance facts (importance >= threshold) in
// specified categories, for always-on injection into the system prompt.
// This is a cheap SQL query with no FTS or embedding needed.
func (s *Store) Tier1Facts(ctx context.Context, threshold float64, categories []string) ([]SearchResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if len(categories) == 0 {
		categories = []string{"decision", "preference", "user_model"}
	}

	placeholders := make([]string, len(categories))
	args := []any{threshold}
	for i, c := range categories {
		placeholders[i] = "?"
		args = append(args, c)
	}

	query := fmt.Sprintf(`
		SELECT CAST(id AS TEXT), content, importance, created_at
		FROM facts
		WHERE active = 1
		  AND importance >= ?
		  AND category IN (%s)
		ORDER BY importance DESC, created_at DESC
		LIMIT 20
	`, strings.Join(placeholders, ","))

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("tier1 facts: %w", err)
	}
	defer rows.Close()

	var results []SearchResult
	for rows.Next() {
		var r SearchResult
		var createdAtStr string
		if err := rows.Scan(&r.ItemID, &r.Content, &r.Importance, &createdAtStr); err != nil {
			continue
		}
		r.ItemType = "fact"
		r.Tier = "long"
		r.Score = r.Importance
		// Parse created_at (facts use TEXT format; legacy rows may use unix ms string).
		if t, err := time.Parse(time.RFC3339Nano, createdAtStr); err == nil {
			r.CreatedAt = t.UnixMilli()
		} else if t, err := time.Parse(time.RFC3339, createdAtStr); err == nil {
			r.CreatedAt = t.UnixMilli()
		} else if ms, err := strconv.ParseInt(createdAtStr, 10, 64); err == nil {
			r.CreatedAt = ms
		}
		results = append(results, r)
	}
	return results, nil
}

// ── Internal scoring ────────────────────────────────────────────────────────

type searchCandidate struct {
	id         int64
	itemType   string
	itemID     string
	tier       string
	importance float64
	createdAt  int64
	ftsRank    float64
}

func (s *Store) scoreCandidates(ctx context.Context, candidates []searchCandidate, opts SearchOpts) ([]SearchResult, error) {
	nowMs := time.Now().UnixMilli()

	type scored struct {
		candidate searchCandidate
		score     float64
		content   string
	}
	var results []scored

	for _, c := range candidates {
		// FTS rank is negative (lower = better). Convert to 0-1 score.
		ftsScore := 1.0 / (1.0 + math.Exp(c.ftsRank))

		// Recency: exponential decay with tier-specific half-lives.
		daysSince := float64(nowMs-c.createdAt) / (1000 * 60 * 60 * 24)
		halfLife := tierHalfLife(c.tier)
		recency := math.Exp(-math.Ln2 * daysSince / halfLife)

		// Tier boost: long-term memories are more trusted.
		tierBoost := tierBoostWeight(c.tier)

		// Final score: weighted combination.
		score := 0.40*ftsScore + 0.25*c.importance + 0.20*recency + 0.15*tierBoost

		if score < opts.MinScore {
			continue
		}

		// Fetch content from source table.
		content := s.fetchContent(c.itemType, c.itemID)
		if content == "" {
			continue
		}

		results = append(results, scored{
			candidate: c,
			score:     score,
			content:   content,
		})
	}

	// Sort by score descending.
	sort.Slice(results, func(i, j int) bool {
		return results[i].score > results[j].score
	})

	// Limit results.
	if len(results) > opts.Limit {
		results = results[:opts.Limit]
	}

	out := make([]SearchResult, len(results))
	for i, r := range results {
		out[i] = SearchResult{
			ItemType:   r.candidate.itemType,
			ItemID:     r.candidate.itemID,
			Tier:       r.candidate.tier,
			Content:    r.content,
			Importance: r.candidate.importance,
			Score:      r.score,
			CreatedAt:  r.candidate.createdAt,
		}
	}
	return out, nil
}

func (s *Store) trigramSearch(ctx context.Context, query string, opts SearchOpts) ([]SearchResult, error) {
	escaped := escapeFTS(query)

	sqlQuery := `
		SELECT mi.id, mi.item_type, mi.item_id, mi.tier, mi.importance, mi.created_at,
		       memory_fts_trigram.rank
		FROM memory_fts_trigram
		JOIN memory_index mi ON mi.id = memory_fts_trigram.rowid
		WHERE memory_fts_trigram MATCH ?
	`
	args := []any{escaped}

	if len(opts.Tiers) > 0 {
		placeholders := make([]string, len(opts.Tiers))
		for i, t := range opts.Tiers {
			placeholders[i] = "?"
			args = append(args, t)
		}
		sqlQuery += " AND mi.tier IN (" + strings.Join(placeholders, ",") + ")"
	}

	sqlQuery += " ORDER BY memory_fts_trigram.rank LIMIT ?"
	args = append(args, opts.Limit*3)

	rows, err := s.db.QueryContext(ctx, sqlQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("trigram search query failed: %w", err)
	}
	defer rows.Close()

	var candidates []searchCandidate
	for rows.Next() {
		var c searchCandidate
		if err := rows.Scan(&c.id, &c.itemType, &c.itemID, &c.tier,
			&c.importance, &c.createdAt, &c.ftsRank); err != nil {
			continue
		}
		candidates = append(candidates, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("trigram search row iteration: %w", err)
	}

	return s.scoreCandidates(ctx, candidates, opts)
}

func (s *Store) fetchContent(itemType, itemID string) string {
	var content string
	switch itemType {
	case "message":
		s.db.QueryRow(`SELECT content FROM messages WHERE message_id = CAST(? AS INTEGER)`, itemID).Scan(&content)
	case "summary":
		s.db.QueryRow(`SELECT COALESCE(narrative, content) FROM summaries WHERE summary_id = ?`, itemID).Scan(&content)
	case "fact":
		s.db.QueryRow(`SELECT content FROM facts WHERE id = CAST(? AS INTEGER) AND active = 1`, itemID).Scan(&content)
	}
	return content
}

func tierHalfLife(tier string) float64 {
	switch tier {
	case "long":
		return 90.0 // facts: 90-day half-life
	case "medium":
		return 30.0 // summaries: 30-day half-life
	default:
		return 3.0 // messages: 3-day half-life
	}
}

func tierBoostWeight(tier string) float64 {
	switch tier {
	case "long":
		return 1.0 // facts are most trusted
	case "medium":
		return 0.6 // summaries are moderately trusted
	default:
		return 0.3 // messages are least trusted (unvetted)
	}
}

// escapeFTS escapes special FTS5 characters for safe MATCH queries.
func escapeFTS(query string) string {
	// Wrap each token in quotes to avoid FTS5 syntax errors.
	tokens := strings.Fields(query)
	if len(tokens) == 0 {
		return query
	}
	escaped := make([]string, len(tokens))
	for i, t := range tokens {
		// Remove existing quotes and re-wrap.
		t = strings.ReplaceAll(t, `"`, `""`)
		escaped[i] = `"` + t + `"`
	}
	return strings.Join(escaped, " ")
}
