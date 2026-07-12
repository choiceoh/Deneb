// recall-bench — measures wiki retrieval quality (hit@K, MRR) against the gold
// set, so fusion changes (RRF, graph-boost) can be scored before/after on real
// data without a gateway.
//
// It calls wiki.Store.Search directly — the exact retrieval behind
// miniapp.memory.search (see handlerminiapp/knowledge.MemorySearcher) — so the number
// here IS what wiki-qa-bench.py's recall mode measures, minus the RPC hop.
//
// ALWAYS point --wiki at a COPY of the production wiki: wiki.NewStore is NOT
// read-only (it reconciles the index and ensures category dirs) and SetEmbedder
// warms the semantic cache into that dir, so it will mutate the tree it scores.
// A copy keeps production untouched. The fusion under test is selected by
// DENEB_WIKI_FUSION (and DENEB_WIKI_GRAPH_BOOST) so one binary scores every
// variant in a single run:
//
//	go run ./cmd/recall-bench --wiki /scratch/wiki --diary /scratch/diary \
//	  --gold ~/.deneb/wiki-qa-gold.jsonl --k 8
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
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
	if code := run(); code != 0 {
		os.Exit(code)
	}
}

type benchmarkConfig struct {
	wikiDir  string
	diaryDir string
	goldPath string
	k        int
	verbose  bool
}

type parseOutcome struct {
	done     bool
	exitCode int
}

type benchmarkStore interface {
	Close() error
	SetEmbedder(wiki.Embedder)
	WarmSemanticIndex(context.Context) error
	Search(context.Context, string, int) ([]wiki.SearchResult, error)
}

type runDependencies struct {
	openStore   func(wikiDir, diaryDir string) (benchmarkStore, error)
	newEmbedder func(*slog.Logger) wiki.Embedder
	loadCases   func(string) ([]goldCase, int, error)
	getenv      func(string) string
	sleep       func(time.Duration)
}

func defaultRunDependencies() runDependencies {
	return runDependencies{
		openStore: func(wikiDir, diaryDir string) (benchmarkStore, error) {
			return wiki.NewStore(wikiDir, diaryDir)
		},
		newEmbedder: func(logger *slog.Logger) wiki.Embedder {
			return embedding.New("", logger)
		},
		loadCases: loadGold,
		getenv:    os.Getenv,
		sleep:     time.Sleep,
	}
}

// run owns only process I/O and exit-code selection. Benchmark stages return
// before main calls os.Exit, so resource cleanup always completes.
func run() int {
	return runCLI(os.Args[0], os.Args[1:], os.Stdout, os.Stderr, defaultRunDependencies())
}

func runCLI(program string, args []string, stdout, stderr io.Writer, deps runDependencies) int {
	cfg, outcome := parseBenchmarkConfig(program, args, stderr)
	if outcome.done {
		return outcome.exitCode
	}
	if err := validateBenchmarkConfig(cfg); err != nil {
		fmt.Fprintln(stderr, "recall-bench:", err)
		return 1
	}
	if err := runBenchmark(context.Background(), cfg, stdout, stderr, deps); err != nil {
		fmt.Fprintln(stderr, "recall-bench:", err)
		return 1
	}
	return 0
}

func parseBenchmarkConfig(program string, args []string, stderr io.Writer) (benchmarkConfig, parseOutcome) {
	fs := flag.NewFlagSet(program, flag.ContinueOnError)
	fs.SetOutput(stderr)
	var cfg benchmarkConfig
	fs.StringVar(&cfg.wikiDir, "wiki", "", "wiki directory (a COPY of prod)")
	fs.StringVar(&cfg.diaryDir, "diary", "", "diary directory")
	fs.StringVar(&cfg.goldPath, "gold", os.ExpandEnv("$HOME/.deneb/wiki-qa-gold.jsonl"), "gold-set JSONL")
	fs.IntVar(&cfg.k, "k", 8, "hit@K")
	fs.BoolVar(&cfg.verbose, "v", false, "print per-case ✓/✗")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return cfg, parseOutcome{done: true}
		}
		return cfg, parseOutcome{done: true, exitCode: 2}
	}
	return cfg, parseOutcome{}
}

func validateBenchmarkConfig(cfg benchmarkConfig) error {
	if cfg.wikiDir == "" {
		return fmt.Errorf("--wiki required")
	}
	return nil
}

func runBenchmark(ctx context.Context, cfg benchmarkConfig, stdout, stderr io.Writer, deps runDependencies) error {
	logger := slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	store, err := deps.openStore(cfg.wikiDir, cfg.diaryDir)
	if err != nil {
		return fmt.Errorf("open wiki: %w", err)
	}
	defer store.Close()

	semantic, err := prepareSemantic(ctx, store, deps.newEmbedder(logger), deps.sleep)
	if err != nil {
		return err
	}
	cases, err := loadValidatedCases(cfg.goldPath, deps.loadCases)
	if err != nil {
		return err
	}
	fusion, graphBoost := resolveFusion(deps.getenv, semantic)
	writeBenchmarkHeader(stdout, cfg, fusion, graphBoost, semantic, len(cases))

	result := evaluateCases(ctx, store, cases, cfg.k, cfg.verbose, stdout)
	if err := result.validate(); err != nil {
		return err
	}
	writeBenchmarkResult(stdout, cfg.k, fusion, result)
	return nil
}

