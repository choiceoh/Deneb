package genesis

import (
	"context"
	"fmt"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

// meta_lowconf_judge.go — the tie-break judge for meta-evolution proposals the
// deterministic bench CLEARED but could not RANK.
//
// The gate is unchanged. Contract + epoch bench still reject on their own and
// still auto-adopt anything they can show improving; this judge is only reached
// on "bench margin <= 0 — no measurable improvement", the one outcome the
// deterministic chain explicitly declines to decide.
//
// That outcome used to become an operator card. Measured 2026-08-26, that is
// where the RSI loop stalled: skill-judge-system-prompt.md sat pending from
// 08-03 until it expired without a verdict, the self-coding nudger reached
// ignoredStreak=5 re-asking about one proposal from 07-12, and the feed held
// 1,249 unread cards. Generation was healthy the whole time — the loop was open
// at the human step, so nothing it produced ever closed.
//
// Why an LLM is admissible HERE specifically, when "LLM scoring only" was ruled
// out for the acceptance gate: this judge cannot accept anything the bench
// rejected, and cannot reject anything the bench auto-adopts. It sees only ties.
// A wrong call costs one neutral-rated revision, which the revert watch and the
// feed card's post-hoc veto both still cover.

// lowConfidenceJudgePrompt asks for a decision about VALUE, not correctness —
// correctness is what the bench already established. The model is told the
// bench found no measurable difference so it does not try to re-derive one.
const lowConfidenceJudgePrompt = `당신은 자가개선 루프의 타이브레이크 판정자다.

이미 결정적 벤치가 이 개정안을 통과시켰다. 즉 **정확성·계약 위반은 쟁점이 아니다**.
벤치가 못 한 것은 단 하나 — 개정안이 현행보다 나은지 **순위를 매기는 것**이다.
벤치의 판단은 "측정 가능한 개선 없음"이었다.

따라서 당신이 답할 질문은 이것뿐이다:
**측정상 중립인 이 변경을 그래도 채택할 가치가 있는가?**

채택(adopt=true) 쪽 근거가 되는 것:
- 같은 동작을 더 명확·간결하게 표현해 이후 개정이 쉬워진다
- 실패 모드나 경계 조건을 명시적으로 다룬다(벤치 코퍼스가 안 건드리는 부분)
- 현행의 모순·중복·죽은 지시를 제거한다

기각(adopt=false) 쪽 근거가 되는 것:
- 표현만 바꾸고 실질이 같다(순수 리라이트)
- 더 길고 모호해진다
- 벤치가 못 본 영역에서 위험을 새로 들인다
- 근거가 "다르다"뿐이고 "낫다"가 아니다

애매하면 기각한다. 중립 변경을 쌓는 것보다 현행 유지가 싸다.

JSON만 출력: {"adopt": true|false, "rationale": "한국어 한두 문장, 무엇을 근거로 갈랐는지"}`

type lowConfJudgeReply struct {
	Adopt     bool   `json:"adopt"`
	Rationale string `json:"rationale"`
}

// NewLowConfidenceJudge builds the tie-break judge from an LLM client. Returns
// nil when the client or model is missing, which leaves the operator-card
// fallback in place rather than silently disabling the routing.
func NewLowConfidenceJudge(client *llm.Client, model string, thinking func(string) *llm.ThinkingConfig) func(context.Context, LowConfidenceCase) (LowConfidenceVerdict, error) {
	if client == nil || strings.TrimSpace(model) == "" {
		return nil
	}
	return func(ctx context.Context, in LowConfidenceCase) (LowConfidenceVerdict, error) {
		var think *llm.ThinkingConfig
		if thinking != nil {
			think = thinking(model)
		}
		user := fmt.Sprintf(`## 대상
아티팩트: %s
에폭: %s

## 개정 사유(제안자가 밝힌 것)
%s

## 벤치가 순위를 못 매긴 이유
%s

## 현행
%s

## 개정안
%s`,
			in.Artifact, in.Epoch, orNone(in.Reason), orNone(in.Margin),
			truncateForJudge(in.Incumbent), truncateForJudge(in.Proposal))

		raw, err := client.Complete(ctx, llm.ChatRequest{
			Model:          model,
			Messages:       []llm.Message{llm.NewTextMessage("user", user)},
			System:         llm.SystemString(lowConfidenceJudgePrompt),
			MaxTokens:      2048,
			Temperature:    evolveTemperature(),
			Thinking:       think,
			ResponseFormat: &llm.ResponseFormat{Type: "json_object"},
		})
		if err != nil {
			return LowConfidenceVerdict{}, err
		}
		reply, err := jsonutil.UnmarshalLLM[lowConfJudgeReply](raw)
		if err != nil {
			return LowConfidenceVerdict{}, err
		}
		return LowConfidenceVerdict{Adopt: reply.Adopt, Rationale: strings.TrimSpace(reply.Rationale)}, nil
	}
}

// truncateForJudge bounds each artifact body. These are system prompts, not
// documents; a runaway one must not push the other side out of the window.
func truncateForJudge(s string) string {
	const limit = 12000
	r := []rune(s)
	if len(r) <= limit {
		return s
	}
	return string(r[:limit]) + "\n…(이하 생략)"
}

func orNone(s string) string {
	if strings.TrimSpace(s) == "" {
		return "(없음)"
	}
	return s
}
