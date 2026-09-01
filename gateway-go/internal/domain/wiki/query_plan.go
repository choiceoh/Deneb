package wiki

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// QueryKind selects the retrieval backend for one typed query clause.
type QueryKind string

const (
	QueryKindLex  QueryKind = "lex"
	QueryKindVec  QueryKind = "vec"
	QueryKindHyDE QueryKind = "hyde"
)

// QueryClause is one independently ranked retrieval expression. The first
// clause defaults to weight 2 (the original-query prior); later clauses default
// to 1. Explicit positive weights override that convention.
type QueryClause struct {
	Kind   QueryKind `json:"kind"`
	Query  string    `json:"query"`
	Weight float64   `json:"weight,omitempty"`
}

// QueryPlan keeps retrieval clauses, intent, and path scopes typed instead of
// encoding them into one ambiguous query string.
type QueryPlan struct {
	Clauses     []QueryClause `json:"clauses"`
	Intent      string        `json:"intent,omitempty"`
	Scopes      []string      `json:"scopes,omitempty"`
	Explain     bool          `json:"explain,omitempty"`
	ForceRerank bool          `json:"forceRerank,omitempty"`
}

type QueryClauseDiagnostic struct {
	Kind       QueryKind `json:"kind"`
	Weight     float64   `json:"weight"`
	Candidates int       `json:"candidates"`
}

// ParseQueryPlan accepts qmd-style line operators. Unknown/non-operator input
// becomes both a lexical and vector clause so ordinary text remains useful.
func ParseQueryPlan(input string) QueryPlan {
	var plan QueryPlan
	plain := make([]string, 0, 1)
	for _, line := range strings.Split(strings.ReplaceAll(input, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			plain = append(plain, line)
			continue
		}
		value = strings.TrimSpace(value)
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "lex", "vec", "hyde":
			if value != "" {
				plan.Clauses = append(plan.Clauses, QueryClause{Kind: QueryKind(strings.ToLower(strings.TrimSpace(key))), Query: value})
			}
		case "intent":
			plan.Intent = value
		case "scope":
			if value != "" {
				plan.Scopes = append(plan.Scopes, value)
			}
		default:
			plain = append(plain, line)
		}
	}
	if len(plain) > 0 {
		query := strings.Join(plain, " ")
		plan.Clauses = append(plan.Clauses, QueryClause{Kind: QueryKindLex, Query: query}, QueryClause{Kind: QueryKindVec, Query: query})
	}
	return plan
}

func normalizeQueryPlan(plan QueryPlan) QueryPlan {
	clauses := make([]QueryClause, 0, len(plan.Clauses))
	for _, clause := range plan.Clauses {
		clause.Query = strings.TrimSpace(clause.Query)
		if clause.Query == "" {
			continue
		}
		switch clause.Kind {
		case QueryKindLex, QueryKindVec, QueryKindHyDE:
		default:
			clause.Kind = QueryKindLex
		}
		if clause.Weight <= 0 {
			clause.Weight = 1
			if len(clauses) == 0 {
				clause.Weight = 2
			}
		}
		clauses = append(clauses, clause)
	}
	plan.Clauses = clauses
	plan.Intent = strings.TrimSpace(plan.Intent)
	plan.Scopes = normalizeScopes(plan.Scopes)
	return plan
}

func normalizeScopes(scopes []string) []string {
	out := make([]string, 0, len(scopes))
	seen := make(map[string]struct{}, len(scopes))
	for _, scope := range scopes {
		scope = strings.Trim(strings.TrimSpace(strings.ReplaceAll(scope, "\\", "/")), "/")
		scope = strings.TrimSuffix(scope, ".md")
		if scope == "" {
			continue
		}
		if _, ok := seen[scope]; ok {
			continue
		}
		seen[scope] = struct{}{}
		out = append(out, scope)
	}
	return out
}

