package main

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/codesearch"
)

type benchCase struct {
	query string
	want  []string
}

// goldset is the long-lived regression suite for semantic code search. It is
// intentionally separate from wiki/document multi-gold evaluation: these
// labels score code paths/symbols only, using Hit@K and MRR.
var goldset = []benchCase{
	{"프로바이더 429 재시도 백오프", []string{"ai/llm/client"}},
	{"메일 첨부 다운로드", []string{"attachment"}},
	{"세션 상태 전이 검증", []string{"domain/session/lifecycle", "domain/session"}},
	{"임베딩 클라이언트 헬스체크", []string{"ai/embedding/client.go"}},
	{"음성 전사 화자분리", []string{"tools/artifact/asr"}},
	{"위키 회상 RRF 융합", []string{"wiki/search", "knowledge/router", "semhybrid"}},
	{"결재 문서 승인 반려", []string{"approval"}},
	{"프롬프트 캐시 컴팩션", []string{"compact", "prompt_cache"}},
	{"작업 피드 카드 생성", []string{"workfeed"}},
	{"스킬 자동 진화 심사", []string{"genesis/evolver", "skills/genesis"}},
	// Held-out provenance: these were not used for the first tuning pass.
	{"유튜브 자막 추출", []string{"youtube"}},
	{"크론 잡 실행 스케줄", []string{"cron"}},
	{"환율 시세 조회", []string{"morning", "market", "money"}},
}

// hardset pins real misses observed during the 2026-07-20 audit. It covers
// orchestration and non-AST files, not just easy function-name paraphrases.
var hardset = []benchCase{
	{"CodeGraph 검색 결과에 가까운 CLAUDE 문서를 자동 주입", []string{"externalmcp/codegraph_tuning"}},
	{"사용자 질문과 관련된 문서를 매 턴 자동으로 찾아 프롬프트에 넣는 코드", []string{"pipeline/chat/recall/recall_preflight"}},
	{"프롬프트 캐시를 유지하려고 recall을 마지막 사용자 메시지에 넣는 로직", []string{"pipeline/chat/run_tail_inject"}},
	{"배포할 때 시맨틱 코드 인덱스를 자동으로 갱신하는 로직", []string{"scripts/deploy/deploy.sh"}},
	{"문서 청크 임베딩에 파일 경로와 섹션 제목을 붙이는 코드", []string{"domain/filestore/semindex"}},
}

type benchMetrics struct {
	cases   int
	hit1    int
	hit5    int
	hit20   int
	mrr20   float64
	latency []time.Duration
}

func (m *benchMetrics) add(rank int, latency time.Duration) {
	m.cases++
	m.latency = append(m.latency, latency)
	if rank == 1 {
		m.hit1++
	}
	if rank > 0 && rank <= 5 {
		m.hit5++
	}
	if rank > 0 && rank <= 20 {
		m.hit20++
		m.mrr20 += 1 / float64(rank)
	}
}

func runBench(ctx context.Context, dir string, emb codesearch.Embedder) {
	repo := repoRoot()
	rr := reranker()
	rrName := "none"
	if rr != nil {
		rrName = rr.Identity()
	}
	runBenchSuite(ctx, repo, dir, emb, rr, rrName, "regression", goldset)
	fmt.Println()
	runBenchSuite(ctx, repo, dir, emb, rr, rrName, "hard", hardset)
}

func runBenchSuite(
	ctx context.Context,
	repo, dir string,
	emb codesearch.Embedder,
	rr codesearch.Reranker,
	rrName, name string,
	cases []benchCase,
) {
	base, reranked := benchMetrics{}, benchMetrics{}
	fmt.Printf("== code-search bench suite=%s cases=%d reranker=%s\n", name, len(cases), rrName)
	for _, c := range cases {
		started := time.Now()
		hits, err := codesearch.Search(ctx, dir, emb, c.query, 20)
		baseLatency := time.Since(started)
		if err != nil {
			fatal(err)
		}
		started = time.Now()
		ranked, err := codesearch.SearchRanked(ctx, repo, dir, emb, rr, c.query, 20)
		rrLatency := time.Since(started)
		if err != nil {
			fatal(err)
		}
		baseRank := firstGoldRank(hits, c.want)
		rrRank := firstGoldRank(ranked, c.want)
		base.add(baseRank, baseLatency)
		reranked.add(rrRank, rrLatency)
		mark := "MISS"
		if rrRank > 0 {
			mark = "hit "
		}
		top := ""
		topRR, goldRR := 0.0, 0.0
		if len(ranked) > 0 {
			top = ranked[0].File
			topRR = ranked[0].RerankScore
		}
		if rrRank > 0 {
			goldRR = ranked[rrRank-1].RerankScore
		}
		fmt.Printf("%s  %-34s rank(base/rr)=%d/%d rr(top/gold)=%.3f/%.3f top1=%s\n",
			mark, c.query, baseRank, rrRank, topRR, goldRR, top)
	}
	printMetrics("base", base)
	printMetrics("reranked("+rrName+")", reranked)
}

func printMetrics(label string, m benchMetrics) {
	mrr := 0.0
	if m.cases > 0 {
		mrr = m.mrr20 / float64(m.cases)
	}
	latencies := append([]time.Duration(nil), m.latency...)
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	p50, p95 := time.Duration(0), time.Duration(0)
	if len(latencies) > 0 {
		p50 = latencies[(len(latencies)-1)/2]
		p95 = latencies[min(len(latencies)-1, (len(latencies)*95+99)/100-1)]
	}
	fmt.Printf("%-24s Hit@1=%d/%d Hit@5=%d/%d Hit@20=%d/%d MRR@20=%.3f latency(p50/p95)=%s/%s\n",
		label, m.hit1, m.cases, m.hit5, m.cases, m.hit20, m.cases, mrr,
		p50.Round(time.Millisecond), p95.Round(time.Millisecond))
}

func firstGoldRank(hits []codesearch.Hit, want []string) int {
	for i, hit := range hits {
		if isGold(hit, want) {
			return i + 1
		}
	}
	return 0
}

func isGold(h codesearch.Hit, want []string) bool {
	for _, w := range want {
		if strings.Contains(strings.ToLower(h.File), strings.ToLower(w)) ||
			strings.Contains(strings.ToLower(h.Qualified), strings.ToLower(w)) {
			return true
		}
	}
	return false
}
