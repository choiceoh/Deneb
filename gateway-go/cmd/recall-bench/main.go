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
// A copy keeps production untouched. The default path honors DENEB_WIKI_FUSION
// (and DENEB_WIKI_GRAPH_BOOST); --matrix scores BM25, semantic-only, hybrid,
// and graph-enhanced full retrieval in one run:
//
//	go run ./cmd/recall-bench --wiki /scratch/wiki --diary /scratch/diary \
//	  --gold ~/.deneb/wiki-qa-gold.jsonl --k 8 --matrix
//
// --health adds recall's loop-closing signals (production ledger utility +
// gold-set coverage of the live project roster + a composite recall-health
// score); --emit-gold prints deterministic gold candidates for uncovered
// projects. Both are default-off so the bare invocation stays a pure P@K tool.
// The `make recall-health` target wires the wiki copy + --health end to end;
// see health.go.
//
// Gold sets live outside the repo in ~/.deneb/ (real data): wiki-qa-gold.jsonl
// (hand-written chat-style questions, the default) plus themed sets like
// wiki-qa-gold-hard/-multiturn/-analysis-xl.jsonl. The analysis-xl set — real
// mail/approval subjects labeled with their wiki project, probing the dominant
// mail/approval-analysis recall path at scale — is regenerated with
// scripts/audit/mine_analysis_gold.py (spot-check labels before trusting; see
// its docstring for the precision guards).
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"hash/fnv"
	"io"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/embedding"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/rerank"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
)