func inQueryScopes(path string, scopes []string) bool {
	if len(scopes) == 0 {
		return true
	}
	path = strings.TrimSuffix(strings.Trim(strings.ReplaceAll(path, "\\", "/"), "/"), ".md")
	for _, scope := range scopes {
		if path == scope || strings.HasPrefix(path, scope+"/") {
			return true
		}
	}
	return false
}

// SearchPlan executes typed clauses independently and combines their ranks
// with weighted RRF. It preserves the existing per-backend admission floors,
// validity demotion, deterministic ties, and graceful semantic fallback.
func (s *Store) SearchPlan(ctx context.Context, plan QueryPlan, limit int) (SearchReport, error) {
	return s.SearchPlanWithOptions(ctx, plan, limit, QueryOptions{})
}

// SearchPlanWithOptions executes a typed plan with caller-specific result-plane
// options. SearchPlan keeps the production all-plane default; page-only
// consumers set ExcludeFactResults so synthetic facts never consume page slots.
func (s *Store) SearchPlanWithOptions(ctx context.Context, plan QueryPlan, limit int, options QueryOptions) (SearchReport, error) {
	plan = normalizeQueryPlan(plan)
	if len(plan.Clauses) == 0 || s == nil || s.fts == nil {
		return SearchReport{}, nil
	}
	if limit <= 0 {
		limit = 10
	}
	fetchLimit := max(limit*3, limit+50)
	rankings := make([][]SearchResult, 0, len(plan.Clauses))
	diagnostics := SearchDiagnostics{Mode: SearchModeFull, Fusion: "weighted-rrf", Scopes: plan.Scopes}
	semanticQueries := make([]string, 0, len(plan.Clauses))
	semanticClauseIndexes := make([]int, 0, len(plan.Clauses))
	for i, clause := range plan.Clauses {
		if clause.Kind == QueryKindVec || clause.Kind == QueryKindHyDE {
			semanticQueries = append(semanticQueries, clause.Query)
			semanticClauseIndexes = append(semanticClauseIndexes, i)
		}
	}
	semanticVectors := make(map[int][]float32, len(semanticClauseIndexes))
	if len(semanticQueries) > 0 {
		if vectors := s.embedQueriesBatch(ctx, semanticQueries); vectors != nil {
			for i, clauseIndex := range semanticClauseIndexes {
				semanticVectors[clauseIndex] = vectors[i]
			}
		}
	}
	for clauseIndex, clause := range plan.Clauses {
		options := QueryOptions{skipMetadata: true, SkipRerank: true, skipValidity: true}
		var report SearchReport
		if clause.Kind == QueryKindVec || clause.Kind == QueryKindHyDE {
			options.Mode = SearchModeSemantic
			semantic := s.searchSemanticWithVec(ctx, semanticVectors[clauseIndex], max(fetchLimit, semanticBlendK))
			report = s.composeSearchReport(ctx, clause.Query, fetchLimit, fetchLimit, nil, semantic, false, options, nil, nil)
		} else {
			options.Mode = SearchModeBM25
			var err error
			report, err = s.SearchWithOptions(ctx, clause.Query, fetchLimit, options)
			if err != nil {
				return SearchReport{}, fmt.Errorf("wiki query plan %s: %w", clause.Kind, err)
			}
		}
		filtered := report.Results[:0]
		for _, result := range report.Results {
			if inQueryScopes(result.Path, plan.Scopes) {
				filtered = append(filtered, result)
			}
		}
		rankings = append(rankings, filtered)
		diagnostics.Clauses = append(diagnostics.Clauses, QueryClauseDiagnostic{
			Kind: clause.Kind, Weight: clause.Weight, Candidates: len(filtered),
		})
		diagnostics.SemanticAvailable = diagnostics.SemanticAvailable || ((clause.Kind == QueryKindVec || clause.Kind == QueryKindHyDE) && len(filtered) > 0)
	}
	results := fuseQueryPlan(rankings, plan.Clauses, fetchLimit)
	diagnostics.CandidateCount = len(results)
	baseScores := resultScoreMap(results)
	results = s.fts.applyValidity(results)
	results = s.applyRecallTRS(results)
	lifecycleQuery := factLifecyclePlanQuery(plan)
	factSnapshot := s.RecallFactSnapshot()
	beforeLifecycle := len(results)
	results = s.filterFactLifecycleSearchResults(lifecycleQuery, results, factSnapshot)
	if dropped := beforeLifecycle - len(results); dropped > 0 {
		diagnostics.Dropped = appendDrop(diagnostics.Dropped, "superseded_fact_evidence", dropped)
	}
	intentResults := []SearchResult(nil)
	if plan.Intent != "" && shouldIntentRerank(results, QueryOptions{Intent: plan.Intent}) {
		intentResults, _ = s.fts.search(ctx, plan.Intent, fetchLimit)
		if len(plan.Scopes) > 0 {
			filtered := intentResults[:0]
			for _, result := range intentResults {
				if inQueryScopes(result.Path, plan.Scopes) {
					filtered = append(filtered, result)
				}
			}
			intentResults = filtered
		}
		intentResults = s.filterFactLifecycleSearchResults(lifecycleQuery, intentResults, factSnapshot)
	}
	bonuses, applied := s.applyIntentRerank(results, intentResults, QueryOptions{Intent: plan.Intent})
	diagnostics.IntentApplied = applied
	if len(results) > 0 {
		s.attachResultMetadata(plan.Clauses[0].Query, results[:min(len(results), rerankCandidateLimit)])
	}
	rerankQuery := plan.Intent
	if rerankQuery == "" {
		rerankQuery = plan.Clauses[0].Query
	}
	rerankScores, rerankWeights, rerankDiagnostics := s.applyModelRerank(ctx, rerankQuery, results, plan.ForceRerank)
	diagnostics.Rerank = rerankDiagnostics
	if !options.ExcludeFactResults {
		factResults := searchActiveFactsForPlan(plan, fetchLimit, factSnapshot.Active)
		results = mergeActiveFactSearchResults(results, factResults, fetchLimit)
		diagnostics.CandidateCount += len(factResults)
		for _, result := range factResults {
			baseScores[result.Path] = result.Score
		}
	}
	results = truncateResults(results, limit)
	// Vocabulary-gap backfill (query_expansion.go): fires only when the primary
	// plan under-fills the limit — a full result list is byte-identical to the
	// pre-expansion pipeline. After truncate (slots are known), before metadata
	// (backfilled hits get context/snippets like any other result).
	results = s.backfillWithExpansion(ctx, rerankQuery, results, limit)
	s.attachResultMetadata(plan.Clauses[0].Query, results)
	diagnostics.ContextExpanded = s.attachLateContext(results)
	latestFactSnapshot := s.RecallFactSnapshot()
	beforeLifecycle = len(results)
	results = s.filterFactLifecycleSearchResults(lifecycleQuery, results, latestFactSnapshot)
	if !options.ExcludeFactResults {
		results = mergeActiveFactSearchResults(
			results,
			searchActiveFactsForPlan(plan, fetchLimit, latestFactSnapshot.Active),
			limit,
		)
	}
	results = truncateResults(results, limit)
	if dropped := beforeLifecycle - len(results); dropped > 0 {
		diagnostics.Dropped = appendDrop(diagnostics.Dropped, "superseded_fact_late_context", dropped)
	}
	diagnostics.ReturnedCount = len(results)
	if plan.Explain {
		s.attachPlanExplanations(results, rankings, plan.Clauses, intentResults, baseScores, bonuses, applied)
		attachRerankExplanations(results, rerankScores, rerankWeights)
		markFactSearchExplanations(results)
	}
	return SearchReport{Results: results, Diagnostics: diagnostics}, nil
}

