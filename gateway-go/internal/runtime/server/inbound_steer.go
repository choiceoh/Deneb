// inbound_steer.go — Parse `/steer <note>` for main-agent mid-run nudges.
//
// Main agent ported from Hermes' `/steer <note>` — queues a note for the
// running agent's next tool_result. Distinct from the subagent
// `/steer <id> <note>` form, which targets a child run.
//
// Distinguishing heuristic:
//   - If the body starts with /steer and is followed by a note whose FIRST
//     non-prefix token is NOT a plausible subagent id (short numeric, or
//     a recognizable run-id/label prefix), treat as main-agent steer.
//   - Otherwise, defer to the subagent dispatcher which owns the
//     `/steer <id> <note>` flow.
//
// This parser is intentionally syntactic-only (no registry lookup); the
// caller attempts main-agent enqueue first, and on failure the existing
// subagent dispatcher takes over (which then consults live run state).
package server

import (
	"strings"
	"unicode"
)

const mainAgentSteerPrefix = "/steer"

// parseMainAgentSteerCommand inspects body for the `/steer <note>` form
// used by the main agent. Returns (note, true) when the body is a
// plausible main-agent steer, or ("", false) otherwise.
//
// Rules:
//   - body must start with "/steer " (case-insensitive) followed by text.
//   - the remainder must have length > 0 after trimming.
//   - the FIRST token of the remainder must NOT look like a subagent id.
//     A "plausible id" is either:
//   - a 1-3 digit positive integer (index into the subagents list), or
//   - a hex-looking prefix (all chars in [0-9a-fA-F], length >= 4)
//     matching the run-id prefix heuristic.
//
// When the first token is a plain word (letters/Hangul/general text) we
// route to the main agent; the subagent dispatcher still handles every
// other form (/steer 1 go, /steer abc12345 go).
func parseMainAgentSteerCommand(body string) (string, bool) {
	trimmed := strings.TrimSpace(body)
	if trimmed == "" {
		return "", false
	}
	// Case-insensitive prefix check; the slash command itself is ascii.
	if len(trimmed) < len(mainAgentSteerPrefix) {
		return "", false
	}
	if !strings.EqualFold(trimmed[:len(mainAgentSteerPrefix)], mainAgentSteerPrefix) {
		return "", false
	}
	rest := trimmed[len(mainAgentSteerPrefix):]
	// Require at least one whitespace separator (avoid matching /steerage or
	// /steer-at-target).
	if rest == "" || !unicode.IsSpace(rune(rest[0])) {
		return "", false
	}
	rest = strings.TrimSpace(rest)
	if rest == "" {
		return "", false
	}
	// Inspect the first token — if it LOOKS like a subagent id, let the
	// subagent path handle it.
	firstEnd := 0
	for firstEnd < len(rest) {
		r := rune(rest[firstEnd])
		if unicode.IsSpace(r) {
			break
		}
		firstEnd++
	}
	firstToken := rest[:firstEnd]
	if looksLikeSubagentID(firstToken) {
		return "", false
	}
	// The whole rest — including the first token — is the note.
	return rest, true
}

// looksLikeSubagentID returns true when token is structurally a subagent id
// (numeric index or hex/runid prefix). Strings with mixed ASCII + Hangul,
// punctuation, or any spaces are never ids.
func looksLikeSubagentID(token string) bool {
	if token == "" {
		return false
	}
	// Numeric index (1-3 digits).
	if len(token) <= 3 && isAllDigits(token) {
		return true
	}
	// Hex-looking run-id prefix (>= 4 chars, all hex).
	if len(token) >= 4 && isAllHex(token) {
		return true
	}
	return false
}

func isAllDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func isAllHex(s string) bool {
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9':
		case r >= 'a' && r <= 'f':
		case r >= 'A' && r <= 'F':
		default:
			return false
		}
	}
	return true
}
