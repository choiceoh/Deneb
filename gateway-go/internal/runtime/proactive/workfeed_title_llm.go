package proactive

import (
	"context"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/runtime/proactive/cardtitle"
)

// Tiny-LLM card titler + summarizer for proactive work-feed cards.
//
// The card title is whatever the tiny model extracts (labeled "제목:" / "요약:"),
// not the first heading or prose line of the report. The analysis (main) model
// often writes a generic heading ("메일 분석 리포트") or opens with narration, so
// a first-line heuristic is the wrong source. Naming a card is a tiny extraction
// job (≤20-char noun phrase + 2-line gist).
//
// Why tiny, not lightweight: this call caps output at a small token budget, which
// only fits an answer with the thinking channel OFF. The tiny role is an explicit
// self-hosted extraction model (agents.tinyModel) whose thinking-off toggle is
// honored on the vLLM path. The lightweight role can resolve to a cloud reasoning
// model that ignores the toggle and burns the token budget on reasoning.
//
// Parser rule: only labeled lines count. An unlabeled first line is ignored —
// reasoning models dump chain-of-thought there, and that used to land as the
// live card title (2026-08 srv4 workfeed). Best-effort: any failure or unlabeled
// output returns ("", "") and proactive_relay falls back to extractCardTitle /
// extractCardSummary independently.

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
	if len([]rune(line)) < 3 || isGenericMailReportTitle(line) || isReasoningLeakTitle(line) {
		return ""
	}
	return line
}

// isReasoningLeakTitle reports titles that are the model's own instructions or
// chain-of-thought, not a card noun phrase. Live 2026-08 feed cards stored
// "We need answer in Korean…" / "我们根据要求…" / "<think>" as the title when
// the unlabeled first line of the titler output was accepted.
func isReasoningLeakTitle(t string) bool {
	if strings.HasPrefix(t, "<") {
		return true
	}
	for _, prefix := range reasoningTitlePrefixes {
		if hasPrefixFold(t, prefix) {
			return true
		}
	}
	return false
}

var reasoningTitlePrefixes = []string{
	"We need", "Need answer", "Need to ", "The user wants",
	"Let me ", "I need to",
	"我们", "우리는 입력", "우리는 주어진", "우리는 출력",
	"우선 입력", "사용자가 제공",
}

func hasPrefixFold(s, prefix string) bool {
	if len(s) < len(prefix) {
		return false
	}
	return strings.EqualFold(s[:len(prefix)], prefix)
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
	if len([]rune(s)) < 4 || strings.HasPrefix(s, "<") {
		return ""
	}
	return clipRunes(s, workFeedSummaryMaxRunes)
}

func parseLLMTitleSummary(raw string) (title, summary string) {
	title, summary = cardtitle.ParseLLMTitleSummary(raw)
	return cleanLLMCardTitle(title), cleanLLMCardSummary(summary)
}
