package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/embedding"
	"github.com/choiceoh/deneb/gateway-go/internal/vega"
)

// Ground truth types for benchmark dataset.

type benchmarkGroundTruth struct {
	Facts   []benchmarkFact  `json:"facts"`
	Queries []benchmarkQuery `json:"queries"`
}

type benchmarkFact struct {
	Tag        string            `json:"tag"`
	Content    string            `json:"content"`
	Category   string            `json:"category"`
	Importance float64           `json:"importance"`
	DaysAgo    float64           `json:"days_ago"`
	Verified   bool              `json:"verified"`
	Entities   []benchmarkEntity `json:"entities,omitempty"`
}

type benchmarkEntity struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

type benchmarkQuery struct {
	Query        string   `json:"query"`
	ExpectedTags []string `json:"expected_tags"`
	Description  string   `json:"description"`
}

// testdataDir returns the path to the testdata directory relative to this file.
func testdataDir() string {
	_, thisFile, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(thisFile), "testdata")
}

// embedAllFacts generates embeddings for all active facts in the store.
func embedAllFacts(t *testing.T, store *Store, embedder *Embedder) {
	t.Helper()
	ctx := context.Background()

	// Collect all facts first to avoid holding the rows cursor while running
	// embedding queries (SQLite single-connection deadlock).
	type factRow struct {
		id      int64
		content string
	}
	rows, err := store.db.QueryContext(ctx, "SELECT id, content FROM facts WHERE active = 1")
	if err != nil {
		t.Fatalf("query facts for embedding: %v", err)
	}
	var facts []factRow
	for rows.Next() {
		var f factRow
		if err := rows.Scan(&f.id, &f.content); err != nil {
			t.Fatalf("scan fact: %v", err)
		}
		facts = append(facts, f)
	}
	rows.Close()

	count := 0
	for _, f := range facts {
		var cnt int
		store.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM fact_embeddings WHERE fact_id = ?", f.id).Scan(&cnt)
		if cnt > 0 {
			continue
		}
		if err := embedder.EmbedAndStore(ctx, f.id, f.content); err != nil {
			t.Logf("embed fact %d: %v", f.id, err)
		}
		count++
	}
	t.Logf("embedded %d facts", count)
}

// setupVectorAndReranker creates embedder and reranker, embeds all facts.
// Falls back to FTS-only if GEMINI_API_KEY is not set.
func setupVectorAndReranker(t *testing.T, store *Store) func(ctx context.Context, query string, opts SearchOpts) ([]SearchResult, error) {
	t.Helper()

	geminiKey := os.Getenv("GEMINI_API_KEY")
	if geminiKey == "" {
		t.Log("GEMINI_API_KEY not set, running FTS-only")
		return func(ctx context.Context, query string, opts SearchOpts) ([]SearchResult, error) {
			return store.SearchFacts(ctx, query, nil, opts)
		}
	}

	embedder := NewEmbedder(
		embedding.NewGeminiEmbedder(geminiKey, slog.Default()),
		store,
		slog.Default(),
	)

	for attempt := 0; attempt < 3; attempt++ {
		embedAllFacts(t, store, embedder)
	}

	jinaKey := os.Getenv("JINA_API_KEY")
	if jinaKey != "" {
		jinaReranker := vega.NewReranker(vega.RerankConfig{
			APIKey: jinaKey,
			Logger: slog.Default(),
		})
		if jinaReranker != nil {
			store.SetReranker(func(ctx context.Context, query string, docs []string, topN int) ([]RerankResult, error) {
				vr, err := jinaReranker.Rerank(ctx, query, docs, topN)
				if err != nil {
					return nil, err
				}
				result := make([]RerankResult, len(vr))
				for i, r := range vr {
					result[i] = RerankResult{Index: r.Index, RelevanceScore: r.RelevanceScore}
				}
				return result, nil
			})
			t.Log("reranker enabled (Jina)")
		}
	} else {
		t.Log("JINA_API_KEY not set, reranker disabled")
	}

	return func(ctx context.Context, query string, opts SearchOpts) ([]SearchResult, error) {
		vec, err := embedder.EmbedQuery(ctx, query)
		if err != nil {
			t.Logf("embed query failed: %v, falling back to FTS-only", err)
			return store.SearchFacts(ctx, query, nil, opts)
		}
		return store.SearchFacts(ctx, query, vec, opts)
	}
}

