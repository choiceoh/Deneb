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
	ID       string `json:"id"`
	Category string `json:"category"`
	Question string `json:"question"`
	// GoldPaths scores RETRIEVAL: did the block carry the right page.
	GoldPaths []string `json:"gold_paths"`
	// MustContain scores the ANSWER, and until now nothing read it. The gold
	// set has carried these tokens since it was written; the probe parsed them
	// away, so the wiki plane measured only whether the page was found and
	// never whether the model then answered from it. Exported for the reader
	// stage (scripts/eval/wiki_reader_eval.py).
	MustContain []string `json:"must_contain"`
	MustNot     []string `json:"must_not"`
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

	// DENEB_PROBE_EXPORT freezes every case's rendered production block so a
	// reader stage can score ANSWERS over the exact retrieval this run
	// measured — the same reader-separation the session bench uses.
	var exportFile *os.File
	if path := strings.TrimSpace(os.Getenv("DENEB_PROBE_EXPORT")); path != "" {
		f, err := os.Create(path)
		if err != nil {
			t.Fatalf("export: %v", err)
		}
		defer f.Close()
		exportFile = f
	}

	// gold-in-block asks whether SOME page under the gold path was rendered.
	// That over-counts: gold_paths are path substrings and a third of them name
	// a project FOLDER, so a sibling page (로그.md) satisfies it while the page
	// holding the answer (대표.md) is absent — measured, 3 of 13 excerpt
	// failures were this. answer-in-block is the honest companion: are the
	// must_contain tokens actually IN the block. Both are reported; optimizing
	// against the path metric alone optimizes against a false pass.
	hit, answerHit, answerable := 0, 0, 0
	durations := make([]time.Duration, 0, len(cases))
	for _, c := range cases {
		start := time.Now()
		block, _ := Build(context.Background(), Params{
			SessionKey:         "client:korean-probe",
			Message:            c.Question,
			FilesToolReachable: true,
		}, Deps{Wiki: store}, logger)
		durations = append(durations, time.Since(start))
		found := false
		for _, want := range c.GoldPaths {
			if strings.Contains(block, want) {
				hit++
				found = true
				break
			}
		}
		if len(c.MustContain) > 0 {
			answerable++
			if blockCarriesTokens(block, c.MustContain) {
				answerHit++
			}
		}
		if exportFile != nil {
			line, _ := json.Marshal(map[string]any{
				"id": c.ID, "category": c.Category, "question": c.Question,
				"gold_paths": c.GoldPaths, "must_contain": c.MustContain,
				"must_not": c.MustNot, "block": block, "gold_in_block": found,
				"answer_in_block": len(c.MustContain) > 0 && blockCarriesTokens(block, c.MustContain),
			})
			_, _ = exportFile.Write(append(line, '\n'))
		}
		// DENEB_PROBE_DUMP_MISSES=<path>: append each miss as JSONL for
		// offline diagnosis — which gold pages the block failed to carry.
		if !found {
			if dump := os.Getenv("DENEB_PROBE_DUMP_MISSES"); dump != "" {
				if f, err := os.OpenFile(dump, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
					rec, _ := json.Marshal(map[string]any{
						"question": c.Question, "gold": c.GoldPaths, "block": block,
					})
					_, _ = f.Write(append(rec, '\n'))
					_ = f.Close()
				}
			}
		}
	}
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	answerPct := 0.0
	if answerable > 0 {
		answerPct = 100 * float64(answerHit) / float64(answerable)
	}
	t.Logf("KOREAN_PROBE n=%d gold-in-block=%.1f%% answer-in-block=%.1f%% (n=%d) median=%v p90=%v max=%v (deadline %v)",
		len(cases), 100*float64(hit)/float64(len(cases)), answerPct, answerable,
		durations[len(durations)/2], durations[(len(durations)*9)/10],
		durations[len(durations)-1], recallPreflightTimeout)
}

// blockCarriesTokens reports whether every must_contain requirement is present
// in the block. "a|b" means either alternative satisfies; comparison folds the
// spacing and separators a token author never meant to be significant (the
// token "1500" has to match "1,500V").
func blockCarriesTokens(block string, mustContain []string) bool {
	norm := func(s string) string {
		var b strings.Builder
		for _, r := range strings.ToLower(s) {
			switch r {
			case ' ', '\t', '\n', ',', '(', ')', '·':
			default:
				b.WriteRune(r)
			}
		}
		return b.String()
	}
	haystack := norm(block)
	for _, group := range mustContain {
		ok := false
		for _, alt := range strings.Split(group, "|") {
			if a := norm(alt); a != "" && strings.Contains(haystack, a) {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	return true
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
