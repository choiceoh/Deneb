package reply

import (
	"regexp"
	"strings"
)

// toolCallTagRe matches <tool_call>...</tool_call> blocks.
var toolCallTagRe = regexp.MustCompile(`(?s)<tool_call>.*?</tool_call>`)

// jsonToolCallRe matches JSON-style tool call blocks.
var jsonToolCallRe = regexp.MustCompile(`(?s)\{"(?:name|type)":\s*"(?:function|tool_use|[a-z_]+)"[^}]*"(?:arguments|input|parameters)":\s*\{[^}]*\}\s*\}`)

// pipeFunctionRe matches model-specific function call tokens.
var pipeFunctionRe = regexp.MustCompile(`<\|(?:python_tag|function|tool_call)\|>[^\n]*(?:\n|$)`)

// bracketToolCallRe matches [tool:NAME(ARGS)] patterns.
var bracketToolCallRe = regexp.MustCompile(`(?ms)^\[tool:[a-z_]+\(.*\)\]\s*$`)
var bracketResultRe = regexp.MustCompile(`(?m)^\[result → .*\]\s*$`)

// bracketToolCallUnterminatedRe catches truncated tool calls.
var bracketToolCallUnterminatedRe = regexp.MustCompile(`(?s)\[tool:[a-z_]+\(.*`)

// bareToolNameRe matches bare [toolname] patterns on their own line.
var bareToolNameRe = regexp.MustCompile(`(?m)^\[([a-z][a-z0-9_]*)\]\s*$`)

// koreanToolCallRe matches Korean-formatted tool call lines.
var koreanToolCallRe = regexp.MustCompile(`(?m)^—\s+[a-z][a-z0-9_.]*\s+사용:.*$`)

// koreanToolResultRe matches tool result arrow lines.
var koreanToolResultRe = regexp.MustCompile(`(?m)^\s+↳\s+.*$`)

// StripLeakedToolCallMarkup removes leaked tool-call envelope text that should
// stay internal. Handles multiple model-specific formats:
//   - Llama-style: <function=name>...</tool_call>
//   - XML-style: <tool_call>...</tool_call>
//   - JSON-style: {"name": "tool_name", "arguments": {...}}
//   - Special tokens: <|python_tag|>, <|function|>, <|tool_call|>
//   - Bracket-style: [tool:NAME(ARGS)], [result → ...]
//   - Korean-style: — tool_name 사용: {args}
func StripLeakedToolCallMarkup(text string) string {
	trimmed := text

	for {
		start := strings.Index(trimmed, "<function=")
		if start < 0 {
			break
		}
		end := strings.Index(trimmed[start:], "</tool_call>")
		if end < 0 {
			break
		}
		end += start + len("</tool_call>")
		trimmed = strings.TrimSpace(trimmed[:start] + "\n" + trimmed[end:])
	}

	trimmed = toolCallTagRe.ReplaceAllString(trimmed, "")
	trimmed = jsonToolCallRe.ReplaceAllString(trimmed, "")
	trimmed = pipeFunctionRe.ReplaceAllString(trimmed, "")
	trimmed = bracketToolCallRe.ReplaceAllString(trimmed, "")
	trimmed = bracketResultRe.ReplaceAllString(trimmed, "")
	trimmed = bracketToolCallUnterminatedRe.ReplaceAllString(trimmed, "")
	trimmed = bareToolNameRe.ReplaceAllString(trimmed, "")
	trimmed = koreanToolCallRe.ReplaceAllString(trimmed, "")
	trimmed = koreanToolResultRe.ReplaceAllString(trimmed, "")

	return strings.TrimSpace(trimmed)
}

var fencedCodeBlockRe = regexp.MustCompile("(?s)```[a-zA-Z]*\\n?.*?```")

// StripFencedCodeBlocks removes fenced code blocks from text.
func StripFencedCodeBlocks(text string) string {
	return strings.TrimSpace(fencedCodeBlockRe.ReplaceAllString(text, ""))
}

// SanitizeDraftText applies all draft-time filters: leaked tool call markup,
// fenced code blocks, and trailing whitespace.
func SanitizeDraftText(text string) string {
	text = StripLeakedToolCallMarkup(text)
	if idx := strings.LastIndex(text, "[tool:"); idx >= 0 {
		text = text[:idx]
	}
	text = bareToolNameRe.ReplaceAllString(text, "")
	text = StripFencedCodeBlocks(text)
	return strings.TrimSpace(text)
}