func factLifecyclePlanQuery(plan QueryPlan) string {
	parts := make([]string, 0, len(plan.Clauses))
	seen := make(map[string]struct{}, len(plan.Clauses))
	for _, clause := range plan.Clauses {
		query := strings.TrimSpace(clause.Query)
		if query == "" {
			continue
		}
		folded := strings.ToLower(strings.Join(strings.Fields(query), " "))
		if _, duplicate := seen[folded]; duplicate {
			continue
		}
		seen[folded] = struct{}{}
		parts = append(parts, query)
	}
	return strings.Join(parts, " ")
}

func searchActiveFactsForPlan(plan QueryPlan, limit int, claims []FactClaim) []SearchResult {
	if len(plan.Scopes) > 0 {
		// Fact claims have no wiki page path to prove they belong to a requested
		// path scope. Exclude them rather than crossing an explicit scope fence.
		return nil
	}
	ambiguities := factSearchAmbiguousSubjectSignatures(claims)
	rankings := make([][]SearchResult, 0, len(plan.Clauses))
	for _, clause := range plan.Clauses {
		rankings = append(rankings, searchActiveFactClaims(clause.Query, limit, claims, ambiguities))
	}
	// Facts participate in the same weighted-RRF admission contract as pages.
	// Intent remains rerank-only and must never admit a new candidate.
	return fuseQueryPlan(rankings, plan.Clauses, limit)
}

