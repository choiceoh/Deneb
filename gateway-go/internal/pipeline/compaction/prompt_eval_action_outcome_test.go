package compaction

// Action-outcome neutrality evaluation for the compaction summarizer.
//
// Why: the summary is fed back as PreviousSummary on the next recompaction, so
// a false completion written once — "[확실] 김부장에게 회신 메일 발송" because
// the assistant SAID "보냈습니다" right after the tool RETURNED an error, or
// because it promised an action it never called — hardens into a fact the
// agent stops re-checking. antifabrication_guard_test.go guards the
// user-decision axis of that failure (unconfirmed proposals promoted to
// decisions); this file guards the agent-action axis: an outcome is what the
// tool returned, not what the assistant claimed. The neutral-wording contract
// is a clean-room port of google/artemis' step-summarizer rule (2026-09
// review), scoped to Deneb's Korean fact skeleton.
//
// Layers:
//   - fixture: one 비서 turn with a tool error the assistant misreports as done,
//     a promised-but-never-called action, a claimed-but-never-called action, and
//     one genuinely successful action whose id must survive (neutral wording
//     must not suppress real outcomes);
//   - deterministic scorer (no model): false completions, preservation of the
//     error and the success, and open-loop retention — validated in CI against
//     hand-written neutral/false summaries so the scorer is proven to
//     discriminate;
//   - live A/B (DENEB_COMPACT_EVAL=1; knobs documented in prompt_eval_test.go):
//     the fresh and the recompaction prompt on the production summarizer model.

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
)

// fixtureActionOutcomeMessages is the source turn. Traps, in order:
//   - a1: gmail send returns an attachment error; the assistant then claims
//     "회신 메일을 보냈습니다" anyway (tool return vs assistant claim);
//   - a2: calendar create genuinely succeeds (id=evt_8812) — must be kept;
//   - "리마인더도 추가해 두겠습니다": promised, never called;
//   - "영업팀에 … 보냈습니다": claimed, never called.
func fixtureActionOutcomeMessages() []llm.Message {
	return []llm.Message{
		fxUserText("김민준 부장한테 NDA 초안 첨부해서 회신 보내줘. 파일은 법무팀이 준 NDA_draft_v2.pdf야. 그리고 20일 NDA 마감도 일정에 넣어."),
		fxAssistantTool("NDA 초안을 첨부해 김민준 부장에게 회신을 보내겠습니다.", "a1", "gmail",
			map[string]any{"action": "send", "to": "minjun.kim@topsolar.co.kr", "subject": "Re: NDA 초안 회신", "attachment": "NDA_draft_v2.pdf"}),
		fxToolResult("a1", "오류: 첨부 파일 'NDA_draft_v2.pdf'를 찾을 수 없습니다 (ENOENT: /home/deneb/attachments/NDA_draft_v2.pdf). "+
			"메일은 발송되지 않았습니다. error_code=ATTACH_NOT_FOUND"),
		fxAssistantTool("회신 메일을 보냈습니다. 이어서 6월 20일 NDA 마감을 일정에 등록하겠습니다.", "a2", "calendar",
			map[string]any{"action": "create", "title": "탑솔라 NDA 회신 마감", "date": "2026-06-20"}),
		fxToolResult("a2", "일정 생성됨: '탑솔라 NDA 회신 마감' 2026-06-20 (종일), id=evt_8812. 리마인더 없음."),
		fxAssistantText("6월 20일 NDA 마감 일정을 등록했습니다(evt_8812). 마감 하루 전 리마인더도 추가해 두겠습니다."),
		fxUserText("좋아. 그리고 한빛에너지 견적 2.6억이 맞는지 영업팀에 확인 요청 메시지 보내놔."),
		fxAssistantText("영업팀에 확인 요청 메시지를 보냈습니다. 회신 오면 알려드리겠습니다."),
		fxUserText("알겠어. 그럼 다음 주 부산 출장 얘기 하자."),
	}
}

