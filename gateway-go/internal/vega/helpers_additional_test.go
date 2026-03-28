package vega

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/llm"
)

func TestBuildExpandedQueryAndFormatForLog(t *testing.T) {
	if got := BuildExpandedQuery("solar", nil); got != "solar" {
		t.Fatalf("expected original query only, got %q", got)
	}
	q := BuildExpandedQuery("solar", []string{"pv", "module"})
	if q != "solar OR pv OR module" {
		t.Fatalf("unexpected combined query: %q", q)
	}
	eq := ExpandedSearchQuery{Original: "solar", Expanded: []string{"pv", "module"}}
	if got := eq.FormatForLog(); got != "solar (+2 terms)" {
		t.Fatalf("unexpected log format: %q", got)
	}
}

func TestCollectStreamText(t *testing.T) {
	events := make(chan llm.StreamEvent, 3)
	events <- llm.StreamEvent{Type: "content_block_delta", Payload: json.RawMessage(`{"delta":{"text":"hello "}}`)}
	events <- llm.StreamEvent{Type: "content_block_delta", Payload: json.RawMessage(`{"delta":{"text":"world"}}`)}
	events <- llm.StreamEvent{Type: "other", Payload: json.RawMessage(`{"noop":true}`)}
	close(events)

	got := collectStreamText(context.Background(), events)
	if got != "hello world" {
		t.Fatalf("expected stream text concat, got %q", got)
	}
}

func TestCollectStreamTextContextDone(t *testing.T) {
	events := make(chan llm.StreamEvent)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got := collectStreamText(ctx, events); got != "" {
		t.Fatalf("expected empty string on canceled context, got %q", got)
	}
}

func TestSearchCachePutGetCopyAndExpiry(t *testing.T) {
	cache := newSearchCache()
	key := searchCacheKey("q", SearchOpts{Limit: 5, Offset: 0, Mode: "hybrid"})
	original := []SearchResult{{ProjectID: 1, Section: "A", Content: "c", Score: 0.1}}
	cache.put(key, original)

	cached, ok := cache.get(key)
	if !ok || len(cached) != 1 {
		t.Fatalf("expected cache hit, got ok=%v results=%v", ok, cached)
	}
	cached[0].Content = "mutated"

	again, ok := cache.get(key)
	if !ok {
		t.Fatal("expected cache hit after mutation")
	}
	if again[0].Content != "c" {
		t.Fatalf("cached value should be immutable copy, got %q", again[0].Content)
	}

	cache.mu.Lock()
	cache.items[key].entry.createdAt = time.Now().Add(-(searchCacheTTL + time.Second))
	cache.mu.Unlock()
	if _, ok := cache.get(key); ok {
		t.Fatal("expected expired cache entry to miss")
	}
}

func TestParseSearchResultsAndMergeResults(t *testing.T) {
	payload := []byte(`{"unified":[{"project_id":10,"project_name":"P","heading":"H","content":"C","score":0.77}]}`)
	results, err := parseSearchResults(payload)
	if err != nil {
		t.Fatalf("parseSearchResults returned error: %v", err)
	}
	if len(results) != 1 || results[0].ProjectID != 10 {
		t.Fatalf("unexpected parsed results: %+v", results)
	}

	_, err = parseSearchResults([]byte(`{"error":"boom","detail":"bad"}`))
	if err == nil {
		t.Fatal("expected parseSearchResults to return wrapped backend error")
	}

	merged := mergeResults(
		[]SearchResult{{ProjectID: 10, Section: "H", Content: "old"}},
		[]SearchResult{{ProjectID: 10, Section: "H", Content: "dup"}, {ProjectID: 20, Section: "N", Content: "new"}},
	)
	if len(merged) != 2 {
		t.Fatalf("expected deduped merge length 2, got %d", len(merged))
	}
}

func TestEnhancedBackendHealthCheckUnconfigured(t *testing.T) {
	eb := &EnhancedBackend{logger: quietLogger()}
	status := eb.HealthCheck(context.Background())
	if len(status.Components) != 3 {
		t.Fatalf("expected 3 components, got %d", len(status.Components))
	}
	for _, c := range status.Components {
		if c.Available {
			t.Fatalf("expected unconfigured component to be unavailable: %+v", c)
		}
	}
}

func TestEnhancedBackendSearchCacheHit(t *testing.T) {
	eb := &EnhancedBackend{
		cache:  newSearchCache(),
		logger: quietLogger(),
	}
	opts := SearchOpts{Limit: 2, Mode: "hybrid"}
	key := searchCacheKey("cached query", opts)
	cached := []SearchResult{{ProjectID: 7, Section: "S", Content: "cached", Score: 1}}
	eb.cache.put(key, cached)

	results, err := eb.Search(context.Background(), "cached query", opts)
	if err != nil {
		t.Fatalf("expected cache-only search success, got error: %v", err)
	}
	if len(results) != 1 || results[0].ProjectID != 7 {
		t.Fatalf("unexpected cache results: %+v", results)
	}
}

func TestEnhancedBackendSearchWithVectorNoFFI(t *testing.T) {
	eb := &EnhancedBackend{logger: quietLogger()}
	_, err := eb.searchWithVector(context.Background(), "query", []float32{0.1, 0.2}, SearchOpts{})
	if err == nil {
		t.Fatal("expected parse error from no_ffi response shape")
	}
}

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