func fuseQueryPlan(rankings [][]SearchResult, clauses []QueryClause, limit int) []SearchResult {
	type fused struct {
		result SearchResult
		score  float64
	}
	byPath := make(map[string]*fused)
	totalWeight := 0.0
	for i, ranking := range rankings {
		weight := clauses[i].Weight
		totalWeight += weight
		for rank, result := range ranking {
			item := byPath[result.Path]
			if item == nil {
				resultCopy := result
				item = &fused{result: resultCopy}
				byPath[result.Path] = item
			}
			item.score += weight / (rrfKValue() + float64(rank+1))
			if item.result.Content == "" && result.Content != "" {
				item.result.Content, item.result.Line, item.result.EndLine = result.Content, result.Line, result.EndLine
			}
		}
	}
	if totalWeight == 0 {
		return nil
	}
	scale := 0.8 * (rrfKValue() + 1) / totalWeight
	out := make([]fused, 0, len(byPath))
	for _, item := range byPath {
		item.result.Score = item.score * scale
		out = append(out, *item)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].score != out[j].score {
			return out[i].score > out[j].score
		}
		return out[i].result.Path < out[j].result.Path
	})
	if len(out) > limit {
		out = out[:limit]
	}
	results := make([]SearchResult, len(out))
	for i := range out {
		results[i] = out[i].result
	}
	return results
}

func (s *Store) attachPlanExplanations(results []SearchResult, rankings [][]SearchResult, clauses []QueryClause, intent []SearchResult, base, bonuses map[string]float64, applied bool) {
	rankMaps := make([]map[string]int, len(rankings))
	for i, ranking := range rankings {
		rankMaps[i] = resultRankMap(ranking)
	}
	intentRanks := resultRankMap(intent)
	for i := range results {
		explanation := &SearchExplanation{
			Fusion: "weighted-rrf", BaseScore: base[results[i].Path], IntentBonus: bonuses[results[i].Path],
			ValidityFactor: s.fts.validityFor(results[i].Path), FinalScore: results[i].Score,
		}
		for clauseIndex := range rankings {
			rank := rankMaps[clauseIndex][results[i].Path]
			if rank == 0 {
				continue
			}
			signal := &SearchSignalExplanation{Rank: rank, Contribution: clauses[clauseIndex].Weight / (rrfKValue() + float64(rank))}
			if clauses[clauseIndex].Kind == QueryKindLex && explanation.BM25 == nil {
				explanation.BM25 = signal
			} else if explanation.Semantic == nil {
				explanation.Semantic = signal
			}
		}
		if applied {
			rank := intentRanks[results[i].Path]
			if rank > 0 {
				explanation.Intent = &SearchSignalExplanation{Rank: rank, Contribution: bonuses[results[i].Path]}
			}
		}
		results[i].Explain = explanation
	}
}
