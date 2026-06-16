package genesis

import (
	"fmt"
	"strings"
	"unicode"
)

type skillReplayTrace struct {
	text   string
	tokens map[string]struct{}
}

func scoreSkillReplayCase(body string, tc SkillValidationCaseRecord) validationCaseScore {
	replay := tc.Replay
	if !replay.hasAssertions() {
		return validationCaseScore{}
	}
	label := validationCaseLabel(tc)
	trace := buildSkillReplayTrace(body, replay)
	var score validationCaseScore
	for _, action := range replay.RequiredActions {
		score.Total++
		if containsNormalizedValidationText(trace.text, action) {
			score.Passed++
			continue
		}
		score.Failures = append(score.Failures, fmt.Sprintf("%s replay missing required action %q", label, truncateRunes(action, 80)))
	}
	for _, action := range replay.ForbiddenActions {
		score.Total++
		if !containsNormalizedValidationText(trace.text, action) {
			score.Passed++
			continue
		}
		score.Failures = append(score.Failures, fmt.Sprintf("%s replay contains forbidden action %q", label, truncateRunes(action, 80)))
	}
	for _, observation := range replay.RequiredObservations {
		score.Total++
		if containsNormalizedValidationText(trace.text, observation) {
			score.Passed++
			continue
		}
		score.Failures = append(score.Failures, fmt.Sprintf("%s replay missing required observation %q", label, truncateRunes(observation, 80)))
	}
	for _, observation := range replay.ForbiddenObservations {
		score.Total++
		if !containsNormalizedValidationText(trace.text, observation) {
			score.Passed++
			continue
		}
		score.Failures = append(score.Failures, fmt.Sprintf("%s replay contains forbidden observation %q", label, truncateRunes(observation, 80)))
	}
	for _, tool := range replay.RequiredTools {
		score.Total++
		if trace.hasTool(tool) {
			score.Passed++
			continue
		}
		score.Failures = append(score.Failures, fmt.Sprintf("%s replay missing required tool %q", label, truncateRunes(tool, 80)))
	}
	for _, tool := range replay.ForbiddenTools {
		score.Total++
		if !trace.hasTool(tool) {
			score.Passed++
			continue
		}
		score.Failures = append(score.Failures, fmt.Sprintf("%s replay contains forbidden tool %q", label, truncateRunes(tool, 80)))
	}
	for _, call := range replay.ExpectedToolCalls {
		score.Total++
		if trace.hasToolCall(call) {
			score.Passed++
			continue
		}
		score.Failures = append(score.Failures, fmt.Sprintf("%s replay missing expected tool call %q", label, formatReplayToolCall(call)))
	}
	for _, call := range replay.ForbiddenToolCalls {
		score.Total++
		if !trace.hasToolCall(call) {
			score.Passed++
			continue
		}
		score.Failures = append(score.Failures, fmt.Sprintf("%s replay contains forbidden tool call %q", label, formatReplayToolCall(call)))
	}
	if replay.RequireOrder && len(replay.ExpectedToolCalls) > 1 {
		score.Total++
		if trace.hasToolCallsInOrder(replay.ExpectedToolCalls) {
			score.Passed++
		} else {
			score.Failures = append(score.Failures, fmt.Sprintf("%s replay expected tool calls are out of order", label))
		}
	}
	return score
}

func buildSkillReplayTrace(body string, replay SkillReplayCaseRecord) skillReplayTrace {
	text := normalizedValidationText(body)
	return skillReplayTrace{
		text:   text,
		tokens: validationTokens(text),
	}
}

func (t skillReplayTrace) hasTool(tool string) bool {
	tool = normalizedValidationText(tool)
	if tool == "" {
		return true
	}
	if strings.Contains(tool, " ") {
		return containsNormalizedValidationText(t.text, tool)
	}
	_, ok := t.tokens[tool]
	return ok
}

func (t skillReplayTrace) hasToolCall(call SkillReplayToolCallRecord) bool {
	if strings.TrimSpace(call.Name) != "" && !t.hasTool(call.Name) {
		return false
	}
	for _, required := range call.InputIncludes {
		if !containsNormalizedValidationText(t.text, required) {
			return false
		}
	}
	for _, forbidden := range call.InputExcludes {
		if containsNormalizedValidationText(t.text, forbidden) {
			return false
		}
	}
	return true
}

func (t skillReplayTrace) hasToolCallsInOrder(calls []SkillReplayToolCallRecord) bool {
	start := 0
	for _, call := range calls {
		pos := t.toolCallPositionAtOrAfter(call, start)
		if pos < 0 {
			return false
		}
		start = pos + 1
	}
	return true
}

func (t skillReplayTrace) toolCallPositionAtOrAfter(call SkillReplayToolCallRecord, start int) int {
	if start < 0 {
		start = 0
	}
	if start > len(t.text) {
		return -1
	}
	pos := start
	if name := normalizedValidationText(call.Name); name != "" {
		idx := strings.Index(t.text[start:], name)
		if idx < 0 {
			return -1
		}
		pos = start + idx
	}
	searchFrom := pos
	for _, required := range call.InputIncludes {
		required = normalizedValidationText(required)
		if required == "" {
			continue
		}
		idx := strings.Index(t.text[searchFrom:], required)
		if idx < 0 {
			return -1
		}
		found := searchFrom + idx
		if found > pos {
			pos = found
		}
	}
	for _, forbidden := range call.InputExcludes {
		if containsNormalizedValidationText(t.text[pos:], forbidden) {
			return -1
		}
	}
	return pos
}

func formatReplayToolCall(call SkillReplayToolCallRecord) string {
	var parts []string
	if name := strings.TrimSpace(call.Name); name != "" {
		parts = append(parts, name)
	}
	if len(call.InputIncludes) > 0 {
		parts = append(parts, "includes="+strings.Join(call.InputIncludes, ", "))
	}
	if len(call.InputExcludes) > 0 {
		parts = append(parts, "excludes="+strings.Join(call.InputExcludes, ", "))
	}
	if call.FixtureError {
		parts = append(parts, "fixtureError=true")
	}
	return truncateRunes(strings.Join(parts, " "), 120)
}

func containsNormalizedValidationText(haystack, needle string) bool {
	needle = normalizedValidationText(needle)
	if needle == "" {
		return true
	}
	return strings.Contains(haystack, needle)
}

func normalizedValidationText(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(value), " "))
}

func validationTokens(value string) map[string]struct{} {
	tokens := make(map[string]struct{})
	var b strings.Builder
	flush := func() {
		if b.Len() == 0 {
			return
		}
		tokens[b.String()] = struct{}{}
		b.Reset()
	}
	for _, r := range value {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r), r == '_', r == '-', r == '.':
			b.WriteRune(unicode.ToLower(r))
		default:
			flush()
		}
	}
	flush()
	return tokens
}
