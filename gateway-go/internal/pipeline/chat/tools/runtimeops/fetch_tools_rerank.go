package runtimeops

import (
	"context"
	"math"
	"sort"
	"strings"
	"time"
)

const (
	fetchToolRerankCandidateLimit = 10
	fetchToolRerankTimeout        = 800 * time.Millisecond
)

// rerankFetchToolNames reorders only retrieval-admitted candidates. It never
// adds a tool and returns ok=false on every malformed response or service
// failure so callers preserve the lexical/dense order unchanged.
func rerankFetchToolNames(ctx context.Context, query string, names []string, docs []searchDoc, reranker FetchToolReranker) ([]string, bool) {
	if ctx == nil || reranker == nil || len(names) < 2 || strings.TrimSpace(query) == "" {
		return names, false
	}
	count := min(len(names), fetchToolRerankCandidateLimit)
	textByName := make(map[string]string, len(docs))
	for _, doc := range docs {
		textByName[doc.name] = doc.semantic
	}
	documents := make([]string, count)
	for i := range documents {
		text, ok := textByName[names[i]]
		if !ok {
			return names, false
		}
		documents[i] = text
	}

	rankCtx, cancel := context.WithTimeout(ctx, fetchToolRerankTimeout)
	defer cancel()
	scores, err := reranker.Rerank(rankCtx, query, documents)
	if err != nil || len(scores) != len(documents) {
		return names, false
	}
	for _, score := range scores {
		if math.IsNaN(score) || math.IsInf(score, 0) {
			return names, false
		}
	}

	order := make([]int, count)
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(i, j int) bool {
		return scores[order[i]] > scores[order[j]]
	})
	out := append([]string(nil), names...)
	for rank, index := range order {
		out[rank] = names[index]
	}
	return out, true
}
