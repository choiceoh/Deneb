package genesis

import (
	"context"
	"fmt"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

// self_correction_judge.go — the triage judge that answers the Trust Inbox's
// approve/reject question so the operator does not have to.
//
// Operator directive: self-correction candidates are judged by an LLM, not by a
// person. The low-confidence tie-break judge (meta_lowconf_judge.go) closed the
// same open-at-the-human-step loop for meta-evolution proposals; this closes it
// for the candidate inbox, which was still posting one card per NEW proposed
// candidate unconditionally.
//
// The judge is triage, NOT a landing gate. "accepted" means only "approved to
// try": a scope=code candidate still goes through coding-dispatch, CI, the
// deploy watch, and the rollback watch before anything is durable. That is why
// an LLM is admissible here while "LLM scoring only" stays ruled out for the
// skill acceptance gate — nothing downstream is being replaced.
//
// Abstention is a first-class answer. Forcing a verdict on every candidate
// would trade one failure (a loop stalled at the human step) for a worse one
// (a loop that decides confidently about things it cannot see). An abstain
// falls through to the operator card exactly as before.

// SelfCorrectionVerdict is the judge's answer. The zero value abstains, so any
// path that fails to produce a real verdict lands on the operator card rather
// than on a silent accept or reject.
type SelfCorrectionVerdict struct {
	// Decided is false when the judge declines to rule.
	Decided bool
	Accept  bool
	// Rationale is stored as the review note, so the ledger records WHY a
	// candidate was accepted or dropped without a person in the loop.
	Rationale string
}

// selfCorrectionJudgePrompt asks about whether the correction is worth
// attempting, not whether it is correct — correctness is what the downstream
// gates establish.
const selfCorrectionJudgePrompt = `당신은 Deneb 자가개선 루프의 자기교정 후보 심사자다.
사람 대신 이 후보를 승인할지 기각할지 판정한다.

**승인(accept)은 "시도해도 좋다"는 뜻이지 "옳다"는 뜻이 아니다.** 승인된 코드 후보는
배차 레인 → CI → 배포 감시 → 롤백 감시를 모두 거친다. 즉 당신은 정확성을 최종
판정하지 않는다. 당신이 답할 것은 하나다: **이 후보에 배차 한 번을 쓸 값이 있는가?**

승인 근거가 되는 것:
- 근거가 구체적이다 — 실제 실패·로그·재발 관측을 가리킨다
- 제안된 변경이 그 근거에 실제로 대응한다
- 범위가 좁고 되돌리기 쉽다

기각 근거가 되는 것:
- 근거가 추측이거나 "그럴 수 있다" 수준이다
- 제안이 근거와 무관하거나 근거보다 훨씬 넓다
- 이미 다른 방식으로 해결됐다고 볼 정황이 뚜렷하다
- 위험이 이득보다 명백히 크다

**판단할 근거가 부족하면 decided=false로 기권하라.** 기권은 실패가 아니라
사람에게 넘긴다는 뜻이다. 억지로 고르는 것보다 낫다.

JSON만 출력: {"decided": true|false, "accept": true|false, "rationale": "한국어 한두 문장, 무엇을 근거로 갈랐는지"}`

type selfCorrectionJudgeReply struct {
	Decided   bool   `json:"decided"`
	Accept    bool   `json:"accept"`
	Rationale string `json:"rationale"`
}

// NewSelfCorrectionJudge builds the triage judge. Returns nil when the client
// or model is missing, which leaves the operator card in place rather than
// silently disabling review altogether.
func NewSelfCorrectionJudge(
	client *llm.Client,
	model string,
	thinking func(string) *llm.ThinkingConfig,
) func(context.Context, SelfCorrectionCandidateRecord) (SelfCorrectionVerdict, error) {
	if client == nil || strings.TrimSpace(model) == "" {
		return nil
	}
	return func(ctx context.Context, rec SelfCorrectionCandidateRecord) (SelfCorrectionVerdict, error) {
		var think *llm.ThinkingConfig
		if thinking != nil {
			think = thinking(model)
		}
		raw, err := client.Complete(ctx, llm.ChatRequest{
			Model:          model,
			Messages:       []llm.Message{llm.NewTextMessage("user", selfCorrectionJudgeUser(rec))},
			System:         llm.SystemString(selfCorrectionJudgePrompt),
			MaxTokens:      2048,
			Temperature:    evolveTemperature(),
			Thinking:       think,
			ResponseFormat: &llm.ResponseFormat{Type: "json_object"},
		})
		if err != nil {
			return SelfCorrectionVerdict{}, err
		}
		reply, err := jsonutil.UnmarshalLLM[selfCorrectionJudgeReply](raw)
		if err != nil {
			return SelfCorrectionVerdict{}, err
		}
		rationale := strings.TrimSpace(reply.Rationale)
		// A verdict with no reasoning is not a verdict. The rationale becomes
		// the ledger's only record of why a candidate closed without a person,
		// so an empty one abstains rather than writing an unexplained decision.
		if reply.Decided && rationale == "" {
			return SelfCorrectionVerdict{}, nil
		}
		return SelfCorrectionVerdict{
			Decided:   reply.Decided,
			Accept:    reply.Accept,
			Rationale: rationale,
		}, nil
	}
}

func selfCorrectionJudgeUser(rec SelfCorrectionCandidateRecord) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## 후보\n제목: %s\n범위: %s\n", rec.Title, orNone(rec.Scope))
	if rec.SkillName != "" {
		fmt.Fprintf(&b, "대상 스킬: %s\n", rec.SkillName)
	}
	if len(rec.TargetFiles) > 0 {
		fmt.Fprintf(&b, "대상 파일: %s\n", strings.Join(rec.TargetFiles, ", "))
	}
	fmt.Fprintf(&b, "\n## 근거\n%s\n", orNone(truncateForJudge(rec.Evidence)))
	fmt.Fprintf(&b, "\n## 제안 변경\n%s\n", orNone(truncateForJudge(rec.ProposedChange)))
	if rec.Risk != "" {
		fmt.Fprintf(&b, "\n## 제안자가 밝힌 위험\n%s\n", truncateForJudge(rec.Risk))
	}
	return b.String()
}
