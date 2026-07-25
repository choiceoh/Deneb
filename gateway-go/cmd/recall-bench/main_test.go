package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
)

func TestPathHitRequiresSegmentBoundary(t *testing.T) {
	tests := []struct {
		name, gold, path string
		want             bool
	}{
		{"exact", "프로젝트/영덕.md", "프로젝트/영덕.md", true},
		{"suffix expansion", "비금도-154kv", "프로젝트/비금도-154kv-케이블.md", true},
		{"nested segment", "영덕", "프로젝트/영덕/대표.md", true},
		{"substring is not a segment", "영덕", "프로젝트/남영덕/대표.md", false},
		{"empty", "", "프로젝트/영덕.md", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := pathHit(tt.gold, tt.path); got != tt.want {
				t.Fatalf("pathHit(%q, %q) = %v", tt.gold, tt.path, got)
			}
		})
	}
}

func TestLoadGoldSkipsCommentsAndCountsMalformedRows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gold.jsonl")
	data := "# header\n\n" +
		`{"id":"one","question":"q","gold_paths":["a.md"]}` + "\n" +
		"not-json\n" +
		`{"id":"two","question":"q2","gold_paths":["b.md"]}` + "\n"
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	cases, malformed, err := loadGold(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cases) != 2 || malformed != 1 || cases[1].ID != "two" {
		t.Fatalf("cases=%#v malformed=%d", cases, malformed)
	}
}

func TestLoadGoldParsesLifecycleFieldsAndRejectsUnknownOpType(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gold.jsonl")
	data := `{"id":"upd","question":"q","gold_paths":["new.md"],"must_not":["옛담당"],"op_type":"update","stale_values":["1.0억"]}` + "\n" +
		`{"id":"typo","question":"q","gold_paths":["a.md"],"op_type":"updated"}` + "\n" +
		`{"id":"plain","question":"q","gold_paths":["b.md"]}` + "\n"
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	cases, malformed, err := loadGold(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cases) != 2 || malformed != 1 {
		t.Fatalf("cases=%d malformed=%d, want 2/1 (unknown op_type must count malformed)", len(cases), malformed)
	}
	c := cases[0]
	if c.OpType != "update" || len(c.StaleValues) != 1 || c.StaleValues[0] != "1.0억" || len(c.MustNot) != 1 {
		t.Fatalf("lifecycle fields not parsed: %#v", c)
	}
}

func TestRunCLIParseExitAndErrorCharacteristics(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		args       []string
		wantCode   int
		wantStderr string
	}{
		{name: "missing wiki", wantCode: 1, wantStderr: "recall-bench: --wiki required\n"},
		{name: "unknown flag", args: []string{"--unknown"}, wantCode: 2, wantStderr: "flag provided but not defined: -unknown"},
		{name: "help", args: []string{"-h"}, wantCode: 0, wantStderr: "Usage of recall-bench:"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := runCLI("recall-bench", tt.args, &stdout, &stderr, unopenedRunDependencies())
			if code != tt.wantCode {
				t.Fatalf("exit code = %d, want %d", code, tt.wantCode)
			}
			if !strings.Contains(stderr.String(), tt.wantStderr) {
				t.Fatalf("stderr = %q, want %q", stderr.String(), tt.wantStderr)
			}
			if stdout.Len() != 0 {
				t.Fatalf("stdout = %q, want empty", stdout.String())
			}
		})
	}
}

