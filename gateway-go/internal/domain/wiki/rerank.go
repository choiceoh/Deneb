package wiki

import (
	"context"
	"os"
	"sort"
	"strings"
	"time"
)

const (
	rerankCandidateLimit = 10
	rerankTimeout        = 800 * time.Millisecond
	// rerankDocBodyChars bounds the page-body head included in each rerank
	// document. 600 chars is what the offline validation scored (2026-07-19,
	// merged wiki-qa gold 177 cases: P@1 83.6→87.0 with xprovence): enough for
	// the lead facts a cross-encoder needs, small enough that 10 docs stay one
	// fast batch.
	rerankDocBodyChars = 600
)

// rerankForced reports whether DENEB_RERANK_FORCE=on. The default ambiguity
// trigger (shouldModelRerank) fired on only 6% of the gold queries — RRF's
// top1-top2 gap is usually wide even when the top-1 is wrong — capturing +1.1pp
// of the +3.4pp a rerank-always policy measured. The env knob lets production
// opt into rerank-always without a rebuild; keep default off so a configured
// reranker alone does not change latency behavior until explicitly asked.
func rerankForced() bool {
	return os.Getenv("DENEB_RERANK_FORCE") == "on"
}

// Reranker is an optional dedicated cross-encoder/ranking service. Search is
// fully functional without it and falls back unchanged on every error.
type Reranker interface {
	Rerank(ctx context.Context, query string, documents []string) ([]float64, error)
	Identity() string
}

type RerankDiagnostics struct {
	Eligible   bool   `json:"eligible"`
	Attempted  bool   `json:"attempted"`
	Applied    bool   `json:"applied"`
	Candidates int    `json:"candidates,omitempty"`
	Identity   string `json:"identity,omitempty"`
	Reason     string `json:"reason,omitempty"`
	LatencyMS  int64  `json:"latencyMs,omitempty"`
}

func (s *Store) SetReranker(reranker Reranker) {
	if s != nil {
		s.reranker = reranker
	}
}

func shouldModelRerank(results []SearchResult, force bool) bool {
	if len(results) < 2 {
		return false
	}
	if force || rerankForced() {
		return true
	}
	return results[0].Score-results[1].Score < intentAmbiguousGap || results[0].Score < intentWeakTopScore
}

// rerankDocument builds the text the cross-encoder scores for one hit: page
// path + title + summary + body head, falling back to the retrieval snippet
// when the page cannot be read (diary hits, deleted files). The page head beats
// the BM25/semantic snippet because the snippet is biased toward the retrieval
// query's own terms — exactly the signal the reranker is supposed to second-
// guess — while title/summary/lead state what the page IS about.
func (s *Store) rerankDocument(result SearchResult) string {
	if page, err := s.ReadPage(result.Path); err == nil && page != nil {
		body := strings.Join(strings.Fields(page.Body), " ")
		if runes := []rune(body); len(runes) > rerankDocBodyChars {
			body = string(runes[:rerankDocBodyChars])
		}
		parts := []string{result.Path, page.Meta.Title, page.Meta.Summary, body}
		return strings.TrimSpace(strings.Join(parts, "\n"))
	}
	return strings.TrimSpace(strings.Join(append(append([]string(nil), result.Context...), result.Content), "\n"))
}

func (s *Store) applyModelRerank(ctx context.Context, query string, results []SearchResult, force bool) (map[string]float64, map[string]float64, RerankDiagnostics) {
	scores := make(map[string]float64)
	weights := make(map[string]float64)
	diagnostics := RerankDiagnostics{Eligible: shouldModelRerank(results, force)}
	if !diagnostics.Eligible {
		diagnostics.Reason = "strong_retrieval_signal"
		return scores, weights, diagnostics
	}
	if s == nil || s.reranker == nil {
		diagnostics.Reason = "not_configured"
		return scores, weights, diagnostics
	}
	query = strings.TrimSpace(query)
	if query == "" {
		diagnostics.Reason = "empty_query"
		return scores, weights, diagnostics
	}
	count := min(len(results), rerankCandidateLimit)
	documents := make([]string, count)
	for i := range documents {
		documents[i] = s.rerankDocument(results[i])
	}
	diagnostics.Attempted = true
	diagnostics.Candidates = count
	diagnostics.Identity = s.reranker.Identity()
	started := time.Now()
	rankCtx, cancel := context.WithTimeout(ctx, rerankTimeout)
	defer cancel()
	ranked, err := s.reranker.Rerank(rankCtx, query, documents)
	diagnostics.LatencyMS = time.Since(started).Milliseconds()
	if err != nil {
		diagnostics.Reason = "service_error"
		return scores, weights, diagnostics
	}
	if len(ranked) != len(documents) {
		diagnostics.Reason = "empty_response"
		return scores, weights, diagnostics
	}
	order := make([]int, len(ranked))
	for i, score := range ranked {
		order[i] = i
		scores[results[i].Path] = score
	}
	sort.SliceStable(order, func(i, j int) bool {
		if ranked[order[i]] != ranked[order[j]] {
			return ranked[order[i]] > ranked[order[j]]
		}
		return results[order[i]].Path < results[order[j]].Path
	})
	ranks := make(map[int]int, len(order))
	for rank, index := range order {
		ranks[index] = rank
	}
	maxRetrieval := results[0].Score
	if maxRetrieval <= 0 {
		maxRetrieval = 1
	}
	for i := 0; i < count; i++ {
		rank, ok := ranks[i]
		if !ok {
			continue
		}
		retrievalWeight := 0.4
		switch {
		case i < 3:
			retrievalWeight = 0.75
		case i < 10:
			retrievalWeight = 0.60
		}
		// Blend by reranker rank rather than raw score. Some servers return
		// already-sigmoided/compressed values; applying another score transform
		// destroys separation, while ordinal rank is calibration-independent.
		rerankRankScore := 1.0
		if len(order) > 1 {
			rerankRankScore = 1 - float64(rank)/float64(len(order)-1)
		}
		results[i].Score = retrievalWeight*(results[i].Score/maxRetrieval) + (1-retrievalWeight)*rerankRankScore
		weights[results[i].Path] = retrievalWeight
	}
	// Only reorder the candidate window sent to the reranker. Keeping the tail
	// behind that window prevents an untouched retrieval score from interleaving
	// with blended scores that live on a different scale.
	sort.SliceStable(results[:count], func(i, j int) bool {
		if results[i].Score != results[j].Score {
			return results[i].Score > results[j].Score
		}
		return results[i].Path < results[j].Path
	})
	diagnostics.Applied = true
	diagnostics.Reason = "ambiguous_candidates"
	return scores, weights, diagnostics
}

func attachRerankExplanations(results []SearchResult, scores, weights map[string]float64) {
	for i := range results {
		if results[i].Explain == nil {
			continue
		}
		score, ok := scores[results[i].Path]
		if !ok {
			continue
		}
		results[i].Explain.Rerank = &SearchSignalExplanation{BackendScore: score}
		results[i].Explain.RerankWeight = weights[results[i].Path]
		results[i].Explain.FinalScore = results[i].Score
	}
}
