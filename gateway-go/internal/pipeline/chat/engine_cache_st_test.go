package chat

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSampleEngineCacheSTUsesTokensAndRebaselinesSourceChanges(t *testing.T) {
	hits, queries, reused, prompt := 8, 130, 249600, 423319
	st, publishReuse := true, true
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		engine := "0"
		if st {
			engine = "st"
		}
		fmt.Fprintf(w, "vllm:prefix_cache_hits_total{engine=%q} %d\n", engine, hits)
		fmt.Fprintf(w, "vllm:prefix_cache_queries_total{engine=%q} %d\n", engine, queries)
		fmt.Fprintf(w, "vllm:prompt_tokens_total{engine=%q} %d\n", engine, prompt)
		if st && publishReuse {
			fmt.Fprintf(w, "st:prefix_reused_tokens_total{engine=%q} %d\n", engine, reused)
		}
	}))
	defer srv.Close()
	ctx := context.Background()
	if _, _, ok := sampleEngineCacheDelta(ctx, srv.URL); ok {
		t.Fatal("first ST sample must establish a baseline")
	}
	hits, queries, reused, prompt = 9, 131, 250368, 424319
	if h, q, ok := sampleEngineCacheDelta(ctx, srv.URL); !ok || h != 768 || q != 1000 {
		t.Fatalf("token delta = (%d, %d, %v), want (768, 1000, true)", h, q, ok)
	}
	publishReuse = false
	if _, _, ok := sampleEngineCacheDelta(ctx, srv.URL); ok {
		t.Fatal("ST without token metric must not fall back to request counters")
	}
	// A backend swap at the same URL must not compare counters with different units.
	st, hits, queries = false, 300000, 500000
	if _, _, ok := sampleEngineCacheDelta(ctx, srv.URL); ok {
		t.Fatal("switch to vLLM must establish a new baseline")
	}
	hits, queries = 300500, 501000
	if h, q, ok := sampleEngineCacheDelta(ctx, srv.URL); !ok || h != 500 || q != 1000 {
		t.Fatalf("vLLM compatibility delta = (%d, %d, %v)", h, q, ok)
	}
	st, publishReuse, reused, prompt = true, true, 0, 600000
	if _, _, ok := sampleEngineCacheDelta(ctx, srv.URL); ok {
		t.Fatal("switch back to ST must establish a new baseline")
	}
	prompt += 1000
	if h, q, ok := sampleEngineCacheDelta(ctx, srv.URL); !ok || h != 0 || q != 1000 {
		t.Fatalf("published zero reuse = (%d, %d, %v), want (0, 1000, true)", h, q, ok)
	}
}
