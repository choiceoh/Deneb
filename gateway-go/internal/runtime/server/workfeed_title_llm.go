package server

import (
	"context"
	"regexp"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/modelrole"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/pilot"
)

// Lightweight-LLM card titler + summarizer for proactive work-feed cards.
//
// The analysis (main) model often writes a generic heading ("메일 분석 리포트") or
// opens with a narration sentence, so the deterministic heuristic ends up with a
// poor card title (a generic label, or a whole sentence). Naming a card is a tiny
// job, so it is handed to the lightweight role (local, cheap, fast) rather than the
// analysis model — used both for mail reports (to surface the email's real subject)
// and for any proactive body whose heuristic title is weak (see isWeakCardTitle).
// The same call also produces the card's 2-line summary, so the preview under the
// title reads as a real gist instead of the heuristic's joined-and-clipped body
// lines — one call, both outputs. Best-effort: any failure returns ("", "") and
// proactive_relay falls back to the deterministic extractCardTitle / extractCardSummary
// heuristics (independently — a good LLM title still applies even if the summary is
// empty).

const (
	// cardTitleMaxInputRunes bounds the report text sent to the model. The subject
	// / topic sits at the top of a report, so the head is plenty and keeps the call
	// cheap.
	cardTitleMaxInputRunes = 1200
	// cardTitleMaxTokens caps the generated output. It must fit a short title plus a
	// two-line summary, so it is larger than a title-only cap.
	cardTitleMaxTokens = 256
	// cardTitleTimeout bounds the lightweight call so a stalled model never holds
	// up proactive delivery.
	cardTitleTimeout = 8 * time.Second
	// cardTitleMaxRunes hard-caps the LLM card title (the mail-report titler) at a
	// short, glanceable length. The prompt asks for ≤16 Korean characters; this is
	// the safety clamp if the model overshoots. Kept separate from the heuristic
	// fallback's workFeedTitleMaxRunes (40), which clips raw subjects / daily-summary
	// headings that are legitimately longer.
	cardTitleMaxRunes = 16
)

// llmTitleLabelRe / llmSummaryLabelRe match the "제목:" / "요약:" labels the model is
// asked to lead each line with (also the English forms, full-width colon, and an
// optional space before the colon), so the two fields can be parsed apart.
var (
	llmTitleLabelRe   = regexp.MustCompile(`(?i)^\s*(제목|title)\s*[:：]\s*`)
	llmSummaryLabelRe = regexp.MustCompile(`(?i)^\s*(요약|summary)\s*[:：]\s*`)
)

const cardTitleSystemPrompt = `너는 업무 알림 카드의 제목과 짧은 요약을 뽑아내는 도구다. 입력은 메일 분석 리포트, 일정 브리핑, 분석 메모 등이다.
정확히 아래 두 줄 형식으로만 출력한다:
제목: <핵심 명사구>
요약: <무엇에 관한 것이고 왜 중요한지>

규칙:
- 제목은 한글 16자 이내. "메일 분석", "리포트", "보고" 같은 군더더기 단어를 붙이지 마라.
- 요약은 카드 미리보기용으로 2문장(약 80자) 이내. 제목을 그대로 반복하지 말고 핵심 내용과 이유를 담는다.
- 따옴표·마크다운·머리기호·이모지 금지. 위 두 줄 외에 다른 설명·접두어를 출력하지 마라.`

// cardTitleSummary returns a lightweight-model-generated card title and 2-line
// summary for a proactive body, or ("", "") on any failure (so the heuristic
// fallbacks apply). It is wired as proactiveRelayDeps.cardTitler.
func (s *Server) cardTitleSummary(content string) (title, summary string) {
	body := content
	if r := []rune(body); len(r) > cardTitleMaxInputRunes {
		body = string(r[:cardTitleMaxInputRunes])
	}
	ctx, cancel := context.WithTimeout(s.ShutdownCtx(), cardTitleTimeout)
	defer cancel()
	out, err := pilot.CallRoleLLM(ctx, modelrole.RoleLightweight, cardTitleSystemPrompt, body, cardTitleMaxTokens)
	if err != nil {
		return "", ""
	}
	return parseLLMTitleSummary(out)
}

// parseLLMTitleSummary splits the model's "제목: …\n요약: …" output into a cleaned
// title and summary. It tolerates the model dropping the labels: the first unlabeled
// line becomes the title and the rest the summary. Either field may come back "" (the
// caller keeps its heuristic for that field).
func parseLLMTitleSummary(raw string) (title, summary string) {
	var titleRaw, summaryRaw string
	var unlabeled []string
	for _, ln := range strings.Split(raw, "\n") {
		l := strings.TrimSpace(ln)
		if l == "" {
			continue
		}
		if loc := llmTitleLabelRe.FindStringIndex(l); loc != nil {
			if titleRaw == "" {
				titleRaw = l[loc[1]:]
			}
			continue
		}
		if loc := llmSummaryLabelRe.FindStringIndex(l); loc != nil {
			if summaryRaw == "" {
				summaryRaw = l[loc[1]:]
			}
			continue
		}
		unlabeled = append(unlabeled, l)
	}
	// Tolerate dropped labels: a missing title takes the first unlabeled line; a
	// missing summary takes whatever unlabeled lines remain.
	if titleRaw == "" && len(unlabeled) > 0 {
		titleRaw = unlabeled[0]
		unlabeled = unlabeled[1:]
	}
	if summaryRaw == "" && len(unlabeled) > 0 {
		summaryRaw = strings.Join(unlabeled, " ")
	}
	return cleanLLMCardTitle(titleRaw), cleanLLMCardSummary(summaryRaw)
}

// cleanLLMCardTitle normalizes a raw lightweight-model title into a card-ready
// string: first line, markdown/quotes stripped, clipped. Returns "" when the
// model declined or echoed a generic "메일 분석 리포트" label, so the caller falls
// back to the heuristic subject.
func cleanLLMCardTitle(raw string) string {
	line := strings.TrimSpace(raw)
	if i := strings.IndexByte(line, '\n'); i >= 0 {
		line = strings.TrimSpace(line[:i])
	}
	line = stripMarkdownLine(line)
	line = strings.Trim(line, " \t\"'`“”‘’「」『』")
	line = strings.TrimSpace(line)
	if len([]rune(line)) < 3 || isGenericMailReportTitle(line) {
		return ""
	}
	return clipRunes(line, cardTitleMaxRunes)
}

// cleanLLMCardSummary normalizes a raw model summary into a one-paragraph card
// preview: lines joined, markdown/quotes stripped, clipped to the same length as the
// heuristic summary. Returns "" when there is nothing usable, so the caller keeps the
// heuristic summary.
func cleanLLMCardSummary(raw string) string {
	var parts []string
	for _, ln := range strings.Split(strings.TrimSpace(raw), "\n") {
		if ln = stripMarkdownLine(strings.TrimSpace(ln)); ln != "" {
			parts = append(parts, ln)
		}
	}
	s := strings.Join(parts, " ")
	s = strings.Trim(s, " \t\"'`“”‘’「」『』")
	s = strings.TrimSpace(s)
	if len([]rune(s)) < 4 {
		return ""
	}
	return clipRunes(s, workFeedSummaryMaxRunes)
}
