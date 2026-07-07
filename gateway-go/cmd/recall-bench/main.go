// recall-bench — measures wiki retrieval quality (hit@K, MRR) against the
// gold set, read-only, so fusion changes (RRF, graph-boost) can be scored
// before/after on real data without a gateway or any writes to production.
//
// It calls wiki.Store.Search directly — the exact retrieval behind
// miniapp.memory.search (see handlerminiapp.MemorySearcher) — so the number
// here IS what wiki-qa-bench.py's recall mode measures, minus the RPC hop.
//
// Point it at a COPY of the production wiki (SetEmbedder warms the semantic
// cache in-dir). The fusion under test is selected by DENEB_WIKI_FUSION so the
// same binary scores every variant in one run:
//
//	go run ./cmd/recall-bench --wiki /scratch/wiki --diary /scratch/diary \
//	  --gold ~/.deneb/wiki-qa-gold.jsonl --k 8
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/embedding"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
)

type goldCase struct {
	ID        string   `json:"id"`
	Category  string   `json:"category"`
	Question  string   `json:"question"`
	GoldPaths []string `json:"gold_paths"`
}

// pathHit mirrors wiki-qa-bench.py path_hit: gold matches p only from a
// path-segment start (index 0 or right after '/'), so "영덕" never hits
// "남영덕/…" while "비금도-154kv" still hits "비금도-154kv-케이블".
func pathHit(gold, p string) bool {
	p = trimMD(p)
	g := trimMD(gold)
	if g == "" {
		return false
	}
	for start := 0; start <= len(p)-len(g); {
		idx := indexFrom(p, g, start)
		if idx == -1 {
			return false
		}
		if idx == 0 || p[idx-1] == '/' {
			return true
		}
		start = idx + 1
	}
	return false
}

func trimMD(s string) string {
	if len(s) > 3 && s[len(s)-3:] == ".md" {
		return s[:len(s)-3]
	}
	return s
}

func indexFrom(s, sub string, from int) int {
	if from > len(s) {
		return -1
	}
	i := indexOf(s[from:], sub)
	if i == -1 {
		return -1
	}
	return from + i
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func main() {
	wikiDir := flag.String("wiki", "", "wiki directory (a COPY of prod)")
	diaryDir := flag.String("diary", "", "diary directory")
	goldPath := flag.String("gold", os.ExpandEnv("$HOME/.deneb/wiki-qa-gold.jsonl"), "gold-set JSONL")
	k := flag.Int("k", 8, "hit@K")
	verbose := flag.Bool("v", false, "print per-case ✓/✗")
	flag.Parse()
	if *wikiDir == "" {
		fmt.Fprintln(os.Stderr, "recall-bench: --wiki required")
		os.Exit(2)
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	ctx := context.Background()

	store, err := wiki.NewStore(*wikiDir, *diaryDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "open wiki: %v\n", err)
		os.Exit(1)
	}
	defer store.Close()

	emb := embedding.New("", logger)
	// Wait for the embedding server so semantic fusion actually runs (else this
	// silently measures BM25-only). Bounded — degrade to lexical if never up.
	for i := 0; i < 40 && !emb.IsHealthy(); i++ {
		time.Sleep(250 * time.Millisecond)
	}
	semantic := emb.IsHealthy()
	if semantic {
		store.SetEmbedder(emb)
		if err := store.WarmSemanticIndex(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "warm: %v\n", err)
		}
	}

	cases, err := loadGold(*goldPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gold: %v\n", err)
		os.Exit(1)
	}

	fusion := os.Getenv("DENEB_WIKI_FUSION")
	if fusion == "" {
		fusion = "rrf(default)"
	}
	fmt.Printf("== recall-bench  fusion=%s  semantic=%v  K=%d  cases=%d\n", fusion, semantic, *k, len(cases))

	var hit1, hitK, scored int
	var mrrSum float64
	for _, c := range cases {
		if len(c.GoldPaths) == 0 {
			continue
		}
		scored++
		results, err := store.Search(ctx, c.Question, *k)
		if err != nil {
			continue
		}
		rank := -1
		for i, r := range results {
			if i >= *k {
				break
			}
			for _, g := range c.GoldPaths {
				if pathHit(g, r.Path) {
					rank = i
					break
				}
			}
			if rank != -1 {
				break
			}
		}
		mark := "✗"
		if rank == 0 {
			hit1++
		}
		if rank != -1 {
			hitK++
			mrrSum += 1.0 / float64(rank+1)
			mark = fmt.Sprintf("✓@%d", rank+1)
		}
		if *verbose {
			fmt.Printf("  %-3s %-16s %s\n", mark, c.ID, c.Question)
		}
	}

	pct := func(n int) float64 {
		if scored == 0 {
			return 0
		}
		return 100 * float64(n) / float64(scored)
	}
	fmt.Printf("RECALL_BENCH hit@1=%d hit@%d=%d total=%d p@1=%.1f%% r@%d=%.1f%% mrr=%.3f fusion=%s\n",
		hit1, *k, hitK, scored, pct(hit1), *k, pct(hitK), mrrSum/float64(scored), fusion)
}

func loadGold(path string) ([]goldCase, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []goldCase
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var c goldCase
		if err := json.Unmarshal(line, &c); err != nil {
			continue // skip malformed
		}
		out = append(out, c)
	}
	return out, sc.Err()
}