func prepareSemantic(
	ctx context.Context,
	store benchmarkStore,
	embedder wiki.Embedder,
	sleep func(time.Duration),
) (bool, error) {
	semantic := waitForHealthyEmbedder(embedder, sleep)
	if !semantic {
		return false, nil
	}
	store.SetEmbedder(embedder)
	if err := store.WarmSemanticIndex(ctx); err != nil {
		return false, fmt.Errorf("semantic warm FAILED (%w) — metrics would be BM25-degraded", err)
	}
	return true, nil
}

func waitForHealthyEmbedder(embedder wiki.Embedder, sleep func(time.Duration)) bool {
	if embedder == nil {
		return false
	}
	for range 40 {
		if embedder.IsHealthy() {
			return true
		}
		sleep(250 * time.Millisecond)
	}
	return embedder.IsHealthy()
}

func loadValidatedCases(
	path string,
	load func(string) ([]goldCase, int, error),
) ([]goldCase, error) {
	cases, malformed, err := load(path)
	if err != nil {
		return nil, fmt.Errorf("gold: %w", err)
	}
	// Malformed rows silently shrink the denominator and can hide a regression.
	if malformed > 0 {
		return nil, fmt.Errorf("%d malformed gold row(s) in %s — fix or remove them", malformed, path)
	}
	return cases, nil
}

func resolveFusion(getenv func(string) string, semantic bool) (string, bool) {
	configured := getenv("DENEB_WIKI_FUSION")
	fusion := configured
	if fusion == "" {
		fusion = "rrf(default)"
	}
	// Additive ignores graph paths; RRF graph boost additionally requires a
	// healthy semantic index and an enabled graph flag.
	graphBoost := configured != "additive" && getenv("DENEB_WIKI_GRAPH_BOOST") != "off" && semantic
	return fusion, graphBoost
}

type benchmarkResult struct {
	hit1       int
	hitK       int
	scored     int
	searchErrs int
	mrrSum     float64
}

func evaluateCases(
	ctx context.Context,
	store benchmarkStore,
	cases []goldCase,
	k int,
	verbose bool,
	stdout io.Writer,
) benchmarkResult {
	var result benchmarkResult
	for _, c := range cases {
		if len(c.GoldPaths) == 0 {
			continue
		}
		matches, err := store.Search(ctx, c.Question, k)
		if err != nil {
			result.searchErrs++
			continue
		}
		rank := findGoldRank(matches, c.GoldPaths, k)
		result.record(rank)
		if verbose {
			writeCaseResult(stdout, c, matches, rank)
		}
	}
	return result
}

func findGoldRank(results []wiki.SearchResult, goldPaths []string, k int) int {
	for i, result := range results {
		if i >= k {
			break
		}
		for _, gold := range goldPaths {
			if pathHit(gold, result.Path) {
				return i
			}
		}
	}
	return -1
}

func (r *benchmarkResult) record(rank int) {
	r.scored++
	if rank == 0 {
		r.hit1++
	}
	if rank >= 0 {
		r.hitK++
		r.mrrSum += 1.0 / float64(rank+1)
	}
}

func (r benchmarkResult) validate() error {
	if r.scored == 0 {
		return fmt.Errorf("0 cases scored (empty/comment-only gold, all rows lack gold_paths, or %d search error(s))", r.searchErrs)
	}
	return nil
}

func writeCaseResult(out io.Writer, c goldCase, results []wiki.SearchResult, rank int) {
	mark := "✗"
	if rank >= 0 {
		mark = fmt.Sprintf("✓@%d", rank+1)
	}
	fmt.Fprintf(out, "  %-3s %-16s %s\n", mark, c.ID, c.Question)
	if rank == -1 {
		fmt.Fprintf(out, "        gold=%v  got=%v\n", c.GoldPaths, topResultPaths(results, 3))
	}
}

func topResultPaths(results []wiki.SearchResult, limit int) []string {
	top := make([]string, 0, limit)
	for i, result := range results {
		if i >= limit {
			break
		}
		top = append(top, result.Path)
	}
	return top
}

func writeBenchmarkHeader(out io.Writer, cfg benchmarkConfig, fusion string, graphBoost, semantic bool, cases int) {
	fmt.Fprintf(out, "== recall-bench  fusion=%s  graph_boost=%v  semantic=%v  K=%d  cases=%d\n",
		fusion, graphBoost, semantic, cfg.k, cases)
}

func writeBenchmarkResult(out io.Writer, k int, fusion string, result benchmarkResult) {
	if result.searchErrs > 0 {
		fmt.Fprintf(out, "recall-bench: %d search error(s) excluded from the metric\n", result.searchErrs)
	}
	pct := func(n int) float64 {
		return 100 * float64(n) / float64(result.scored)
	}
	fmt.Fprintf(out, "RECALL_BENCH hit@1=%d hit@%d=%d total=%d p@1=%.1f%% r@%d=%.1f%% mrr=%.3f fusion=%s\n",
		result.hit1, k, result.hitK, result.scored, pct(result.hit1), k, pct(result.hitK), result.mrrSum/float64(result.scored), fusion)
}

// loadGold parses the gold JSONL, returning the cases and the count of malformed
// (non-empty, unparseable) rows so the caller can refuse to score against
// corrupt data. Blank lines are not malformed.
func loadGold(path string) ([]goldCase, int, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()
	var out []goldCase
	var skipped int
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		// Blank lines and '#' comments are structural (the gold file opens with a
		// header + section dividers), not malformed — skip without counting.
		if len(line) == 0 || line[0] == '#' {
			continue
		}
		var c goldCase
		if err := json.Unmarshal(line, &c); err != nil {
			skipped++
			continue
		}
		out = append(out, c)
	}
	return out, skipped, sc.Err()
}