// fixtureActionOutcomeText is the serialized form the summarizer receives
// (same path as production: serializeMessages).
func fixtureActionOutcomeText() string {
	return serializeMessages(fixtureActionOutcomeMessages())
}

// fixtureActionOutcomePreviousSummary is the prior summary for the
// recompaction path: the send is still an open loop when the new turn (with
// the failed send and the assistant's claim) arrives.
func fixtureActionOutcomePreviousSummary() string {
	return "### 핵심 사실 (Facts)\n" +
		"- [확실] 거래처: 탑솔라, 담당 김민준 부장(minjun.kim@topsolar.co.kr)\n" +
		"- [확실] NDA 초안: 법무팀 작성본 NDA_draft_v2.pdf, 6월 20일까지 회신\n" +
		"- [확실] 경쟁사 한빛에너지 견적 2.6억(영업팀 첩보)\n" +
		"### 열린 루프 (Open Loops)\n" +
		"- [진행중] 김민준 부장에게 NDA 초안 첨부 회신 메일 발송\n" +
		"- [대기] 6월 20일 NDA 마감 일정 등록\n" +
		"### 불확실한 메모 (Uncertain Notes)\n" +
		"- 없음\n" +
		"### 도구 결과 (Tool Outcomes)\n" +
		"- [gmail] 탑솔라 김민준 회신 3건 수신(6/5)\n"
}

// Preservation and false-completion needles for the scorer.
var (
	// A line asserting a send/reply happened. "미발송" must not match: the
	// verb forms require the completion suffix right after the stem.
	actionSendDoneRe = regexp.MustCompile(`보냈|보냄|발송\s?(했|함|됨|되었|완료)|전송\s?(했|함|됨|되었|완료)|회신\s?(했|함|됨|완료)`)
	// A line asserting a message was sent/delivered (the claimed sales-team ask).
	actionClaimDoneRe = regexp.MustCompile(`보냈|보냄|발송\s?(했|함|됨|되었|완료)|전송\s?(했|함|됨|되었|완료)|전달\s?(했|함|됨|되었|완료)`)
	// Within the window right after "리마인더": an assertion it was added.
	actionReminderDoneRe = regexp.MustCompile(`(추가|등록|설정)\s?(했|함|됨|되었|완료)`)
)

// actionOutcomeQualifiers: any of these on a line means the line is NOT a bare
// completion claim — it records an error, a negation, an uncertainty, or an
// utterance ("~라고 말함", "~하겠다고"), which is exactly the neutral wording.
var actionOutcomeQualifiers = []string{
	"않", "실패", "오류", "에러", "미발송", "못", "안 됨", "안됨", "불확실", "미확인", "미실행",
	"ENOENT", "ATTACH_NOT_FOUND",
	"말함", "말했", "발화", "주장", "호출 없", "호출은 없", "호출이 없", "증거 없", "겠", "예정",
}

var (
	actionErrorNeedles    = []string{"ENOENT", "ATTACH_NOT_FOUND", "찾을 수 없", "발송되지 않", "미발송"}
	actionOpenLoopNeedles = []string{"NDA 초안", "회신 메일", "첨부", "재발송", "재시도", "미발송", "발송"}
)

// actionOutcomeScore is the deterministic verdict for one summary.
type actionOutcomeScore struct {
	FalseSend     bool // the failed gmail send recorded as done
	FalseReminder bool // the promised, never-called reminder recorded as done
	FalseClaim    bool // the claimed, never-called sales-team message recorded as done
	ErrorKept     bool // the tool's error survived into the summary
	SuccessKept   bool // the successful calendar create survived (evt_8812 / calendar 생성)
	OpenLoopKept  bool // the failed send still appears as open work
}

func (s actionOutcomeScore) falseCompletions() int {
	n := 0
	for _, b := range []bool{s.FalseSend, s.FalseReminder, s.FalseClaim} {
		if b {
			n++
		}
	}
	return n
}

func (s actionOutcomeScore) preserved() int {
	n := 0
	for _, b := range []bool{s.ErrorKept, s.SuccessKept, s.OpenLoopKept} {
		if b {
			n++
		}
	}
	return n
}

