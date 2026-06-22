// workfeed_question.go — turn a proactive turn that asks the user something into
// an answerable work-feed card. A ```choices fence becomes inline answer chips
// (ActionAnswer); a trailing question mark (no fence) flags a free-text question
// so the native shows a reply field. Both route the answer back to the asking
// session — the feed stops being a dead-end for the agent's questions.
package server

import (
	"strconv"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/workfeed"
)

// choicesFence is the markdown fence the agent uses to offer one-tap answers,
// mirroring the chat renderer's ```choices convention (one option per line).
const choicesFence = "```choices"

// splitChoicesFence pulls a ```choices block out of proactive content: each
// non-empty line inside is one answer option, and the whole fence is removed from
// the returned body so the card shows the question prose, not a raw code fence.
// Returns the cleaned body + options (nil when there is no well-formed fence).
func splitChoicesFence(content string) (body string, choices []string) {
	i := strings.Index(content, choicesFence)
	if i < 0 {
		return content, nil
	}
	after := content[i+len(choicesFence):]
	nl := strings.IndexByte(after, '\n')
	if nl < 0 {
		return content, nil // malformed open fence — leave content intact
	}
	inner := after[nl+1:]
	end := strings.Index(inner, "```")
	if end < 0 {
		return content, nil // unterminated fence — leave content intact
	}
	for _, line := range strings.Split(inner[:end], "\n") {
		if s := strings.TrimSpace(line); s != "" {
			choices = append(choices, s)
		}
	}
	if len(choices) == 0 {
		return content, nil
	}
	body = strings.TrimSpace(content[:i] + "\n" + inner[end+len("```"):])
	if body == "" {
		body = content // never blank the card
	}
	return body, choices
}

// choiceAnswerActions turns parsed ```choices options into work-feed answer
// chips. Kind=ActionAnswer: tapping one settles the card AND returns the option
// text as a prompt the native sends to the asking session.
func choiceAnswerActions(choices []string) []workfeed.Action {
	if len(choices) == 0 {
		return nil
	}
	actions := make([]workfeed.Action, 0, len(choices))
	for i, c := range choices {
		actions = append(actions, workfeed.Action{
			ID:     "answer:" + strconv.Itoa(i),
			Kind:   workfeed.ActionAnswer,
			Label:  c,
			Prompt: c,
		})
	}
	return actions
}

// endsWithQuestionMark reports whether the trimmed text ends with a question mark
// (ASCII or fullwidth) — the heuristic that flags a free-text proactive question
// (no ```choices) as an answerable card so the native shows a reply field.
func endsWithQuestionMark(s string) bool {
	s = strings.TrimRight(strings.TrimSpace(s), "\"'」』）)]*_")
	return strings.HasSuffix(s, "?") || strings.HasSuffix(s, "？")
}