func TestRunCLIPreservesScoringAndSearchErrorPolicy(t *testing.T) {
	t.Parallel()
	store := &fakeBenchmarkStore{
		results: map[string][]wiki.SearchResult{
			"alpha": {{Path: "projects/alpha.md"}},
			"beta":  {{Path: "projects/wrong.md"}, {Path: "projects/beta.md"}},
			"miss": {
				{Path: "projects/wrong-a.md"},
				{Path: "projects/wrong-b.md"},
				{Path: "projects/wrong-c.md"},
			},
		},
		searchErrs: map[string]error{"broken": errors.New("backend down")},
	}
	embedder := &fakeBenchmarkEmbedder{}
	cases := []goldCase{
		{ID: "one", Question: "alpha", GoldPaths: []string{"projects/alpha.md"}},
		{ID: "two", Question: "beta", GoldPaths: []string{"projects/beta.md"}},
		{ID: "three", Question: "miss", GoldPaths: []string{"projects/wanted.md"}},
		{ID: "skip", Question: "no gold"},
		{ID: "error", Question: "broken", GoldPaths: []string{"projects/broken.md"}},
	}
	deps := successfulRunDependencies(store, embedder, cases)

	var stdout, stderr bytes.Buffer
	code := runCLI("recall-bench", []string{
		"--wiki", "wiki-copy", "--diary", "diary-copy", "--gold", "gold.jsonl", "--k", "2", "-v",
	}, &stdout, &stderr, deps)

	if code != 0 || stderr.String() != bm25DegradedWarning {
		t.Fatalf("exit=%d stderr=%q", code, stderr.String())
	}
	for _, want := range []string{
		"== recall-bench  fusion=rrf(default)  graph_boost=false  semantic=false  K=2  cases=5\n",
		"gold=[projects/wanted.md]  got=[projects/wrong-a.md projects/wrong-b.md projects/wrong-c.md]",
		"recall-bench: 1 search error(s) excluded from the metric\n",
		"RECALL_BENCH hit@1=1 hit@2=2 total=3 p@1=33.3% r@2=66.7% mrr=0.500 fusion=rrf(default)\n",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout missing %q:\n%s", want, stdout.String())
		}
	}
	if !store.closed {
		t.Fatal("store was not closed after successful run")
	}
	if embedder.healthChecks != 41 {
		t.Fatalf("embedding health checks = %d, want 41", embedder.healthChecks)
	}
}

func TestRunCLIMatrixComparesAllRetrievalStagesWithLatency(t *testing.T) {
	store := &fakeBenchmarkStore{results: map[string][]wiki.SearchResult{
		"alpha": {{Path: "projects/alpha.md"}},
	}}
	deps := successfulRunDependencies(store, &fakeBenchmarkEmbedder{}, []goldCase{{
		ID: "one", Question: "alpha", GoldPaths: []string{"projects/alpha.md"},
	}})

	var stdout, stderr bytes.Buffer
	code := runCLI("recall-bench", []string{"--wiki", "wiki-copy", "--matrix"}, &stdout, &stderr, deps)
	if code != 0 || stderr.String() != bm25DegradedWarning {
		t.Fatalf("exit=%d stderr=%q", code, stderr.String())
	}
	for _, mode := range []wiki.SearchMode{wiki.SearchModeBM25, wiki.SearchModeSemantic, wiki.SearchModeHybrid, wiki.SearchModeFull} {
		if !strings.Contains(stdout.String(), "RECALL_BENCH_MATRIX mode="+string(mode)) {
			t.Fatalf("matrix output missing mode %s:\n%s", mode, stdout.String())
		}
	}
	if !strings.Contains(stdout.String(), "p50_ms=") || !strings.Contains(stdout.String(), "p95_ms=") {
		t.Fatalf("matrix latency percentiles missing:\n%s", stdout.String())
	}
	wantCalls := []wiki.SearchMode{wiki.SearchModeBM25, wiki.SearchModeSemantic, wiki.SearchModeHybrid, wiki.SearchModeFull}
	if len(store.optionCalls) != len(wantCalls) {
		t.Fatalf("mode calls = %v, want %v", store.optionCalls, wantCalls)
	}
	for i := range wantCalls {
		if store.optionCalls[i] != wantCalls[i] {
			t.Fatalf("mode calls = %v, want %v", store.optionCalls, wantCalls)
		}
	}
}