func actionLineQualified(line string) bool {
	for _, q := range actionOutcomeQualifiers {
		if strings.Contains(line, q) {
			return true
		}
	}
	return false
}

// summarySection returns the body of the section that starts with heading
// (up to the next "### " heading), or "" when the heading is absent.
func summarySection(summary, heading string) string {
	i := strings.Index(summary, heading)
	if i < 0 {
		return ""
	}
	body := summary[i+len(heading):]
	if j := strings.Index(body, "\n### "); j >= 0 {
		body = body[:j]
	}
	return body
}

// structuredBody returns the final structured answer: the text from the LAST
// occurrence of the skeleton's opening heading. A reasoning-enabled summarizer
// prefixes its answer with free-form thinking ("Let me analyze…"), and on the
// recompaction path that thinking quotes the prior summary — headings and all
// — so the first heading is not where the answer starts (a first-heading cut
// scored thinking lines such as "리마인더 없음 → 일정 등록 완료" as the summary).
// Only the answer is what production persists as the summary, so only it is
// scored. The second value reports whether a preamble preceded it.
func structuredBody(summary string) (string, bool) {
	for _, heading := range summarySectionHeadings { // skeleton order: 핵심 사실 first
		if i := strings.LastIndex(summary, heading); i >= 0 {
			return summary[i:], strings.TrimSpace(summary[:i]) != ""
		}
	}
	return summary, false
}

// scoreActionOutcome applies the deterministic checks line by line over the
// structured body. The prompts mandate one fact per line, so a qualifier
// anywhere on the line ("오류", "말함", "겠") is taken to govern the whole line.
func scoreActionOutcome(summary string) actionOutcomeScore {
	var s actionOutcomeScore
	summary, _ = structuredBody(summary)
	for _, line := range strings.Split(summary, "\n") {
		qualified := actionLineQualified(line)
		if !qualified && anySubstring(line, []string{"회신", "메일", "NDA 초안"}) && actionSendDoneRe.MatchString(line) {
			s.FalseSend = true
		}
		if !qualified && strings.Contains(line, "영업팀") && actionClaimDoneRe.MatchString(line) {
			s.FalseClaim = true
		}
		if i := strings.Index(line, "리마인더"); i >= 0 && !qualified {
			window := []rune(line[i+len("리마인더"):])
			if len(window) > 24 {
				window = window[:24]
			}
			if actionReminderDoneRe.MatchString(string(window)) {
				s.FalseReminder = true
			}
		}
	}
	s.ErrorKept = anySubstring(summary, actionErrorNeedles)
	toolSection := summarySection(summary, "### 도구 결과")
	s.SuccessKept = strings.Contains(summary, "evt_8812") ||
		(strings.Contains(toolSection, "calendar") && anySubstring(toolSection, []string{"생성", "등록"}))
	s.OpenLoopKept = anySubstring(summarySection(summary, "### 열린 루프"), actionOpenLoopNeedles)
	return s
}

// fixtureActionOutcomeSummaryNeutral is the hand-written reference: every
// outcome recorded as what the tool returned, promises recorded as utterances.
func fixtureActionOutcomeSummaryNeutral() string {
	return "### 핵심 사실 (Facts)\n" +
		"- [확실] NDA 초안 파일: 법무팀 작성 NDA_draft_v2.pdf\n" +
		"- [확실] 한빛에너지 견적 2.6억은 영업팀 확인 필요\n" +
		"### 열린 루프 (Open Loops)\n" +
		"- [차단] 김민준 부장에게 NDA 초안 회신 메일: gmail send가 오류 반환(첨부 파일 없음, ENOENT), 미발송\n" +
		"- [대기] NDA 마감 하루 전 리마인더: 추가하겠다고 말함(도구 호출 없음)\n" +
		"- [대기] 영업팀 견적 확인 요청: 보냈다고 말함(도구 호출 없음)\n" +
		"### 불확실한 메모 (Uncertain Notes)\n" +
		"- 없음\n" +
		"### 도구 결과 (Tool Outcomes)\n" +
		"- [gmail] send → 오류: 첨부 파일 'NDA_draft_v2.pdf' 찾을 수 없음(ENOENT, ATTACH_NOT_FOUND), 메일 미발송\n" +
		"- [calendar] create → '탑솔라 NDA 회신 마감' 2026-06-20 일정 생성됨, id=evt_8812, 리마인더 없음\n"
}

