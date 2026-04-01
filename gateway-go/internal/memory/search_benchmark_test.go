package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
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

// TestSearchBenchmarkMRR measures search quality using MRR@10 and Recall@10.
// This test is designed to be driven by autoresearch: the agent edits
// testdata/search_params.json, this test reads it, and outputs a scalar metric.
func TestSearchBenchmarkMRR(t *testing.T) {
	ctx := context.Background()
	dir := testdataDir()

	// 1. Load search params.
	params, err := LoadSearchParams(filepath.Join(dir, "search_params.json"))
	if err != nil {
		t.Fatalf("load search params: %v", err)
	}

	// 2. Load ground truth.
	gtData, err := os.ReadFile(filepath.Join(dir, "benchmark_ground_truth.json"))
	if err != nil {
		t.Fatalf("load ground truth: %v", err)
	}
	var gt benchmarkGroundTruth
	if err := json.Unmarshal(gtData, &gt); err != nil {
		t.Fatalf("parse ground truth: %v", err)
	}

	// 3. Create store with params.
	store := tempStore(t)
	store.SetSearchParams(params)

	// 4. Populate facts with controlled timestamps and entities.
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

		// Set timestamps to control recency scoring.
		createdAt := now.Add(-time.Duration(f.DaysAgo*24) * time.Hour)
		ts := createdAt.Format(time.RFC3339)
		if _, err := store.db.ExecContext(ctx,
			`UPDATE facts SET created_at = ?, updated_at = ? WHERE id = ?`,
			ts, ts, id); err != nil {
			t.Fatalf("set timestamp for %q: %v", f.Tag, err)
		}

		// Set verification status.
		if f.Verified {
			verifiedAt := now.Add(-time.Duration(f.DaysAgo*24) * time.Hour)
			vts := verifiedAt.Format(time.RFC3339)
			if _, err := store.db.ExecContext(ctx,
				`UPDATE facts SET verified_at = ? WHERE id = ?`,
				vts, id); err != nil {
				t.Fatalf("set verified_at for %q: %v", f.Tag, err)
			}
		}

		// Create entities and links.
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

	// 5. Run queries and compute metrics.
	const k = 10
	var reciprocalRankSum float64
	var recallSum float64
	queryCount := len(gt.Queries)

	for _, q := range gt.Queries {
		results, err := store.SearchFacts(ctx, q.Query, nil, SearchOpts{Limit: k})
		if err != nil {
			t.Errorf("query %q failed: %v", q.Query, err)
			continue
		}

		// Build expected ID set.
		expectedIDs := make(map[int64]bool, len(q.ExpectedTags))
		for _, tag := range q.ExpectedTags {
			if id, ok := tagToID[tag]; ok {
				expectedIDs[id] = true
			} else {
				t.Errorf("query %q references unknown tag %q", q.Query, tag)
			}
		}

		// MRR: reciprocal rank of first relevant result.
		rr := 0.0
		for rank, r := range results {
			if expectedIDs[r.Fact.ID] {
				rr = 1.0 / float64(rank+1)
				break
			}
		}
		reciprocalRankSum += rr

		// Recall@K: fraction of expected results found in top K.
		found := 0
		for _, r := range results {
			if expectedIDs[r.Fact.ID] {
				found++
			}
		}
		if len(expectedIDs) > 0 {
			recallSum += float64(found) / float64(len(expectedIDs))
		}

		// Per-query detail for debugging.
		t.Logf("  query=%q  rr=%.4f  recall=%.2f/%d  hits=%d  desc=%s",
			q.Query, rr, float64(found), len(expectedIDs), len(results), q.Description)
	}

	mrr := reciprocalRankSum / float64(queryCount)
	recall := recallSum / float64(queryCount)

	// Combined metric: weighted sum (MRR is primary signal).
	combined := 0.7*mrr + 0.3*recall

	// Print metrics in parseable format for autoresearch extraction.
	fmt.Printf("mrr@%d: %.6f\n", k, mrr)
	fmt.Printf("recall@%d: %.6f\n", k, recall)
	fmt.Printf("METRIC: %.6f\n", combined)
}
