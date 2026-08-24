// web_rerank.go — Reorder search hits by what the query actually asked.
//
// A search provider ranks for its own audience: popularity, freshness, SEO. The
// agent's question is narrower than the query string it managed to write, and
// the answer is often the fourth hit. The reranker that already serves wiki
// recall and code search scores query-against-document directly, so pointing it
// at web results costs one call to the resident sidecar and moves the useful
// page to the top — which matters most in search+fetch, where only the first N
// results are actually fetched.
//
// Fails open in every direction: no sidecar, a busy sidecar, a slow one, or a
// short result list all return the provider's own order. Search must never wait
// on an optional ranking pass.
package web

import (
	"context"
	"log/slog"
	"sort"
	"strings"
	"sync"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/rerank"
)

// minRerankResults is the point below which reordering cannot change anything
// worth a network call.
const minRerankResults = 3

var (
	searchRerankMu sync.RWMutex
	searchReranker *rerank.Client
)

// SetSearchReranker wires the shared ranking sidecar into web search. Nil (the
// default) leaves provider order untouched.
func SetSearchReranker(c *rerank.Client) {
	searchRerankMu.Lock()
	defer searchRerankMu.Unlock()
	searchReranker = c
}

func currentSearchReranker() *rerank.Client {
	searchRerankMu.RLock()
	defer searchRerankMu.RUnlock()
	return searchReranker
}

// rerankSearchResults returns results ordered by relevance to query, or the
// input unchanged when ranking is unavailable or not worth doing.
func rerankSearchResults(ctx context.Context, query string, results []searchResult) []searchResult {
	client := currentSearchReranker()
	if client == nil || len(results) < minRerankResults || strings.TrimSpace(query) == "" {
		return results
	}

	docs := make([]string, len(results))
	for i, r := range results {
		docs[i] = searchRerankDocument(r)
	}
	scores, err := client.Rerank(ctx, query, docs)
	if err != nil || len(scores) != len(results) {
		// Includes rerank.ErrBusy: another optional rerank holds the sidecar, and
		// queueing behind a GPU request would spend the turn's deadline.
		slog.Debug("web search rerank skipped", "query", query, "results", len(results), "error", err)
		return results
	}

	// Scores travel WITH their result rather than through a URL lookup: two hits
	// can share a URL (or carry none), and a map would then rank one of them by
	// the other's score.
	type ranked struct {
		result searchResult
		score  float64
	}
	pairs := make([]ranked, len(results))
	for i, r := range results {
		pairs[i] = ranked{result: r, score: scores[i]}
	}
	// Stable: equal scores keep the provider's order, which is a real signal.
	sort.SliceStable(pairs, func(a, b int) bool { return pairs[a].score > pairs[b].score })

	ordered := make([]searchResult, len(pairs))
	for i, p := range pairs {
		ordered[i] = p.result
	}
	return ordered
}

// searchRerankDocument is what the model scores against the query. Title first
// because a result's title is the strongest statement of what the page is;
// the snippet is supporting evidence.
func searchRerankDocument(r searchResult) string {
	title := strings.TrimSpace(r.Title)
	desc := strings.TrimSpace(r.Description)
	switch {
	case title != "" && desc != "":
		return title + "\n" + desc
	case title != "":
		return title
	default:
		return desc
	}
}