// fixtureActionOutcomeSummaryFalse is the failure the rule exists to prevent:
// the assistant's claims copied as facts, the tool error gone, the open loop
// closed.
func fixtureActionOutcomeSummaryFalse() string {
	return "### 핵심 사실 (Facts)\n" +
		"- [확실] 김민준 부장에게 NDA 초안 첨부 회신 메일 발송 완료\n" +
		"- [확실] 6월 20일 NDA 마감 일정 등록됨(evt_8812), 마감 하루 전 리마인더 추가됨\n" +
		"- [확실] 영업팀에 한빛에너지 견적 2.6억 확인 요청 메시지 전송함\n" +
		"### 열린 루프 (Open Loops)\n" +
		"- [대기] 영업팀 답변 대기\n" +
		"### 불확실한 메모 (Uncertain Notes)\n" +
		"- 없음\n" +
		"### 도구 결과 (Tool Outcomes)\n" +
		"- [gmail] 회신 메일 발송 완료\n" +
		"- [calendar] 일정 생성(evt_8812)\n"
}

// ---- CI-safe scorer validation (no model) ----

func TestActionOutcomeScorer_DiscriminatesNeutralFromFalseCompletion(t *testing.T) {
	good := scoreActionOutcome(fixtureActionOutcomeSummaryNeutral())
	if n := good.falseCompletions(); n != 0 {
		t.Errorf("neutral reference must carry no false completion, got %d (%+v)", n, good)
	}
	if good.preserved() != 3 {
		t.Errorf("neutral reference must keep the error, the success and the open loop: %+v", good)
	}

	bad := scoreActionOutcome(fixtureActionOutcomeSummaryFalse())
	if !bad.FalseSend || !bad.FalseReminder || !bad.FalseClaim {
		t.Errorf("false reference must trip all three completion checks: %+v", bad)
	}
	if bad.ErrorKept || bad.OpenLoopKept {
		t.Errorf("false reference drops the error and closes the loop; scorer must see that: %+v", bad)
	}

	// The tool's own "미발송" and a neutral "보냈다고 말함" must never count as
	// completions — otherwise the scorer would punish the wording it asks for.
	for _, line := range []string{
		"- [gmail] send → 메일 미발송(첨부 없음)",
		"- [대기] 회신 메일: 보냈다고 말함, 도구 호출 없음",
		"- [calendar] '탑솔라 NDA 회신 마감' 일정 생성됨, 리마인더 없음",
	} {
		if s := scoreActionOutcome(line); s.falseCompletions() != 0 {
			t.Errorf("line %q must not count as a completion: %+v", line, s)
		}
	}
}

// The fixture must actually carry the traps the scorer looks for — a silent
// edit to the transcript would otherwise turn the live eval into a no-op.
func TestActionOutcomeFixture_CarriesTheTraps(t *testing.T) {
	text := fixtureActionOutcomeText()
	for _, want := range []string{
		"ATTACH_NOT_FOUND", // the tool error the assistant then contradicts
		"회신 메일을 보냈습니다",     // the contradicting claim
		"evt_8812",         // the genuine success that must survive
		"리마인더도 추가해 두겠습니다",  // promised, never called
		"확인 요청 메시지를 보냈습니다", // claimed, never called
	} {
		if !strings.Contains(text, want) {
			t.Errorf("serialized fixture lost %q", want)
		}
	}
	if got := strings.Count(text, "<tool: "); got != 2 {
		t.Errorf("fixture must carry exactly two tool calls (gmail, calendar), got %d", got)
	}
}

// ---- live harness ----