func TestRunCLIPreservesSemanticWarmFailureAndCleanup(t *testing.T) {
	t.Parallel()
	store := &fakeBenchmarkStore{warmErr: errors.New("warm failed")}
	embedder := &fakeBenchmarkEmbedder{healthy: true}
	deps := successfulRunDependencies(store, embedder, nil)
	deps.loadCases = func(string) ([]goldCase, int, error) {
		t.Fatal("gold loaded after semantic warm failure")
		return nil, 0, nil
	}

	var stdout, stderr bytes.Buffer
	code := runCLI("recall-bench", []string{"--wiki", "wiki-copy"}, &stdout, &stderr, deps)

	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	want := "recall-bench: semantic warm FAILED (warm failed) — metrics would be BM25-degraded\n"
	if stderr.String() != want {
		t.Fatalf("stderr = %q, want %q", stderr.String(), want)
	}
	if !store.closed || store.embedder != embedder {
		t.Fatalf("cleanup/embedder: closed=%v embedder=%T", store.closed, store.embedder)
	}
}

func TestRunCLIPreservesGoldAndZeroScoreErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		cases      []goldCase
		malformed  int
		searchErrs map[string]error
		wantError  string
	}{
		{
			name:      "malformed gold",
			malformed: 2,
			wantError: "recall-bench: 2 malformed gold row(s) in gold.jsonl — fix or remove them\n",
		},
		{
			name:       "zero scored",
			cases:      []goldCase{{ID: "skip"}, {ID: "error", Question: "broken", GoldPaths: []string{"p.md"}}},
			searchErrs: map[string]error{"broken": errors.New("backend down")},
			wantError:  "recall-bench: 0 cases scored (empty/comment-only gold, all rows lack gold_paths, or 1 search error(s))\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &fakeBenchmarkStore{searchErrs: tt.searchErrs}
			deps := successfulRunDependencies(store, &fakeBenchmarkEmbedder{}, tt.cases)
			deps.loadCases = func(string) ([]goldCase, int, error) {
				return tt.cases, tt.malformed, nil
			}
			var stdout, stderr bytes.Buffer
			code := runCLI("recall-bench", []string{"--wiki", "wiki-copy", "--gold", "gold.jsonl"}, &stdout, &stderr, deps)
			if code != 1 || stderr.String() != bm25DegradedWarning+tt.wantError {
				t.Fatalf("exit=%d stderr=%q", code, stderr.String())
			}
			if !store.closed {
				t.Fatal("store was not closed on error")
			}
		})
	}
}

func TestResolveFusionReturnsEffectiveGraphState(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, fusion, graph string
		semantic            bool
		wantFusion          string
		wantGraph           bool
	}{
		{name: "default semantic", semantic: true, wantFusion: "rrf(default)", wantGraph: true},
		{name: "lexical fallback", semantic: false, wantFusion: "rrf(default)", wantGraph: false},
		{name: "additive ignores graph", fusion: "additive", semantic: true, wantFusion: "additive", wantGraph: false},
		{name: "explicit graph off", graph: "off", semantic: true, wantFusion: "rrf(default)", wantGraph: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			getenv := func(key string) string {
				if key == "DENEB_WIKI_FUSION" {
					return tt.fusion
				}
				return tt.graph
			}
			fusion, graph := resolveFusion(getenv, tt.semantic)
			if fusion != tt.wantFusion || graph != tt.wantGraph {
				t.Fatalf("resolveFusion() = (%q, %v), want (%q, %v)", fusion, graph, tt.wantFusion, tt.wantGraph)
			}
		})
	}
}

type fakeBenchmarkStore struct {
	results     map[string][]wiki.SearchResult
	searchErrs  map[string]error
	warmErr     error
	embedder    wiki.Embedder
	closed      bool
	optionCalls []wiki.SearchMode
}

func (s *fakeBenchmarkStore) SearchWithOptions(ctx context.Context, query string, limit int, options wiki.QueryOptions) (wiki.SearchReport, error) {
	s.optionCalls = append(s.optionCalls, options.Mode)
	results, err := s.Search(ctx, query, limit)
	return wiki.SearchReport{Results: results}, err
}

