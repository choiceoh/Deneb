// btw_command.go — /btw command detection and extraction.
// Mirrors src/auto-reply/reply/btw-command.ts (26 LOC).
package commands

import (
	"regexp"
	"strings"
)

var (
	btwCommandRe = regexp.MustCompile(`(?i)^/btw(?::|\s|$)`)
	// Matches /btw with optional whitespace args OR colon args:
	// /btw question  →  "question"
	// /btw:question  →  "question"
	// /btw           →  ""
	btwExtractRe = regexp.MustCompile(`(?i)^/btw(?:(?:\s+|:\s*)([\s\S]*))?$`)
)

// IsBtwRequestText returns true if the text is a /btw command.
func IsBtwRequestText(text, botUsername string, registry *CommandRegistry) bool {
	if text == "" {
		return false
	}
	normalized := text
	if registry != nil {
		normalized = registry.NormalizeCommandBody(text, botUsername)
	}
	normalized = strings.TrimSpace(normalized)
	return btwCommandRe.MatchString(normalized)
}

// ExtractBtwQuestion extracts the question body from a /btw command.
// Returns ("", true) for bare /btw, (question, true) for /btw <question>,
// or ("", false) if not a /btw command.
func ExtractBtwQuestion(text, botUsername string, registry *CommandRegistry) (string, bool) {
	if text == "" {
		return "", false
	}
	normalized := text
	if registry != nil {
		normalized = registry.NormalizeCommandBody(text, botUsername)
	}
	normalized = strings.TrimSpace(normalized)
	m := btwExtractRe.FindStringSubmatch(normalized)
	if m == nil {
		return "", false
	}
	if len(m) > 1 && m[1] != "" {
		return strings.TrimSpace(m[1]), true
	}
	return "", true
}