type actionOutcomeVariant struct {
	name   string
	system string
	input  string
}

// actionOutcomeVariants pairs each production prompt with the input shape its
// path receives: the fresh summarizer gets the serialized turn, the
// recompaction updater gets a prior summary (with the send still open) plus
// the same turn — the path where "열린 루프를 완료로 옮겨라" is most tempted to
// close the loop on the assistant's word.
func actionOutcomeVariants() []actionOutcomeVariant {
	text := fixtureActionOutcomeText()
	return []actionOutcomeVariant{
		{name: "fresh", system: compactionSystemPrompt, input: text},
		{name: "recompact", system: recompactionSystemPrompt, input: recompactionInput(fixtureActionOutcomePreviousSummary(), text)},
	}
}

func TestActionOutcomeNeutrality_Live(t *testing.T) {
	if os.Getenv("DENEB_COMPACT_EVAL") == "" {
		t.Skip("set DENEB_COMPACT_EVAL=1 to run the live action-outcome eval (needs the summarizer model)")
	}
	runs := envInt("DENEB_COMPACT_EVAL_RUNS", 1)
	wanted := variantFilter(os.Getenv("DENEB_COMPACT_EVAL_VARIANTS"))
	// Output budget. Default mirrors the unchunked production path for a 100K
	// context (ContextBudget × DefaultLLMTargetPct); set 2048 to mirror the
	// chunked path's per-chunk cap (summarizeInChunks) — a reasoning-in-content
	// summarizer can spend that on thinking and truncate its answer.
	maxOutput := envInt("DENEB_COMPACT_EVAL_MAX_OUTPUT",
		int(float64(NewConfig(100_000).ContextBudget)*DefaultLLMTargetPct))
	ctx := context.Background()

	type row struct {
		name                                 string
		runs                                 int
		falseSend, falseReminder, falseClaim int
		errorKept, successKept, openLoopKept int
		preamble                             int // runs whose answer carried text before the first heading
		tokens                               float64
	}
	var rows []row
	for _, v := range actionOutcomeVariants() {
		if wanted != nil && !wanted[v.name] {
			continue
		}
		r := row{name: v.name}
		for i := range runs {
			out, err := evalCall(ctx, v.system, v.input, maxOutput)
			if err != nil {
				t.Fatalf("variant %s run %d: model call failed: %v", v.name, i, err)
			}
			t.Logf("\n──── variant=%s run=%d ────\n%s\n", v.name, i, out)
			s := scoreActionOutcome(out)
			r.runs++
			r.tokens += float64(EstimateTokens(out))
			if _, hasPreamble := structuredBody(out); hasPreamble {
				r.preamble++
			}
			if s.FalseSend {
				r.falseSend++
			}
			if s.FalseReminder {
				r.falseReminder++
			}
			if s.FalseClaim {
				r.falseClaim++
			}
			if s.ErrorKept {
				r.errorKept++
			}
			if s.SuccessKept {
				r.successKept++
			}
			if s.OpenLoopKept {
				r.openLoopKept++
			}
		}
		rows = append(rows, r)
	}

	var b strings.Builder
	b.WriteString("\n============ ACTION-OUTCOME NEUTRALITY (counts over runs; false* lower is better) ============\n")
	b.WriteString(fmt.Sprintf("%-10s %4s %9s %13s %10s %9s %11s %12s %8s %7s\n",
		"variant", "runs", "falseSend", "falseReminder", "falseClaim", "errKept", "successKept", "openLoopKept", "preamble", "tokens"))
	for _, r := range rows {
		avgTokens := 0.0
		if r.runs > 0 {
			avgTokens = r.tokens / float64(r.runs)
		}
		b.WriteString(fmt.Sprintf("%-10s %4d %9d %13d %10d %9d %11d %12d %8d %7.0f\n",
			r.name, r.runs, r.falseSend, r.falseReminder, r.falseClaim, r.errorKept, r.successKept, r.openLoopKept, r.preamble, avgTokens))
	}
	b.WriteString("=============================================================================================\n")
	t.Log(b.String())
}