func (s *fakeBenchmarkStore) Close() error {
	s.closed = true
	return nil
}

func (s *fakeBenchmarkStore) SetEmbedder(embedder wiki.Embedder) {
	s.embedder = embedder
}

func (s *fakeBenchmarkStore) WarmSemanticIndex(context.Context) error {
	return s.warmErr
}

func (s *fakeBenchmarkStore) Search(_ context.Context, query string, _ int) ([]wiki.SearchResult, error) {
	if err := s.searchErrs[query]; err != nil {
		return nil, err
	}
	return s.results[query], nil
}

type fakeBenchmarkEmbedder struct {
	healthy      bool
	healthChecks int
}

func (e *fakeBenchmarkEmbedder) Embed(context.Context, []string) ([][]float32, error) {
	return nil, nil
}

func (e *fakeBenchmarkEmbedder) IsHealthy() bool {
	e.healthChecks++
	return e.healthy
}

func successfulRunDependencies(
	store benchmarkStore,
	embedder wiki.Embedder,
	cases []goldCase,
) runDependencies {
	return runDependencies{
		openStore: func(string, string) (benchmarkStore, error) { return store, nil },
		newEmbedder: func(*slog.Logger) wiki.Embedder {
			return embedder
		},
		loadCases: func(string) ([]goldCase, int, error) { return cases, 0, nil },
		getenv:    func(string) string { return "" },
		sleep:     func(time.Duration) {},
	}
}

func unopenedRunDependencies() runDependencies {
	return runDependencies{
		openStore: func(string, string) (benchmarkStore, error) {
			panic("store opened during parse-only CLI path")
		},
	}
}

func TestContentMatcherHitsOnAnswerTokensAndAlternatives(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "프로젝트/pl2-tha-epc-001"), 0o755); err != nil {
		t.Fatal(err)
	}
	page := filepath.Join(dir, "프로젝트/pl2-tha-epc-001/대표.md")
	if err := os.WriteFile(page, []byte("title: 대한전선 당진\nPM 용역 발주서 금액 1.13억 회신."), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newContentMatcher(dir)

	// all tokens present (path has no .md suffix — matcher appends it)
	if !m("프로젝트/pl2-tha-epc-001/대표", []string{"1.13억", "발주서"}) {
		t.Fatal("expected content hit when every token present")
	}
	// alternative satisfied ("없는값|1.13억")
	if !m("프로젝트/pl2-tha-epc-001/대표", []string{"없는값|1.13억"}) {
		t.Fatal("expected hit when one alternative matches")
	}
	// a missing token fails the whole match
	if m("프로젝트/pl2-tha-epc-001/대표", []string{"1.13억", "존재하지않는토큰"}) {
		t.Fatal("expected miss when a required token is absent")
	}
	// empty must_contain never matches; missing page never matches
	if m("프로젝트/pl2-tha-epc-001/대표", nil) {
		t.Fatal("empty must_contain must not match")
	}
	if m("프로젝트/없는페이지", []string{"x"}) {
		t.Fatal("missing page must not match")
	}
}

func TestCaseHitFallsBackToContentWhenPathStale(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "프로젝트/nde-joc-cbl-001"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "프로젝트/nde-joc-cbl-001/대표.md"), []byte("JOCA 케이블 8,887 발주"), 0o644); err != nil {
		t.Fatal(err)
	}
	// gold points at the OLD (renamed-away) path; search returns the new code folder.
	c := goldCase{GoldPaths: []string{"프로젝트/거래/joca"}, MustContain: []string{"8,887"}}
	res := wiki.SearchResult{Path: "프로젝트/nde-joc-cbl-001/대표.md"}

	if caseHit(res, c, nil) {
		t.Fatal("path-only must NOT hit a stale-path case")
	}
	if !caseHit(res, c, newContentMatcher(dir)) {
		t.Fatal("content matcher must rescue a stale-path case whose page holds the answer")
	}
	if findGoldRank([]wiki.SearchResult{res}, c, 8, newContentMatcher(dir)) != 0 {
		t.Fatal("findGoldRank must rank the content-hit page at 0")
	}
}