// TestSearchBenchmarkMRR measures search quality using MRR@10 and Recall@10.
// Designed for autoresearch: edits testdata/search_params.json, outputs METRIC.
// With GEMINI_API_KEY + JINA_API_KEY: full hybrid search + reranking.
func TestSearchBenchmarkMRR(t *testing.T) {
	ctx := context.Background()
	dir := testdataDir()

	params, err := LoadSearchParams(filepath.Join(dir, "search_params.json"))
	if err != nil {
		t.Fatalf("load search params: %v", err)
	}

	gtData, err := os.ReadFile(filepath.Join(dir, "benchmark_ground_truth.json"))
	if err != nil {
		t.Fatalf("load ground truth: %v", err)
	}
	var gt benchmarkGroundTruth
	if err := json.Unmarshal(gtData, &gt); err != nil {
		t.Fatalf("parse ground truth: %v", err)
	}

	store := tempStore(t)
	store.SetSearchParams(params)

	now := time.Now()
	tagToID := make(map[string]int64, len(gt.Facts))

	for _, f := range gt.Facts {
		id, err := store.InsertFact(ctx, Fact{
			Content:    f.Content,
			Category:   f.Category,
			Importance: f.Importance,
		})
		if err != nil {
			t.Fatalf("insert fact %q: %v", f.Tag, err)
		}
		tagToID[f.Tag] = id

		createdAt := now.Add(-time.Duration(f.DaysAgo*24) * time.Hour)
		ts := createdAt.Format(time.RFC3339)
		if _, err := store.db.ExecContext(ctx,
			`UPDATE facts SET created_at = ?, updated_at = ? WHERE id = ?`,
			ts, ts, id); err != nil {
			t.Fatalf("set timestamp for %q: %v", f.Tag, err)
		}

		if f.Verified {
			verifiedAt := now.Add(-time.Duration(f.DaysAgo*24) * time.Hour)
			vts := verifiedAt.Format(time.RFC3339)
			if _, err := store.db.ExecContext(ctx,
				`UPDATE facts SET verified_at = ? WHERE id = ?`,
				vts, id); err != nil {
				t.Fatalf("set verified_at for %q: %v", f.Tag, err)
			}
		}

		for _, e := range f.Entities {
			eid, err := store.UpsertEntity(ctx, e.Name, e.Type)
			if err != nil {
				t.Fatalf("upsert entity %q: %v", e.Name, err)
			}
			if err := store.LinkFactEntity(ctx, id, eid, "subject"); err != nil {
				t.Fatalf("link entity %q to fact %q: %v", e.Name, f.Tag, err)
			}
		}
	}

	searchFn := setupVectorAndReranker(t, store)

	const k = 10
	var reciprocalRankSum float64
	var recallSum float64
	queryCount := len(gt.Queries)

	for _, q := range gt.Queries {
		results, err := searchFn(ctx, q.Query, SearchOpts{Limit: k})
		if err != nil {
			t.Errorf("query %q failed: %v", q.Query, err)
			continue
		}

		expectedIDs := make(map[int64]bool, len(q.ExpectedTags))
		for _, tag := range q.ExpectedTags {
			if id, ok := tagToID[tag]; ok {
				expectedIDs[id] = true
			} else {
				t.Errorf("query %q references unknown tag %q", q.Query, tag)
			}
		}

		rr := 0.0
		for rank, r := range results {
			if expectedIDs[r.Fact.ID] {
				rr = 1.0 / float64(rank+1)
				break
			}
		}
		reciprocalRankSum += rr

		found := 0
		for _, r := range results {
			if expectedIDs[r.Fact.ID] {
				found++
			}
		}
		if len(expectedIDs) > 0 {
			recallSum += float64(found) / float64(len(expectedIDs))
		}

		t.Logf("  query=%q  rr=%.4f  recall=%.2f/%d  hits=%d  desc=%s",
			q.Query, rr, float64(found), len(expectedIDs), len(results), q.Description)
	}

	mrr := reciprocalRankSum / float64(queryCount)
	recall := recallSum / float64(queryCount)

	combined := 0.7*mrr + 0.3*recall

	fmt.Printf("mrr@%d: %.6f\n", k, mrr)
	fmt.Printf("recall@%d: %.6f\n", k, recall)
	fmt.Printf("METRIC: %.6f\n", combined)
}
