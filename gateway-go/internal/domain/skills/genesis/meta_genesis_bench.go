package genesis

// Genesis shadow bench — the P2 epoch gate for GENESIS-prompt revisions
// (RSI P5-4 slice 2: the third artifact in the slow-loop rotation).
//
// A genesis-prompt revision changes what every future NEW skill looks like,
// so its fitness is measured by what it produces: replay fixed session
// scenarios through the incumbent and the proposed system prompt and score
// both outputs with the production admissibility gate
// (generation.BenchAdmissibility — parse + specificity issues). Scenarios are
// COMPILED fixtures, identical for both prompts, so the comparison is fair
// and deterministic; the LLM only executes the two prompts. A revision that
// flips a scenario the incumbent handles cleanly (produces an
// issue-carrying skill, or skips a known skill-worthy session) is rejected;
// so is a mean gate-issue regression beyond noise.

import (
	"context"
	"fmt"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis/generation"
)

// genesisBenchIssueEpsilon tolerates generation noise on the mean gate-issue
// count; flips have zero tolerance.
const genesisBenchIssueEpsilon = 0.5

// genesisShadowScenario is one replayable genesis scenario.
type genesisShadowScenario struct {
	Label      string
	UserPrompt string
}

// genesisScenarioPrompt mirrors generation.Service.Generate's user-prompt
// shape byte-for-byte, so the bench exercises the same input format
// production sees. The existing-skill list is FIXED inside the fixture (the
// live catalog would make runs non-deterministic).
func genesisScenarioPrompt(key, label, tools string, turns int, existing, transcript string) string {
	return fmt.Sprintf(`## 완료된 세션 정보
- 세션 키: %s
- 라벨: %s
- 도구 사용 요약: %s
- 에이전트 턴 수: %d

## 기존 스킬 목록 (중복 방지)
%s

## 대화 내용 (요약)
%s`, key, label, tools, turns, existing, transcript)
}

// genesisShadowScenarios are the compiled fixtures: three skill-worthy
// sessions (rich multi-tool workflows with trial-and-error and user
// corrections — exactly what the extraction criteria call for). Ground truth
// is NOT "a skill must be produced" — it is the relative comparison under
// the deterministic gate; a competent prompt produces a clean skill from
// each of these, so a revision that skips or degrades one is measurably
// worse on a known-good input.
func genesisShadowScenarios() []genesisShadowScenario {
	existing := "pr-merge-flow, weekly-report-brief, deploy-gateway"
	return []genesisShadowScenario{
		{
			Label: "mail-to-wiki",
			UserPrompt: genesisScenarioPrompt("bench:mail-wiki", "발주 메일 위키 반영",
				"gmail_search(3회), wiki(3회), recall(1회)", 9, existing,
				`사용자가 "지난주 발주 메일들 위키에 정리해줘"라고 요청. 처음에 gmail_search를 제목 키워드로만 돌려 2건을 놓쳤고, 사용자 교정("보낸사람 도메인으로 걸러") 후 발신 도메인 필터로 재검색해 5건 전부 수집. 각 메일에서 품목/수량/납기를 추출해 위키 거래처 페이지에 반영하는데, 신규 페이지를 만들 뻔한 것을 recall로 기존 페이지를 먼저 확인해 중복 생성을 피하고 기존 페이지에 append 했다. 마지막에 위키를 재조회해 5건 모두 반영됐는지 확인했고, 사용자가 "다음에도 이 순서로"라고 확정. 절차: ① 발신 도메인으로 메일 검색 ② recall로 기존 위키 페이지 확인 ③ 품목/수량/납기 추출해 append ④ 재조회 검증.`),
		},
		{
			Label: "weekly-report",
			UserPrompt: genesisScenarioPrompt("bench:weekly-report", "주간 실적 보고서 작성",
				"wiki(4회), notebook(1회)", 11, existing,
				`매주 반복되는 실적 보고서 작성 세션. 위키에서 거래처별 주간 페이지 3종을 수집해 수치를 대조하고 표로 정리했다. 첫 시도에서 단위가 섞여(kW/MW) 사용자 교정을 받았고, 이후 모든 수치를 MW로 통일하는 규칙을 확립. 두 번째 교정은 정렬 순서(금액 내림차순)였다. 발송 전 합계를 원본 페이지 수치와 재대조해 1건의 전기 오류를 잡았다. 확립된 절차: ① 위키 주간 페이지 3종 수집 ② 단위 MW 통일 ③ 금액 내림차순 표 작성 ④ 합계 재대조 검증 ⑤ notebook에 초안 저장. 사용자가 "이대로 매주 하자"고 확정.`),
		},
		{
			Label: "quote-crosscheck",
			UserPrompt: genesisScenarioPrompt("bench:quote-crosscheck", "거래처 견적 단가 대조",
				"mailarchive_search(2회), wiki(2회)", 8, existing,
				`거래처 견적 메일의 단가를 위키 단가표와 대조하는 세션. mailarchive_search로 견적 메일을 찾았는데 첫 검색은 구버전 첨부가 걸려 사용자가 "최신 것만"이라 교정 — 날짜 내림차순 첫 건만 쓰는 규칙 확립. 위키 단가표 페이지와 품목별로 대조해 2개 품목의 단가 인상을 발견하고 위키에 변경 이력을 기록했다. 함정: 같은 품목이 규격 표기만 다르게 두 번 적힌 경우가 있어 규격 정규화 후 대조해야 한다. 절차: ① 최신 견적 메일 1건 선별 ② 위키 단가표 조회 ③ 규격 정규화 후 품목별 대조 ④ 변동분 위키 기록.`),
		},
	}
}