func TestLifecycleMetricsScoreStaleAndLeakExposureInTopK(t *testing.T) {
	dir := t.TempDir()
	pages := map[string]string{
		"프로젝트/new.md":  "계약금액 2.5억 확정, 담당 김현우.",
		"업무/old.md":    "계약금액 1.0억 (구버전).",
		"프로젝트/etc.md":  "무관한 회의 메모.",
		"프로젝트/leak.md": "금지된 옛담당 박민수 언급.",
	}
	for rel, body := range pages {
		full := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	results := map[string][]wiki.SearchResult{
		"stale-exposed": {{Path: "프로젝트/new.md"}, {Path: "업무/old.md"}},
		"stale-clean":   {{Path: "프로젝트/new.md"}, {Path: "프로젝트/etc.md"}},
		"leak-exposed":  {{Path: "프로젝트/new.md"}, {Path: "프로젝트/leak.md"}},
		// The stale page sits BELOW the K=2 window — must not count as exposed.
		"stale-below-k": {{Path: "프로젝트/new.md"}, {Path: "프로젝트/etc.md"}, {Path: "업무/old.md"}},
	}
	cases := []goldCase{
		{ID: "u1", Question: "stale-exposed", GoldPaths: []string{"프로젝트/new"}, OpType: "update", StaleValues: []string{"1.0억"}},
		{ID: "u2", Question: "stale-clean", GoldPaths: []string{"프로젝트/new"}, OpType: "update", StaleValues: []string{"1.0억"}},
		{ID: "u3", Question: "stale-below-k", GoldPaths: []string{"프로젝트/new"}, OpType: "update", StaleValues: []string{"1.0억"}},
		{ID: "f1", Question: "leak-exposed", GoldPaths: []string{"프로젝트/new"}, OpType: "forget", MustNot: []string{"없는값|박민수"}},
	}
	search := func(_ context.Context, query string, _ int) ([]wiki.SearchResult, error) {
		return results[query], nil
	}
	content, holds := newContentScorers(dir)

	res := evaluateCasesWithSearch(context.Background(), cases, 2, false, io.Discard, search, content, holds)
	if res.staleCases != 3 || res.staleExposed != 1 {
		t.Fatalf("stale = %d/%d, want 1/3", res.staleExposed, res.staleCases)
	}
	if res.leakCases != 1 || res.leakExposed != 1 {
		t.Fatalf("leak = %d/%d, want 1/1", res.leakExposed, res.leakCases)
	}
	if res.opUpdate != 3 || res.opForget != 1 {
		t.Fatalf("op counts = update:%d forget:%d, want 3/1", res.opUpdate, res.opForget)
	}

	var out bytes.Buffer
	writeLifecycleMetrics(&out, "RECALL_BENCH_LIFECYCLE", 2, res)
	want := "RECALL_BENCH_LIFECYCLE k=2 stale_rate=33.3% (1/3) leak_rate=100.0% (1/1) op_update=3 op_forget=1\n"
	if out.String() != want {
		t.Fatalf("lifecycle line = %q, want %q", out.String(), want)
	}

	// nil holder (no --content) must keep lifecycle counters at zero, and the
	// writer must stay silent so existing runs keep byte-identical output.
	plain := evaluateCasesWithSearch(context.Background(), cases, 2, false, io.Discard, search, content, nil)
	if plain.staleCases != 0 || plain.leakCases != 0 || plain.opUpdate != 0 || plain.opForget != 0 {
		t.Fatalf("nil holder must not score lifecycle: %+v", plain)
	}
	out.Reset()
	writeLifecycleMetrics(&out, "RECALL_BENCH_LIFECYCLE", 2, plain)
	if out.Len() != 0 {
		t.Fatalf("lifecycle line must be suppressed without lifecycle cases, got %q", out.String())
	}
}

// A gold case pointing at a path that no longer exists can never be hit, so it
// silently caps the ceiling and reads as a retrieval miss. The 2026-07-19 move to
// code-keyed project folders did exactly that to 29/105 cases — a healthy stack
// scored 63.5% for six days and looked like a regression.
func TestDeadGoldCasesDetectsRetiredPaths(t *testing.T) {
	dir := t.TempDir()
	mustWrite := func(rel string) {
		full := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("---\ntitle: x\n---\n\nbody\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite(filepath.Join("프로젝트", "pl1-gsn-dev-001", "대표.md"))

	cases := []goldCase{
		{ID: "alive", GoldPaths: []string{"프로젝트/pl1-gsn-dev-001"}},
		{ID: "retired", GoldPaths: []string{"프로젝트/군산-옥구읍-수산리"}},
		{ID: "no-gold"}, // cases without gold paths are not "dead", just unscored
	}
	dead, sample := deadGoldCases(dir, cases)
	if len(dead) != 1 || dead[0] != "retired" {
		t.Fatalf("dead = %v, want [retired]", dead)
	}
	if len(sample) != 1 || sample[0] != "retired" {
		t.Errorf("sample = %v, want [retired]", sample)
	}

	// All-alive gold must stay silent — a guard that always fires is ignored.
	if d, _ := deadGoldCases(dir, cases[:1]); len(d) != 0 {
		t.Errorf("healthy gold must not warn, got %v", d)
	}
	// An unwalkable tree must not cry wolf either.
	if d, _ := deadGoldCases(filepath.Join(dir, "nope"), cases); len(d) != 0 {
		t.Errorf("missing wiki dir must stay silent, got %v", d)
	}
}

func TestMismatchedGoldCasesDetectsWrongPage(t *testing.T) {
	dir := t.TempDir()
	mustWrite := func(rel, body string) {
		full := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("---\ntitle: x\n---\n\n"+body+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// The right page holds the answer; an unrelated survivor substring-matches a
	// stale gold path but lacks the answer token.
	mustWrite(filepath.Join("프로젝트", "pl2-tha-epc-002", "대표.md"), "선급금 30% 지급 완료")
	mustWrite(filepath.Join("업무", "비금도-모듈-이슈.md"), "모듈 배치 검토")

	cases := []goldCase{
		{ID: "good", GoldPaths: []string{"프로젝트/pl2-tha-epc-002"}, MustContain: []string{"선급금"}},
		{ID: "wrong-page", GoldPaths: []string{"비금도"}, MustContain: []string{"6/30"}},
		{ID: "path-only", GoldPaths: []string{"비금도"}}, // no must_contain → not judgeable, skipped
	}
	bad, sample := mismatchedGoldCases(dir, cases)
	if len(bad) != 1 || bad[0] != "wrong-page" {
		t.Fatalf("mismatched = %v, want [wrong-page]", bad)
	}
	if len(sample) != 1 || sample[0] != "wrong-page" {
		t.Errorf("sample = %v, want [wrong-page]", sample)
	}

	// A case whose gold path matches NO page is dead, not mismatched — that guard
	// owns it, so this one must stay silent to avoid double-reporting.
	dead := []goldCase{{ID: "dead", GoldPaths: []string{"프로젝트/사라진-폴더"}, MustContain: []string{"x"}}}
	if b, _ := mismatchedGoldCases(dir, dead); len(b) != 0 {
		t.Errorf("dead gold is not mismatched, got %v", b)
	}
	// Healthy gold and an unwalkable tree must both stay silent.
	if b, _ := mismatchedGoldCases(dir, cases[:1]); len(b) != 0 {
		t.Errorf("healthy gold must not warn, got %v", b)
	}
	if b, _ := mismatchedGoldCases(filepath.Join(dir, "nope"), cases); len(b) != 0 {
		t.Errorf("missing wiki dir must stay silent, got %v", b)
	}
}

// The pool probe's whole value is telling a ranking miss apart from a
// generation miss, so the test pins all three outcomes AND the query economy:
// a case that already hit at K must not cost a second search.
func TestPoolCeilingSplitsRankingMissFromGenerationMiss(t *testing.T) {
	cases := []goldCase{
		{ID: "hit", Question: "hit", GoldPaths: []string{"프로젝트/a"}},
		{ID: "buried", Question: "buried", GoldPaths: []string{"프로젝트/b"}},
		{ID: "absent", Question: "absent", GoldPaths: []string{"프로젝트/c"}},
	}
	deep := map[string][]wiki.SearchResult{
		// Gold sits at rank 2 — outside K=2, inside the depth-5 pool.
		"buried": {{Path: "업무/x.md"}, {Path: "업무/y.md"}, {Path: "프로젝트/b.md"}},
		// Gold never appears, even at depth 5.
		"absent": {{Path: "업무/x.md"}, {Path: "업무/y.md"}},
	}
	var deepQueries []string
	search := func(_ context.Context, query string, limit int) ([]wiki.SearchResult, error) {
		if limit != 5 {
			t.Errorf("pool probe must search at the requested depth, got limit=%d", limit)
		}
		deepQueries = append(deepQueries, query)
		return deep[query], nil
	}
	ranks := []caseRank{
		{id: "hit", rank: 0},
		{id: "buried", rank: -1},
		{id: "absent", rank: -1},
	}

	got := evaluatePoolCeiling(context.Background(), cases, ranks, 2, 5, search, nil)

	if got.scored != 3 || got.hitK != 1 || got.rankingMiss != 1 || got.generationMiss != 1 {
		t.Fatalf("split = %+v, want scored=3 hitK=1 rankingMiss=1 generationMiss=1", got)
	}
	if got.inPool != 2 {
		t.Fatalf("inPool = %d, want 2 (the top-K hit plus the buried case)", got.inPool)
	}
	if len(deepQueries) != 2 {
		t.Fatalf("deep searches = %v, want only the two misses (a top-K hit is in the pool by construction)", deepQueries)
	}

	var out bytes.Buffer
	writePoolCeilingResult(&out, 2, 5, got)
	want := "RECALL_BENCH_POOL depth=5 scored=3 pool_recall=66.7% r@2=33.3% ranking_miss=1 generation_miss=1\n"
	if !strings.HasPrefix(out.String(), want) {
		t.Fatalf("pool line = %q, want prefix %q", out.String(), want)
	}
}

// A search error during the probe must drop that case from the split rather
// than silently counting it as a generation miss — the same honesty rule the
// main pass applies to searchErrs.
func TestPoolCeilingExcludesSearchErrors(t *testing.T) {
	cases := []goldCase{{ID: "boom", Question: "boom", GoldPaths: []string{"프로젝트/a"}}}
	search := func(context.Context, string, int) ([]wiki.SearchResult, error) {
		return nil, errors.New("embedder down")
	}

	got := evaluatePoolCeiling(context.Background(), cases, []caseRank{{id: "boom", rank: -1}}, 2, 5, search, nil)

	if got.scored != 0 || got.generationMiss != 0 || got.searchErrs != 1 {
		t.Fatalf("errored probe = %+v, want scored=0 generationMiss=0 searchErrs=1", got)
	}
}

// --pool-depth must be off or genuinely deeper than K; a pool at or below K
// cannot contain a miss the top-K pass already scanned.
func TestPoolDepthValidation(t *testing.T) {
	base := benchmarkConfig{wikiDir: "/tmp/wiki", k: 8, split: "all"}
	for _, depth := range []int{-1, 1, 8} {
		cfg := base
		cfg.poolDepth = depth
		if err := validateBenchmarkConfig(cfg); err == nil {
			t.Errorf("--pool-depth=%d must be rejected against k=%d", depth, base.k)
		}
	}
	for _, depth := range []int{0, 9, 200} {
		cfg := base
		cfg.poolDepth = depth
		if err := validateBenchmarkConfig(cfg); err != nil {
			t.Errorf("--pool-depth=%d must be accepted against k=%d: %v", depth, base.k, err)
		}
	}
}
