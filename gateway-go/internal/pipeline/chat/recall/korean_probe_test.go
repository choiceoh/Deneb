package recall

// Korean end-to-end probe for the recall preflight.
//
// cmd/recall-bench cannot serve this purpose and its numbers were being read as
// if it could: it feeds gold questions straight to wiki.Store.SearchWithOptions,
// so it never derives queries (searchQueries), never fans out across sources,
// never applies the row or character budget, and never renders the block. It
// measures the wiki SEARCH ENGINE. Anything that lives in recall.Build —
// recallMaxQueries above all, since every source shares that list — is invisible
// to it, and a knob sweep there returns identical numbers whose sameness means
// "not measured", not "no regression".
//
// It also drifts: the same configuration run twice scored r@8 95.9% and 96.4%
// (189 vs 190 of 197), and the day's full spread was 189–192. Differences under
// ~2 cases carry no signal there.
//
// Run manually (env-gated, never in CI):
//
//	DENEB_WIKI_DIR=<copy of ~/.deneb/wiki> DENEB_DIARY_DIR=<copy> \
//	DENEB_WIKI_GOLD=~/.deneb/wiki-qa-gold.jsonl \
//	DENEB_EMBEDDING_URL=http://127.0.0.1:8002 \
//	DENEB_RERANK_URL=http://127.0.0.1:8004 DENEB_RERANK_MODEL=xprovence-bgem3-v2 \
//	go test ./internal/pipeline/chat/recall/ -run TestKoreanRecallProbe -v
//
// Copy the wiki first: the store reconciles its index and warms semantics in
// place, and production must not be mutated by a probe.

import (
	"bufio"
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/embedding"
	airerank "github.com/choiceoh/deneb/gateway-go/internal/ai/rerank"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
)

type koreanGoldCase struct {
	Question  string   `json:"question"`
	GoldPaths []string `json:"gold_paths"`
}

func TestKoreanRecallProbe(t *testing.T) {
	wikiDir := strings.TrimSpace(os.Getenv("DENEB_WIKI_DIR"))
	goldPath := strings.TrimSpace(os.Getenv("DENEB_WIKI_GOLD"))
	if wikiDir == "" || goldPath == "" {
		t.Skip("set DENEB_WIKI_DIR (a COPY of the wiki) and DENEB_WIKI_GOLD")
	}
	store, err := wiki.NewStore(wikiDir, os.Getenv("DENEB_DIARY_DIR"))
	if err != nil {
		t.Fatalf("wiki store: %v", err)
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	embedder := embedding.New("", logger)
	ready := false
	for range 40 {
		if embedder.IsHealthy() {
			ready = true
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	if !ready {
		// Semantic off would measure a different pipeline, quietly.
		t.Skip("embedding server not ready (a go test does not inherit the gateway unit's env; set DENEB_EMBEDDING_URL)")
	}
	store.SetEmbedder(embedder)
	if reranker := airerank.NewFromEnv(); reranker != nil {
		store.SetReranker(reranker)
	}
	if err := store.WarmSemanticIndex(context.Background()); err != nil {
		t.Fatalf("semantic warm: %v — metrics would be lexical-only", err)
	}

	cases, err := loadKoreanGold(goldPath)
	if err != nil {
		t.Fatalf("gold: %v", err)
	}
	if len(cases) == 0 {
		t.Fatal("gold set is empty")
	}

	hit := 0
	durations := make([]time.Duration, 0, len(cases))
	for _, c := range cases {
		start := time.Now()
		block, _ := Build(context.Background(), Params{
			SessionKey:         "client:korean-probe",
			Message:            c.Question,
			FilesToolReachable: true,
		}, Deps{Wiki: store}, logger)
		durations = append(durations, time.Since(start))
		for _, want := range c.GoldPaths {
			if strings.Contains(block, want) {
				hit++
				break
			}
		}
	}
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	t.Logf("KOREAN_PROBE n=%d gold-in-block=%.1f%% median=%v p90=%v max=%v (deadline %v)",
		len(cases), 100*float64(hit)/float64(len(cases)),
		durations[len(durations)/2], durations[(len(durations)*9)/10],
		durations[len(durations)-1], recallPreflightTimeout)
}

func loadKoreanGold(path string) ([]koreanGoldCase, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var cases []koreanGoldCase
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1<<20), 1<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		var c koreanGoldCase
		if err := json.Unmarshal([]byte(line), &c); err != nil || c.Question == "" {
			continue
		}
		cases = append(cases, c)
	}
	return cases, scanner.Err()
}
