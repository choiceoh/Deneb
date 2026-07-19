package polaris

import (
	"regexp"
	"sort"
	"strings"
	"unicode"
)

const (
	artifactQuestionRunes   = 600
	artifactSummaryRunes    = 6000
	artifactResolutionRunes = 1800
	artifactBurstRunes      = 1400
	artifactMaxBursts       = 6
)

var (
	artifactToolPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\[도구\s+([a-z0-9_.:-]+)\]`),
		regexp.MustCompile(`(?i)<tool:\s*([a-z0-9_.:-]+)>`),
	}
	artifactCodeRefPattern = regexp.MustCompile(`(?i)(?:^|[\s'"` + "`" + `(])((?:[a-z0-9_.-]+/)+[a-z0-9_.-]+\.(?:go|kt|kts|java|py|js|jsx|ts|tsx|rs|sql|sh|md))(?:$|[\s'"` + "`" + `),:])`)
	artifactSignalPattern  = regexp.MustCompile(`(?i)(?:\b\d{2,}\b|\d{4}-\d{2}-\d{2}|error|failed|exception|panic|결정|확정|완료|해결|실패|배포|머지|검증|/[^\s]+|\[[^\]]+\])`)
)

func deriveConversationArtifact(node SummaryNode, messages []messageRecord) *ConversationArtifact {
	covered := make([]messageRecord, 0)
	for _, message := range messages {
		if message.MsgIndex < node.MsgStart || message.MsgIndex > node.MsgEnd {
			continue
		}
		covered = append(covered, message)
	}
	question := artifactQuestion(covered)
	facts := extractArtifactSections(node.Content, "핵심 사실", "facts")
	if facts == "" {
		facts = node.Content
	}
	toolOutcomes := extractArtifactSections(node.Content, "도구 결과", "tool outcomes", "결정 사항", "decisions")
	resolution := strings.TrimSpace(strings.Join(nonEmptyStrings(facts, toolOutcomes), "\n\n"))
	corpusParts := []string{node.Content}
	for _, message := range covered {
		corpusParts = append(corpusParts, message.TextContent)
	}
	corpus := strings.Join(corpusParts, "\n")
	return &ConversationArtifact{
		Question:   truncateArtifactText(question, artifactQuestionRunes),
		Summary:    truncateArtifactText(node.Content, artifactSummaryRunes),
		Resolution: truncateArtifactText(resolution, artifactResolutionRunes),
		Systems:    artifactSystems(corpus),
		CodeRefs:   artifactCodeRefs(corpus),
		Bursts:     artifactBursts(covered),
	}
}

func artifactQuestion(messages []messageRecord) string {
	var questions []string
	for _, message := range messages {
		if message.Role != "user" {
			continue
		}
		text := strings.TrimSpace(message.TextContent)
		if text != "" {
			questions = append(questions, text)
		}
	}
	if len(questions) == 0 {
		return ""
	}
	if len(questions) == 1 || questions[0] == questions[len(questions)-1] {
		return questions[len(questions)-1]
	}
	return questions[0] + "\n후속 질문: " + questions[len(questions)-1]
}

func extractArtifactSections(text string, names ...string) string {
	wanted := make(map[string]bool, len(names))
	for _, name := range names {
		wanted[normalizeArtifactHeading(name)] = true
	}
	var parts []string
	var current []string
	keep := false
	flush := func() {
		if keep && len(current) > 0 {
			parts = append(parts, strings.TrimSpace(strings.Join(current, "\n")))
		}
		current = nil
	}
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "### ") {
			flush()
			heading := normalizeArtifactHeading(strings.TrimSpace(strings.TrimPrefix(trimmed, "### ")))
			keep = wanted[heading]
			continue
		}
		if keep {
			current = append(current, line)
		}
	}
	flush()
	return strings.TrimSpace(strings.Join(parts, "\n\n"))
}

func normalizeArtifactHeading(heading string) string {
	heading = strings.ToLower(strings.TrimSpace(heading))
	if index := strings.IndexByte(heading, '('); index >= 0 {
		heading = strings.TrimSpace(heading[:index])
	}
	return heading
}

func artifactSystems(text string) []string {
	var systems []string
	for _, pattern := range artifactToolPatterns {
		for _, match := range pattern.FindAllStringSubmatch(text, -1) {
			if len(match) > 1 {
				systems = append(systems, strings.ToLower(strings.TrimSpace(match[1])))
			}
		}
	}
	return sortedArtifactValues(systems, 16)
}

func artifactCodeRefs(text string) []string {
	var refs []string
	for _, match := range artifactCodeRefPattern.FindAllStringSubmatch(text, -1) {
		if len(match) > 1 {
			refs = append(refs, strings.TrimSpace(match[1]))
		}
	}
	return sortedArtifactValues(refs, 24)
}

func artifactBursts(messages []messageRecord) []ConversationBurst {
	var bursts []ConversationBurst
	var current *ConversationBurst
	flush := func() {
		if current == nil {
			return
		}
		current.Text = truncateArtifactText(current.Text, artifactBurstRunes)
		current.Signal = artifactBurstSignal(current.Text)
		if current.Signal >= 2 && len(bursts) < artifactMaxBursts {
			bursts = append(bursts, *current)
		}
		current = nil
	}
	for _, message := range messages {
		if message.Role != "user" && message.Role != "assistant" {
			flush()
			continue
		}
		text := strings.TrimSpace(message.TextContent)
		if text == "" {
			continue
		}
		if current == nil || current.Role != message.Role {
			flush()
			current = &ConversationBurst{Role: message.Role, MsgStart: message.MsgIndex, MsgEnd: message.MsgIndex, Text: text}
			continue
		}
		current.MsgEnd = message.MsgIndex
		current.Text += "\n" + text
	}
	flush()
	return bursts
}

func artifactBurstSignal(text string) int {
	runes := len([]rune(strings.TrimSpace(text)))
	score := 0
	if runes >= 200 {
		score++
	}
	if runes >= 500 {
		score++
	}
	if artifactSignalPattern.MatchString(text) {
		score++
	}
	if len(artifactCodeRefs(text)) > 0 || len(artifactSystems(text)) > 0 {
		score++
	}
	if artifactUniqueTerms(text) >= 12 {
		score++
	}
	return score
}

func artifactUniqueTerms(text string) int {
	seen := make(map[string]bool)
	for _, term := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' && r != '-'
	}) {
		if len([]rune(term)) >= 2 {
			seen[term] = true
		}
	}
	return len(seen)
}

func (a *ConversationArtifact) embeddingText() string {
	if a == nil {
		return ""
	}
	parts := []string{
		"question: " + a.Question,
		"summary: " + a.Summary,
		"resolution: " + a.Resolution,
	}
	if len(a.Systems) > 0 {
		parts = append(parts, "systems: "+strings.Join(a.Systems, ", "))
	}
	if len(a.CodeRefs) > 0 {
		parts = append(parts, "code_refs: "+strings.Join(a.CodeRefs, ", "))
	}
	return strings.TrimSpace(strings.Join(parts, "\n"))
}

func (a *ConversationArtifact) burstEmbeddingText(burst ConversationBurst) string {
	topic := ""
	if a != nil {
		topic = strings.TrimSpace(a.Question)
	}
	return strings.TrimSpace("topic: " + topic + "\n" + burst.Text)
}

func nonEmptyStrings(values ...string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			out = append(out, value)
		}
	}
	return out
}

func truncateArtifactText(text string, maxRunes int) string {
	text = strings.TrimSpace(text)
	if maxRunes <= 0 {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= maxRunes {
		return text
	}
	return strings.TrimSpace(string(runes[:maxRunes]))
}

func sortedArtifactValues(values []string, limit int) []string {
	seen := make(map[string]bool, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	sort.Strings(out)
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}
