// codesearch — semantic (concept) code search CLI over CodeGraph + Nemotron.
//
//	go run ./cmd/codesearch index [-full]     # build/refresh .codegraph/semantic-code.*
//	go run ./cmd/codesearch query "질의" [-k N]
//	go run ./cmd/codesearch bench             # code-path Hit@K/MRR quality
//
// DENEB_EMBEDDING_URL selects the sidecar (default the gateway's :8002).
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/embedding"
	airerank "github.com/choiceoh/deneb/gateway-go/internal/ai/rerank"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/codesearch"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: codesearch index [-full] | query <text> [-k N] | bench")
		os.Exit(2)
	}
	repo := repoRoot()
	dir := filepath.Join(repo, ".codegraph")

	url := os.Getenv("DENEB_EMBEDDING_URL")
	if url == "" {
		url = "http://127.0.0.1:8002"
	}
	emb := embedding.New(url, slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn})))
	ctx := context.Background()

	switch os.Args[1] {
	case "index":
		full := len(os.Args) > 2 && os.Args[2] == "-full"
		t0 := time.Now()
		embedded, reused, removed, err := codesearch.BuildIndex(
			ctx, repo, dir, emb, "nemotron-3-embed-1b", 2048, full,
			func(s string) { fmt.Fprintln(os.Stderr, s) },
		)
		if err != nil {
			fatal(err)
		}
		fmt.Printf("codesearch index: %d embedded, %d reused, %d dropped in %s\n",
			embedded, reused, removed, time.Since(t0).Round(time.Second))
	case "query":
		if len(os.Args) < 3 {
			fatal(fmt.Errorf("query text required"))
		}
		k := 10
		if len(os.Args) >= 5 && os.Args[3] == "-k" {
			if n, err := strconv.Atoi(os.Args[4]); err == nil {
				k = n
			}
		}
		hits, err := codesearch.SearchRanked(ctx, repo, dir, emb, reranker(), os.Args[2], k)
		if err != nil {
			fatal(err)
		}
		for _, h := range hits {
			fmt.Printf("cos=%.3f fused=%.4f rr=%.3f signals=%d  %-10s %s\n       %s:%d\n",
				h.Cosine, h.Score, h.RerankScore, h.Signals, h.Kind, h.Qualified, h.File, h.StartLine)
		}
	case "bench":
		runBench(ctx, dir, emb)
	default:
		fatal(fmt.Errorf("unknown subcommand %q", os.Args[1]))
	}
}

// reranker wires the production XProvence sidecar: DENEB_RERANK_URL wins,
// otherwise probe the well-known local port so the dev CLI picks it up
// automatically. nil (with a note) when neither answers — Search still works.
func reranker() codesearch.Reranker {
	if c := airerank.NewFromEnv(); c != nil {
		return c
	}
	probe, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	req, _ := http.NewRequestWithContext(probe, http.MethodGet, "http://127.0.0.1:8004/health", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil
	}
	resp.Body.Close()
	if c := airerank.New("http://127.0.0.1:8004", "xprovence-bgem3-v2"); c != nil {
		return c
	}
	return nil
}

func repoRoot() string {
	d, _ := os.Getwd()
	for p := d; ; p = filepath.Dir(p) {
		if _, err := os.Stat(filepath.Join(p, ".codegraph")); err == nil {
			return p
		}
		if p == filepath.Dir(p) {
			return d
		}
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "codesearch:", err)
	os.Exit(1)
}
