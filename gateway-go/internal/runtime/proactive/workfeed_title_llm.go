package proactive

import (
	"context"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/runtime/proactive/cardtitle"
)

// Tiny-LLM card titler + summarizer for proactive work-feed cards.
//
// The analysis (main) model often writes a generic heading ("메일 분석 리포트") or
// opens with a narration sentence, so the deterministic heuristic ends up with a
// poor card title (a generic label, or a whole sentence). Naming a card is a tiny
// extraction job (≤20-char noun phrase + 2-line gist), so it is handed to the tiny
// role — used both for mail reports (to surface the email's real subject) and for
// any proactive body whose heuristic title is weak (see isWeakCardTitle).
//
// Why tiny, not lightweight: this call caps output at a small token budget, which
// only fits an answer with the thinking channel OFF. The tiny role is an explicit
// self-hosted extraction model (agents.tinyModel, e.g. vLLM qwen3.6-35b-a3b) whose
// thinking-off toggle is honored on the vLLM path — same role session titles and
// mail-stage-1 extractors use. The lightweight role, by contrast, can resolve to a
// cloud reasoning model (deepseek-v4-flash-api) that ignores the vLLM template
// thinking toggle and burns the whole token budget on reasoning → empty content →
// every card silently fell back to the raw-sentence heuristic.
//
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
)

// CardTitleSummary returns a tiny-model-generated card title and 2-line summary
// for a proactive body, or ("", "") on any failure (so the heuristic fallbacks
// apply). It is wired as proactiveRelayDeps.cardTitler.
func CardTitleSummary(ctx context.Context, content string) (title, summary string) {
	body := content
	if r := []rune(body); len(r) > cardTitleMaxInputRunes {
		body = string(r[:cardTitleMaxInputRunes])
	}
	out, err := cardtitle.CallTiny(ctx, body)
	if err != nil {
		return "", ""
	}
	return parseLLMTitleSummary(out)
}

// cleanLLMCardTitle normalizes a raw lightweight-model title into a card-ready
// string: first line, markdown/quotes stripped. Returns "" when the model declined
// or echoed a generic "메일 분석 리포트" label, so the caller falls back to the
// heuristic subject. No length clamp — the prompt asks for ≤20 chars and we keep
// the model's title intact rather than chopping it mid-word.
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
	return line
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


func parseLLMTitleSummary(raw string) (title, summary string) {
	title, summary = cardtitle.ParseLLMTitleSummary(raw)
	return cleanLLMCardTitle(title), cleanLLMCardSummary(summary)
}