// GenesisBenchOutcome aggregates the shadow replay over all scenarios.
type GenesisBenchOutcome struct {
	Scenarios       int      `json:"scenarios"`       // scored on BOTH sides (non-skip, parseable)
	Flips           int      `json:"flips"`           // incumbent clean → proposal skip/issue
	IncumbentIssues float64  `json:"incumbentIssues"` // mean gate issues over scored scenarios
	ProposalIssues  float64  `json:"proposalIssues"`
	IncumbentSkips  int      `json:"incumbentSkips"`
	ProposalSkips   int      `json:"proposalSkips"`
	Notes           []string `json:"notes,omitempty"`
}

// genesisShadowGenFn generates ONE genesis response with an explicit system
// prompt. Injectable so the bench is testable without an LLM; production
// wires generation.Service.ShadowGenerate (the real genesis model).
type genesisShadowGenFn func(ctx context.Context, systemPrompt, userPrompt string) (string, error)

// runGenesisShadowBench replays every scenario through both prompts and
// scores the outputs with the production admissibility gate. A scenario
// enters the mean-issue comparison only when BOTH prompts produced a
// parseable non-skip skill; skips are counted per side, and a proposal that
// skips or degrades a scenario the incumbent handles CLEANLY is a flip.
func runGenesisShadowBench(ctx context.Context, incumbentPrompt, proposalPrompt string, scenarios []genesisShadowScenario, gen genesisShadowGenFn) GenesisBenchOutcome {
	var out GenesisBenchOutcome
	var incSum, propSum int
	for _, sc := range scenarios {
		incText, err := gen(ctx, incumbentPrompt, sc.UserPrompt)
		if err != nil {
			out.Notes = append(out.Notes, fmt.Sprintf("%s: incumbent generation error: %v", sc.Label, err))
			continue
		}
		propText, err := gen(ctx, proposalPrompt, sc.UserPrompt)
		if err != nil {
			out.Notes = append(out.Notes, fmt.Sprintf("%s: proposal generation error: %v", sc.Label, err))
			continue
		}
		incSkip, incIssues, incErr := generation.BenchAdmissibility(incText)
		propSkip, propIssues, propErr := generation.BenchAdmissibility(propText)
		if incErr != nil || propErr != nil {
			out.Notes = append(out.Notes, fmt.Sprintf("%s: unparsable output (incumbent=%v proposal=%v)", sc.Label, incErr, propErr))
			continue
		}
		if incSkip {
			out.IncumbentSkips++
		}
		if propSkip {
			out.ProposalSkips++
		}
		incClean := !incSkip && len(incIssues) == 0
		propClean := !propSkip && len(propIssues) == 0
		if incClean && !propClean {
			out.Flips++
			detail := "skipped a skill-worthy session"
			if !propSkip {
				detail = fmt.Sprintf("gate issues: %v", propIssues)
			}
			out.Notes = append(out.Notes, fmt.Sprintf("%s: flip — incumbent clean, proposal %s", sc.Label, detail))
		}
		if !incSkip && !propSkip {
			out.Scenarios++
			incSum += len(incIssues)
			propSum += len(propIssues)
		}
	}
	if out.Scenarios > 0 {
		out.IncumbentIssues = float64(incSum) / float64(out.Scenarios)
		out.ProposalIssues = float64(propSum) / float64(out.Scenarios)
	}
	if len(out.Notes) > 4 {
		out.Notes = out.Notes[:4]
	}
	return out
}

// genesisBenchDecision is the deterministic promotion rule for a
// genesis-epoch proposal. Flips reject regardless of the scenario count (a
// proposal-side skip on a clean incumbent scenario never enters the scored
// set). Zero scored scenarios otherwise returns "" — the low-confidence
// routing surfaces those to the operator instead of auto-adopting.
func genesisBenchDecision(out GenesisBenchOutcome) string {
	if out.Flips > 0 {
		return fmt.Sprintf("genesis shadow flipped %d clean scenario(s): %s",
			out.Flips, joinNotes(out.Notes))
	}
	if out.Scenarios == 0 {
		return ""
	}
	if out.ProposalIssues > out.IncumbentIssues+genesisBenchIssueEpsilon {
		return fmt.Sprintf("genesis shadow mean gate issues regressed (%.2f > incumbent %.2f + %.1f)",
			out.ProposalIssues, out.IncumbentIssues, genesisBenchIssueEpsilon)
	}
	return ""
}

// joinNotes compacts bench notes for a rejection reason.
func joinNotes(notes []string) string {
	if len(notes) == 0 {
		return "(no notes)"
	}
	joined := notes[0]
	for _, n := range notes[1:] {
		joined += "; " + n
	}
	return joined
}
