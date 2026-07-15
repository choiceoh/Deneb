package denebui

// Card health + adoption observability for the turn-completion path:
// validity Warns, composition advisories, and the adoption-miss heuristic.

import (
	"log/slog"
	"strings"
	"unicode/utf8"
)

// ReportCardHealth validates any deneb-ui fences in the final reply and
// logs schema violations. Warn (not Error): the client still renders a code
// block, and the turn itself succeeded — but a rising Warn rate is the early
// signal that a model swap or prompt drift broke card emission (the runtime
// validator was previously wired only into tests and denebui-check).
func ReportCardHealth(text, sessionKey string, logger *slog.Logger) {
	if text == "" {
		return
	}
	if logger == nil {
		logger = slog.Default()
	}
	if !HasFence(text) {
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
	// The card-authored counterpart to the adoption-miss signal: one Info per turn
	// that DID ship a card, so `card-authored vs adoption-miss` is a real journald
	// ratio (the adoption RATE this file documents). Previously the denominator was
	// unlogged — a schema-valid card with no advisories emitted nothing — so the
	// rate was uncomputable despite the comment above claiming it. Per-turn (not
	// per-block) to match the per-turn miss signal: N cards in one turn is still one
	// adoption, so the numerator stays a turn count.
	fences := ExtractFences(text)
	logger.Info("deneb-ui card authored",
		"session", sessionKey, "blocks", len(fences), "runes", utf8.RuneCountInString(text))
	for i, body := range fences {
		issues, err := Validate(body)
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
			if adv := CompositionAdvisories(body); len(adv) > 0 {
				logger.Info("deneb-ui card composition advisory",
					"session", sessionKey, "block", i,
					"advisories", strings.Join(adv, ","))
			}
		}
	}
}

// LooksStructuredWithoutCard is the deterministic adoption-miss proxy: a
// markdown table, or >=5 bullet lines in a >=400-rune answer, is the shape the
// authoring contract says should have been a card. Heuristic — it feeds an
// Info observation, never a gate.
func LooksStructuredWithoutCard(text string) bool {
	return looksStructuredWithoutCard(text)
}

func looksStructuredWithoutCard(text string) bool {
	longAnswer := utf8.RuneCountInString(text) >= 400
	bullets := 0
	for _, line := range strings.Split(text, "\n") {
		t := strings.TrimSpace(line)
		if isMarkdownTableSeparator(t) {
			return true
		}
		if longAnswer && (strings.HasPrefix(t, "- ") || strings.HasPrefix(t, "* ")) {
			bullets++
			if bullets >= 5 {
				return true
			}
		}
	}
	return false
}

// isMarkdownTableSeparator reports whether a trimmed line is a markdown table
// separator row — pipe-delimited cells, each a dash run with optional
// alignment colons (`---`, `:---`, `---:`, `:---:`). At least one cell must
// carry >=3 dashes so a stray "|-|" doesn't count.
func isMarkdownTableSeparator(line string) bool {
	if !strings.Contains(line, "|") {
		return false
	}
	threeDash := false
	for _, cell := range strings.Split(strings.Trim(line, "|"), "|") {
		c := strings.TrimSpace(cell)
		if c == "" {
			return false
		}
		dashes := strings.TrimSuffix(strings.TrimPrefix(c, ":"), ":")
		if dashes == "" || strings.Count(dashes, "-") != len(dashes) {
			return false
		}
		if len(dashes) >= 3 {
			threeDash = true
		}
	}
	return threeDash
}