type goldCase struct {
	ID          string   `json:"id"`
	Category    string   `json:"category"`
	Question    string   `json:"question"`
	GoldPaths   []string `json:"gold_paths"`
	MustContain []string `json:"must_contain"`
	// MustNot lists answer tokens that must NOT surface. wiki-qa-bench.py --mode
	// answer already scores them at the answer level; under --content they feed
	// the retrieval-level leakage rate.
	MustNot []string `json:"must_not"`
	// OpType tags the memory-lifecycle operation the case probes: "" or
	// "remember" (plain fact), "update" (fact was superseded), "forget" (fact
	// was tombstone-deleted). Any other value counts as a malformed row.
	OpType string `json:"op_type"`
	// StaleValues holds old-value tokens from superseded/deleted pages; under
	// --content the stale-value rate counts cases whose top-K result bodies
	// still expose one.
	StaleValues []string `json:"stale_values"`
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
	// health adds the loop-closing report (ledger utility + gold-set coverage +
	// composite recall-health score) after the pure P@K result. emitGold prints
	// deterministic gold candidates for uncovered projects. Both read-only over
	// the wiki COPY; default off so `recall-bench` alone stays a pure P@K tool.
	health      bool
	emitGold    bool
	matrix      bool
	byCategory  bool
	dumpSignals bool
	content     bool
	holdoutPct  int
	split       string
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

type benchmarkOptionStore interface {
	SearchWithOptions(context.Context, string, int, wiki.QueryOptions) (wiki.SearchReport, error)
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
			store, err := wiki.NewStore(wikiDir, diaryDir)
			if err != nil {
				return nil, err
			}
			// Mirror production wiring (initSessionAI): a reranker exists only
			// when DENEB_RERANK_URL is set, so the bench scores the exact
			// retrieval stack the gateway would run under the same env.
			if reranker := rerank.NewFromEnv(); reranker != nil {
				store.SetReranker(reranker)
			}
			return store, nil
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
	fs.BoolVar(&cfg.health, "health", false, "add ledger-utility + gold coverage + composite recall-health score")
	fs.BoolVar(&cfg.emitGold, "emit-gold", false, "print deterministic gold candidates for uncovered projects (implies -health)")
	fs.BoolVar(&cfg.matrix, "matrix", false, "compare bm25, semantic, hybrid, and full retrieval with p50/p95 latency")
	fs.BoolVar(&cfg.byCategory, "by-category", false, "per-category P@1 across bm25/semantic/hybrid/full modes — diagnoses where global fusion weights lose to a single mode (headroom for per-query-type weighting)")
	fs.BoolVar(&cfg.dumpSignals, "dump-signals", false, "per-case signal dump (category, top semantic cosine, gold rank under bm25 vs semantic) — grounds confidence-weighted fusion in real numbers")
	fs.BoolVar(&cfg.content, "content", false, "content-aware hit: a result also counts when its page body holds every must_contain answer token (| = alternatives), not only when its path matches gold_paths — robust to wiki folder renames that stale path-based gold")
	fs.IntVar(&cfg.holdoutPct, "holdout-pct", 0, "hold out this %% of cases (stable hash of case ID) as a test split; 0 = use all")
	fs.StringVar(&cfg.split, "split", "all", "which split to score when --holdout-pct>0: all|train|test")
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
	if cfg.holdoutPct < 0 || cfg.holdoutPct > 100 {
		return fmt.Errorf("--holdout-pct must be 0..100")
	}
	switch cfg.split {
	case "all", "train", "test":
	default:
		return fmt.Errorf("--split must be all|train|test")
	}
	return nil
}

// filterSplit partitions cases deterministically by a stable hash of the case
// ID: the test split is the cases whose bucket (hash%100) falls under
// holdoutPct, train is the complement. Stable across runs and independent of
// gold order, so baseline vs candidate score the SAME held-out cases.
func filterSplit(cases []goldCase, holdoutPct int, split string) []goldCase {
	if holdoutPct <= 0 || split == "all" || split == "" {
		return cases
	}
	out := make([]goldCase, 0, len(cases))
	for _, c := range cases {
		h := fnv.New32a()
		_, _ = h.Write([]byte(c.ID))
		isTest := int(h.Sum32()%100) < holdoutPct
		if (split == "test") == isTest {
			out = append(out, c)
		}
	}
	return out
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
	if cfg.holdoutPct > 0 {
		total := len(cases)
		cases = filterSplit(cases, cfg.holdoutPct, cfg.split)
		fmt.Fprintf(stdout, "== split=%s holdout_pct=%d  %d/%d cases (stable hash of case ID)\n", cfg.split, cfg.holdoutPct, len(cases), total)
	}
	fusion, graphBoost := resolveFusion(deps.getenv, semantic)
	writeBenchmarkHeader(stdout, cfg, fusion, graphBoost, semantic, len(cases))

	var content contentMatcher
	var holds bodyHolder
	if cfg.content {
		content, holds = newContentScorers(cfg.wikiDir)
		fmt.Fprintln(stdout, "== content-aware scoring: a hit also counts when the page body holds every must_contain token")
	}

	var result benchmarkResult
	if cfg.byCategory {
		optionStore, ok := store.(benchmarkOptionStore)
		if !ok {
			return fmt.Errorf("store does not expose stage-specific search required by --by-category")
		}
		reportByCategory(ctx, optionStore, cases, cfg.k, stdout, content)
		return nil
	}
	if cfg.dumpSignals {
		optionStore, ok := store.(benchmarkOptionStore)
		if !ok {
			return fmt.Errorf("store does not expose stage-specific search required by --dump-signals")
		}
		dumpSignals(ctx, optionStore, cases, cfg.k, stdout)
		return nil
	}
	if cfg.matrix {
		optionStore, ok := store.(benchmarkOptionStore)
		if !ok {
			return fmt.Errorf("store does not expose stage-specific search required by --matrix")
		}
		fmt.Fprintln(stdout, "== recall-bench matrix  modes=bm25,semantic,hybrid,full")
		for _, mode := range []wiki.SearchMode{wiki.SearchModeBM25, wiki.SearchModeSemantic, wiki.SearchModeHybrid, wiki.SearchModeFull} {
			modeResult := evaluateCasesWithSearch(ctx, cases, cfg.k, cfg.verbose, stdout, func(ctx context.Context, query string, limit int) ([]wiki.SearchResult, error) {
				report, searchErr := optionStore.SearchWithOptions(ctx, query, limit, wiki.QueryOptions{Mode: mode})
				return report.Results, searchErr
			}, content, holds)
			if err := modeResult.validate(); err != nil {
				return fmt.Errorf("mode %s: %w", mode, err)
			}
			writeBenchmarkMatrixResult(stdout, cfg.k, mode, modeResult)
			if mode == wiki.SearchModeFull {
				result = modeResult
			}
		}
	} else {
		result = evaluateCases(ctx, store, cases, cfg.k, cfg.verbose, stdout, content, holds)
		if err := result.validate(); err != nil {
			return err
		}
		writeBenchmarkResult(stdout, cfg.k, fusion, result)
	}

	if cfg.health || cfg.emitGold {
		reportRecallHealth(store, cases, result, cfg.emitGold, stdout, stderr)
	}
	return nil
}

// reportRecallHealth adds the loop-closing signals. Requires the store to expose
// the production ledger + project roster (the real *wiki.Store does); a store
// that does not is skipped with a note rather than failing the pure bench.
func reportRecallHealth(
	store benchmarkStore,
	cases []goldCase,
	result benchmarkResult,
	emitGold bool,
	stdout, stderr io.Writer,
) {
	hs, ok := store.(healthStore)
	if !ok {
		fmt.Fprintln(stderr, "recall-bench: store does not expose recall-health surface; skipping")
		return
	}
	util := computeLedgerUtility(hs.RecallUsageScoreCounts(time.Now()))
	cov := computeGoldCoverage(cases, hs.KnownProjects())
	health := computeRecallHealth(result, cov)
	writeRecallHealth(stdout, &util, cov, health)
	if emitGold && len(cov.uncovered) > 0 {
		emitGoldCandidates(stdout, cov.uncovered)
	}
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
	hit1          int
	hitK          int
	scored        int
	searchErrs    int
	mrrSum        float64
	latencies     []time.Duration
	precisionKSum float64
	recall1Sum    float64
	recall3Sum    float64
	recall5Sum    float64
	recallKSum    float64
	f1KSum        float64
	// Memory-lifecycle exposure (MemOps-style), scored only under --content:
	// counted per case that carries the corresponding gold field, exposed when
	// ANY of its tokens appears in a top-K result body. Additive — the P@K/MRR
	// math above is untouched.
	staleCases   int // cases with stale_values
	staleExposed int // ...whose top-K bodies still expose a superseded/deleted value
	leakCases    int // cases with must_not
	leakExposed  int // ...whose top-K bodies expose a forbidden token
	opUpdate     int // op_type=update cases seen
	opForget     int // op_type=forget cases seen
}

func evaluateCases(
	ctx context.Context,
	store benchmarkStore,
	cases []goldCase,
	k int,
	verbose bool,
	stdout io.Writer,
	content contentMatcher,
	holds bodyHolder,
) benchmarkResult {
	return evaluateCasesWithSearch(ctx, cases, k, verbose, stdout, store.Search, content, holds)
}

func evaluateCasesWithSearch(
	ctx context.Context,
	cases []goldCase,
	k int,
	verbose bool,
	stdout io.Writer,
	search func(context.Context, string, int) ([]wiki.SearchResult, error),
	content contentMatcher,
	holds bodyHolder,
) benchmarkResult {
	var result benchmarkResult
	for _, c := range cases {
		if len(c.GoldPaths) == 0 {
			continue
		}
		started := time.Now()
		matches, err := search(ctx, c.Question, max(k, 5))
		elapsed := time.Since(started)
		if err != nil {
			result.searchErrs++
			continue
		}
		result.latencies = append(result.latencies, elapsed)
		rank := findGoldRank(matches, c, k, content)
		result.record(rank, rankingQuality(matches, c, k, content))
		if holds != nil {
			result.recordLifecycle(c, matches, k, holds)
		}
		if verbose {
			writeCaseResult(stdout, c, matches, rank)
		}
	}
	return result
}

// recordLifecycle scores the memory-lifecycle exposure of one case: whether any
// stale_values (superseded/deleted old values) or must_not (forbidden answers)
// token still surfaces in a top-K result body. This is the retrieval-level twin
// of wiki-qa-bench.py --mode answer's must_not scoring.
func (r *benchmarkResult) recordLifecycle(c goldCase, results []wiki.SearchResult, k int, holds bodyHolder) {
	switch c.OpType {
	case "update":
		r.opUpdate++
	case "forget":
		r.opForget++
	}
	if len(c.StaleValues) == 0 && len(c.MustNot) == 0 {
		return
	}
	staleHit, leakHit := false, false
	for i, result := range results {
		if i >= k {
			break
		}
		staleHit = staleHit || (len(c.StaleValues) > 0 && holds(result.Path, c.StaleValues))
		leakHit = leakHit || (len(c.MustNot) > 0 && holds(result.Path, c.MustNot))
	}
	if len(c.StaleValues) > 0 {
		r.staleCases++
		if staleHit {
			r.staleExposed++
		}
	}
	if len(c.MustNot) > 0 {
		r.leakCases++
		if leakHit {
			r.leakExposed++
		}
	}
}

func (r benchmarkResult) latencyPercentile(percentile float64) time.Duration {
	if len(r.latencies) == 0 {
		return 0
	}
	values := append([]time.Duration(nil), r.latencies...)
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
	index := int(math.Ceil(percentile*float64(len(values)))) - 1
	if index < 0 {
		index = 0
	}
	if index >= len(values) {
		index = len(values) - 1
	}
	return values[index]
}

// contentMatcher reports whether a result page satisfies a case's answer tokens.
// nil disables content-aware scoring (path-only). Built over the wiki dir so it
// reads the exact tree the bench searched.
type contentMatcher func(path string, mustContain []string) bool

// bodyHolder reports whether a result page's body exposes ANY of the given
// tokens (a "a|b" token matches on any alternative). nil disables the lifecycle
// (stale-value/leakage) metrics — they only make sense with page bodies.
type bodyHolder func(path string, tokens []string) bool

// newContentScorers builds both content-aware scorers over ONE shared page
// cache: the matcher reports a hit when EVERY must_contain token appears (empty
// never matches — falls back to path-only), the holder reports exposure when
// ANY token appears (empty never exposes). The cache keeps repeated reads
// across the top-K window cheap.
func newContentScorers(wikiDir string) (contentMatcher, bodyHolder) {
	cache := make(map[string]string)
	read := func(rel string) string {
		if b, ok := cache[rel]; ok {
			return b
		}
		name := rel
		if !strings.HasSuffix(name, ".md") {
			name += ".md"
		}
		data, err := os.ReadFile(filepath.Join(wikiDir, name))
		body := ""
		if err == nil {
			body = string(data)
		}
		cache[rel] = body
		return body
	}
	tokenIn := func(body, tok string) bool {
		for _, alt := range strings.Split(tok, "|") {
			if alt != "" && strings.Contains(body, alt) {
				return true
			}
		}
		return false
	}
	matcher := func(path string, mustContain []string) bool {
		if len(mustContain) == 0 {
			return false
		}
		body := read(path)
		if body == "" {
			return false
		}
		for _, tok := range mustContain {
			if !tokenIn(body, tok) {
				return false
			}
		}
		return true
	}
	holder := func(path string, tokens []string) bool {
		body := read(path)
		if body == "" {
			return false
		}
		for _, tok := range tokens {
			if tokenIn(body, tok) {
				return true
			}
		}
		return false
	}
	return matcher, holder
}

// newContentMatcher keeps the single-scorer constructor for callers (and tests)
// that only need must_contain matching.
func newContentMatcher(wikiDir string) contentMatcher {
	matcher, _ := newContentScorers(wikiDir)
	return matcher
}

func caseHit(result wiki.SearchResult, c goldCase, content contentMatcher) bool {
	for _, gold := range c.GoldPaths {
		if pathHit(gold, result.Path) {
			return true
		}
	}
	return content != nil && content(result.Path, c.MustContain)
}

func findGoldRank(results []wiki.SearchResult, c goldCase, k int, content contentMatcher) int {
	for i, result := range results {
		if i >= k {
			break
		}
		if caseHit(result, c, content) {
			return i
		}
	}
	return -1
}

type qualityMetrics struct {
	precisionK float64
	recall1    float64
	recall3    float64
	recall5    float64
	recallK    float64
	f1K        float64
}

func rankingQuality(results []wiki.SearchResult, c goldCase, k int, content contentMatcher) qualityMetrics {
	goldPaths := c.GoldPaths
	recallAt := func(limit int) float64 {
		if len(goldPaths) == 0 {
			return 0
		}
		matched := make([]bool, len(goldPaths))
		contentSeen := false
		for i, result := range results {
			if i >= limit {
				break
			}
			for goldIndex, gold := range goldPaths {
				if pathHit(gold, result.Path) {
					matched[goldIndex] = true
				}
			}
			if content != nil && content(result.Path, c.MustContain) {
				contentSeen = true
			}
		}
		count := 0
		for _, ok := range matched {
			if ok {
				count++
			}
		}
		// Content-aware: an answer-bearing page satisfies at least one gold slot
		// even when its (post-rename) path matches no gold_paths entry.
		if contentSeen && count == 0 {
			count = 1
		}
		return float64(count) / float64(len(goldPaths))
	}
	relevant := 0
	for i, result := range results {
		if i >= k {
			break
		}
		if caseHit(result, c, content) {
			relevant++
		}
	}
	precision := 0.0
	if k > 0 {
		precision = float64(relevant) / float64(k)
	}
	recallK := recallAt(k)
	f1 := 0.0
	if precision+recallK > 0 {
		f1 = 2 * precision * recallK / (precision + recallK)
	}
	return qualityMetrics{
		precisionK: precision, recall1: recallAt(1), recall3: recallAt(3),
		recall5: recallAt(5), recallK: recallK, f1K: f1,
	}
}

func (r *benchmarkResult) record(rank int, quality qualityMetrics) {
	r.scored++
	r.precisionKSum += quality.precisionK
	r.recall1Sum += quality.recall1
	r.recall3Sum += quality.recall3
	r.recall5Sum += quality.recall5
	r.recallKSum += quality.recallK
	r.f1KSum += quality.f1K
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
	writeQualityMetrics(out, "RECALL_BENCH_QUALITY", k, result)
	writeLifecycleMetrics(out, "RECALL_BENCH_LIFECYCLE", k, result)
}

func writeBenchmarkMatrixResult(out io.Writer, k int, mode wiki.SearchMode, result benchmarkResult) {
	if result.searchErrs > 0 {
		fmt.Fprintf(out, "recall-bench: mode=%s %d search error(s) excluded from the metric\n", mode, result.searchErrs)
	}
	pct := func(n int) float64 { return 100 * float64(n) / float64(result.scored) }
	fmt.Fprintf(out, "RECALL_BENCH_MATRIX mode=%s hit@1=%d hit@%d=%d total=%d p@1=%.1f%% r@%d=%.1f%% mrr=%.3f p50_ms=%.3f p95_ms=%.3f\n",
		mode, result.hit1, k, result.hitK, result.scored, pct(result.hit1), k, pct(result.hitK), result.mrrSum/float64(result.scored),
		float64(result.latencyPercentile(0.50))/float64(time.Millisecond), float64(result.latencyPercentile(0.95))/float64(time.Millisecond))
	writeQualityMetrics(out, "RECALL_BENCH_MATRIX_QUALITY mode="+string(mode), k, result)
	writeLifecycleMetrics(out, "RECALL_BENCH_MATRIX_LIFECYCLE mode="+string(mode), k, result)
}

// reportByCategory answers the question that decides whether per-query-type
// fusion weights are worth building: for each gold category, which single
// retrieval mode gives the best P@1? When the full (globally-weighted) fusion
// already wins every category, there is no headroom — the global weights are
// already category-appropriate and per-type weighting is overfitting waiting to
// happen. When a category scores strictly higher under bm25-only or
// semantic-only than under full, that gap is the achievable headroom from
// routing that category to different weights. Read-only over the wiki copy.
func reportByCategory(
	ctx context.Context,
	store benchmarkOptionStore,
	cases []goldCase,
	k int,
	stdout io.Writer,
	content contentMatcher,
) {
	modes := []wiki.SearchMode{wiki.SearchModeBM25, wiki.SearchModeSemantic, wiki.SearchModeHybrid, wiki.SearchModeFull}
	searchFor := func(mode wiki.SearchMode) func(context.Context, string, int) ([]wiki.SearchResult, error) {
		return func(ctx context.Context, query string, limit int) ([]wiki.SearchResult, error) {
			report, err := store.SearchWithOptions(ctx, query, limit, wiki.QueryOptions{Mode: mode})
			return report.Results, err
		}
	}

	// Group cases by category, preserving first-seen order for stable output.
	order := make([]string, 0)
	groups := make(map[string][]goldCase)
	for _, c := range cases {
		if len(c.GoldPaths) == 0 {
			continue
		}
		cat := c.Category
		if cat == "" {
			cat = "(none)"
		}
		if _, seen := groups[cat]; !seen {
			order = append(order, cat)
		}
		groups[cat] = append(groups[cat], c)
	}

	fmt.Fprintf(stdout, "== recall-bench by-category  K=%d  modes=bm25,semantic,hybrid,full  (P@1 per mode; ★=best, Δ=best−full)\n", k)
	fmt.Fprintf(stdout, "%-10s %3s  %6s %6s %6s %6s   %-8s %6s\n", "category", "n", "bm25", "sem", "hybrid", "full", "best", "Δ")

	headroomCats := 0
	weightedHeadroom := 0.0
	totalScored := 0
	for _, cat := range order {
		grp := groups[cat]
		p1 := make(map[wiki.SearchMode]float64, len(modes))
		var scored int
		for _, mode := range modes {
			// Lifecycle metrics stay off (nil holder) — this report prints P@1 only.
			res := evaluateCasesWithSearch(ctx, grp, k, false, io.Discard, searchFor(mode), content, nil)
			scored = res.scored
			if res.scored > 0 {
				p1[mode] = 100 * float64(res.hit1) / float64(res.scored)
			}
		}
		bestMode := wiki.SearchModeFull
		bestP1 := p1[wiki.SearchModeFull]
		for _, mode := range modes {
			if p1[mode] > bestP1 {
				bestP1 = p1[mode]
				bestMode = mode
			}
		}
		delta := bestP1 - p1[wiki.SearchModeFull]
		mark := func(mode wiki.SearchMode) string {
			s := fmt.Sprintf("%.0f", p1[mode])
			if mode == bestMode && delta > 0 {
				return s + "★"
			}
			return s
		}
		fmt.Fprintf(stdout, "%-10s %3d  %6s %6s %6s %6s   %-8s %5.0f\n",
			cat, scored, mark(wiki.SearchModeBM25), mark(wiki.SearchModeSemantic),
			mark(wiki.SearchModeHybrid), mark(wiki.SearchModeFull), bestMode, delta)
		if delta > 0 {
			headroomCats++
			weightedHeadroom += delta * float64(scored)
		}
		totalScored += scored
	}
	overall := 0.0
	if totalScored > 0 {
		overall = weightedHeadroom / float64(totalScored)
	}
	fmt.Fprintf(stdout, "RECALL_BENCH_BYCAT headroom_categories=%d/%d weighted_p@1_headroom=%.2fpp total=%d\n",
		headroomCats, len(order), overall, totalScored)
	fmt.Fprintln(stdout, "  (weighted_p@1_headroom = max attainable P@1 gain if each category were routed to its best single mode — an upper bound; real per-type weighting captures a fraction)")
}

// dumpSignals prints, per case, the raw retrieval signals a confidence-weighted
// fusion would key on: the top semantic cosine (semantic self-confidence) and
// where the gold page lands under BM25-only vs semantic-only. Machine-readable
// (SIGNAL prefix) so the numbers can be aggregated by category — the empirical
// basis for scaling each signal's fusion weight by its own confidence instead of
// a fixed global weight.
func dumpSignals(ctx context.Context, store benchmarkOptionStore, cases []goldCase, k int, stdout io.Writer) {
	fmt.Fprintln(stdout, "SIGNAL_HEADER category topSemCos goldSemRank goldSemCos goldBm25Rank goldFullRank id")
	goldRankAndScore := func(results []wiki.SearchResult, gold []string) (int, float64) {
		for i, r := range results {
			for _, g := range gold {
				if pathHit(g, r.Path) {
					return i, r.Score
				}
			}
		}
		return -1, 0
	}
	for _, c := range cases {
		if len(c.GoldPaths) == 0 {
			continue
		}
		semRep, err1 := store.SearchWithOptions(ctx, c.Question, max(k, 10), wiki.QueryOptions{Mode: wiki.SearchModeSemantic})
		bmRep, err2 := store.SearchWithOptions(ctx, c.Question, max(k, 10), wiki.QueryOptions{Mode: wiki.SearchModeBM25})
		fullRep, err3 := store.SearchWithOptions(ctx, c.Question, max(k, 10), wiki.QueryOptions{Mode: wiki.SearchModeFull})
		if err1 != nil || err2 != nil || err3 != nil {
			continue
		}
		topSemCos := 0.0
		if len(semRep.Results) > 0 {
			topSemCos = semRep.Results[0].Score
		}
		goldSemRank, goldSemCos := goldRankAndScore(semRep.Results, c.GoldPaths)
		goldBm25Rank, _ := goldRankAndScore(bmRep.Results, c.GoldPaths)
		goldFullRank, _ := goldRankAndScore(fullRep.Results, c.GoldPaths)
		fmt.Fprintf(stdout, "SIGNAL %s %.4f %d %.4f %d %d %s\n",
			c.Category, topSemCos, goldSemRank, goldSemCos, goldBm25Rank, goldFullRank, c.ID)
		// Raw per-mode rankings (path TAB score). Weight-independent, so one dump
		// lets an offline harness re-fuse them under ANY weight scheme — global
		// sweeps and per-query adaptive weighting alike — without another Go run.
		writeRankedPaths(stdout, "BM25", c.ID, bmRep.Results)
		writeRankedPaths(stdout, "SEM", c.ID, semRep.Results)
		writeRankedPaths(stdout, "FULL", c.ID, fullRep.Results)
	}
}

func writeRankedPaths(out io.Writer, mode, id string, results []wiki.SearchResult) {
	fmt.Fprintf(out, "PATHS %s %s", mode, id)
	for i, r := range results {
		if i >= 10 {
			break
		}
		fmt.Fprintf(out, "\t%s\x1f%.4f", r.Path, r.Score)
	}
	fmt.Fprintln(out)
}

func writeQualityMetrics(out io.Writer, prefix string, k int, result benchmarkResult) {
	denominator := float64(result.scored)
	fmt.Fprintf(out, "%s p@%d=%.3f recall@1=%.3f recall@3=%.3f recall@5=%.3f recall@%d=%.3f f1@%d=%.3f\n",
		prefix, k, result.precisionKSum/denominator, result.recall1Sum/denominator,
		result.recall3Sum/denominator, result.recall5Sum/denominator,
		k, result.recallKSum/denominator, k, result.f1KSum/denominator)
}

// writeLifecycleMetrics prints the stale-value and leakage rates (lower is
// better: % of lifecycle-labeled cases whose top-K result bodies expose an old
// or forbidden value). Printed only when the run scored at least one such case
// under --content, so existing gold sets keep byte-identical output.
func writeLifecycleMetrics(out io.Writer, prefix string, k int, result benchmarkResult) {
	if result.staleCases == 0 && result.leakCases == 0 {
		return
	}
	rate := func(exposed, cases int) float64 {
		if cases == 0 {
			return 0
		}
		return 100 * float64(exposed) / float64(cases)
	}
	fmt.Fprintf(out, "%s k=%d stale_rate=%.1f%% (%d/%d) leak_rate=%.1f%% (%d/%d) op_update=%d op_forget=%d\n",
		prefix, k,
		rate(result.staleExposed, result.staleCases), result.staleExposed, result.staleCases,
		rate(result.leakExposed, result.leakCases), result.leakExposed, result.leakCases,
		result.opUpdate, result.opForget)
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
		// A typo'd op_type would silently score as a plain remember case and
		// misreport the lifecycle rates — treat it as malformed instead.
		switch c.OpType {
		case "", "remember", "update", "forget":
		default:
			skipped++
			continue
		}
		out = append(out, c)
	}
	return out, skipped, sc.Err()
}
