package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/codesearch"
)

// goldset: 개념 질의 → 정답으로 인정할 파일 경로 조각들 (any-match).
// 수작성 — 이 레포의 실제 소유 지식으로 고정한 P@5 벤치.
var goldset = []struct {
	query string
	want  []string
}{
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
}

func runBench(ctx context.Context, dir string, emb codesearch.Embedder) {
	hitAt5 := 0
	for _, g := range goldset {
		hits, err := codesearch.Search(ctx, dir, emb, g.query, 5)
		if err != nil {
			fatal(err)
		}
		ok := false
		for _, h := range hits {
			for _, w := range g.want {
				if strings.Contains(strings.ToLower(h.File), strings.ToLower(w)) ||
					strings.Contains(strings.ToLower(h.Qualified), strings.ToLower(w)) {
					ok = true
				}
			}
		}
		mark := "MISS"
		if ok {
			hitAt5++
			mark = "hit "
		}
		top := ""
		if len(hits) > 0 {
			top = hits[0].File
		}
		fmt.Printf("%s  %-24s top1=%s\n", mark, g.query, top)
	}
	fmt.Printf("\nP@5(any-gold): %d/%d\n", hitAt5, len(goldset))
}
