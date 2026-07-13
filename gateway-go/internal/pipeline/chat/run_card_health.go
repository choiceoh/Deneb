package chat

// deneb-ui card health + adoption observability, split out of run_lifecycle.go
// (single responsibility: this file owns everything the turn-completion path
// observes about cards — validity Warns, composition advisories, and the
// adoption-miss heuristic).

import (
	"log/slog"
	"strings"
	"unicode/utf8"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/denebui"
)

// reportDenebUICardHealth validates any deneb-ui fences in the final reply and
// logs schema violations. Warn (not Error): the client still renders a code
// block, and the turn itself succeeded — but a rising Warn rate is the early
// signal that a model swap or prompt drift broke card emission (the runtime
// validator was previously wired only into tests and denebui-check).
func reportDenebUICardHealth(text, sessionKey string, logger *slog.Logger) {
	if text == "" {
		return
	}
	if !denebui.HasFence(text) {
		// Adoption observability (능동형 카드 강화 S4): a structured-looking
		// answer that shipped WITHOUT a card is the drift we cannot see from
		// validity Warns alone. Heuristic on purpose (markdown table or a
		// bullet run in a long answer) and Info-level — journald grep of
		// adoption-miss vs card-authored turns yields the adoption rate.
		if looksStructuredWithoutCard(text) {
			logger.Info("deneb-ui adoption miss — structured answer without a card (heuristic)",
				"session", sessionKey, "runes", utf8.RuneCountInString(text))
		}
		return
	}
	for i, body := range denebui.ExtractFences(text) {
		issues, err := denebui.Validate(body)
		switch {
		case err != nil:
			logger.Warn("deneb-ui card unparseable — client will show a code block",
				"session", sessionKey, "block", i, "error", err)
		case len(issues) > 0:
			// Cap the detail: one line per turn is plenty for drift detection.
			logger.Warn("deneb-ui card has schema issues",
				"session", sessionKey, "block", i,
				"issueCount", len(issues), "firstIssue", issues[0].String())
		default:
			// Schema-valid cards still get a composition read (조형 관례 —
			// prose dumps, missing section headers): Info-level observation
			// only, so the journal shows whether the authoring contract
			// actually shapes production cards.
			if adv := denebui.CompositionAdvisories(body); len(adv) > 0 {
				logger.Info("deneb-ui card composition advisory",
					"session", sessionKey, "block", i,
					"advisories", strings.Join(adv, ","))
			}
		}
	}
}

// looksStructuredWithoutCard is the deterministic adoption-miss proxy: a
// markdown table, or >=5 bullet lines in a >=400-rune answer, is the shape the
// authoring contract says should have been a card. Heuristic — it feeds an
// Info observation, never a gate.
func looksStructuredWithoutCard(text string) bool {
	if strings.Contains(text, "|---") || strings.Contains(text, "| ---") {
		return true
	}
	if utf8.RuneCountInString(text) < 400 {
		return false
	}
	bullets := 0
	for _, line := range strings.Split(text, "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "- ") || strings.HasPrefix(t, "* ") {
			bullets++
			if bullets >= 5 {
				return true
			}
		}
	}
	return false
}
