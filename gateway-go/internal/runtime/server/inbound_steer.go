// inbound_steer.go — Parse `/steer <note>` vs `/steer <id> <note>` by
// consulting the live subagent registry instead of a syntactic heuristic.
//
// Phase 1 used a purely syntactic rule — "first token looks like a short
// numeric/hex id → subagent; otherwise main agent". That mis-routes in
// edge cases: a subagent with a word-like label, or a user typing
// `/steer abc123 please focus on X` where `abc123` happens to be prose.
//
// Phase 2 replaces the heuristic with an unambiguous registry lookup:
//   - if the first token matches a currently-active subagent for this
//     session → SteerSubagent (note = body after first token),
//   - otherwise → SteerMainAgent (note = full body).
//
// When the registry is nil (e.g. ACP not configured, early startup) we
// degrade to the Phase 1 heuristic so an inbound /steer is never silently
// dropped.
package server

import (
	"strings"
	"unicode"
)

const mainAgentSteerPrefix = "/steer"

// SteerKind classifies the outcome of parsing a /steer body.
type SteerKind int

const (
	// SteerNone — body is empty or not a /steer command. Caller should no-op.
	SteerNone SteerKind = iota
	// SteerMainAgent — body targets the main agent; note is the full nudge.
	SteerMainAgent
	// SteerSubagent — body targets a specific subagent identified by subagentID.
	SteerSubagent
)

// SubagentLookup is a minimal interface satisfied by any registry that can
// answer "does session `sessionKey` currently have an active subagent
// identified by `token`?". Implementations MUST be safe for concurrent use.
//
// A nil SubagentLookup is treated as "no registry available" — the parser
// then falls back to the Phase 1 syntactic heuristic.
type SubagentLookup interface {
	// HasSubagent returns true when `subagentID` resolves to a live subagent
	// owned by `sessionKey` (via index, run-id prefix, session key, or label
	// — whatever the concrete registry accepts).
	HasSubagent(sessionKey, subagentID string) bool
}

// parseSteerCommand decomposes a /steer body into either a main-agent nudge
// or a subagent-targeted nudge.
//
// Routing rules (in order):
//  1. empty / not /steer → SteerNone.
//  2. if `registry` is non-nil AND registry.HasSubagent(sessionKey, firstToken)
//     returns true → SteerSubagent (note = rest of body after firstToken).
//  3. if `registry` is nil → Phase 1 syntactic heuristic
//     (numeric/hex-looking first token → SteerNone so the subagent
//     dispatcher can handle it; otherwise SteerMainAgent).
//  4. else → SteerMainAgent (note = full body).
//
// sessionKey scopes the registry lookup — subagents are owned per session.
// Returns (SteerNone, "", "") when the body is not a /steer at all.
func parseSteerCommand(body, sessionKey string, registry SubagentLookup) (kind SteerKind, note, subagentID string) {
	rest, ok := stripSteerPrefix(body)
	if !ok {
		return SteerNone, "", ""
	}
	firstToken, remainder := splitFirstToken(rest)
	if firstToken == "" {
		return SteerNone, "", ""
	}

	if registry != nil && registry.HasSubagent(sessionKey, firstToken) {
		return SteerSubagent, remainder, firstToken
	}

	if registry == nil && looksLikeSubagentID(firstToken) {
		return SteerNone, "", ""
	}

	return SteerMainAgent, rest, ""
}

// parseMainAgentSteerCommand preserves the Phase 1 signature for callers
// that don't have a registry to consult. Deprecated: prefer parseSteerCommand.
func parseMainAgentSteerCommand(body string) (string, bool) {
	kind, note, _ := parseSteerCommand(body, "", nil)
	if kind == SteerMainAgent {
		return note, true
	}
	return "", false
}

// stripSteerPrefix returns the trimmed remainder of body after the
// `/steer ` prefix (case-insensitive) plus required whitespace separator.
func stripSteerPrefix(body string) (string, bool) {
	trimmed := strings.TrimSpace(body)
	if trimmed == "" {
		return "", false
	}
	if len(trimmed) < len(mainAgentSteerPrefix) {
		return "", false
	}
	if !strings.EqualFold(trimmed[:len(mainAgentSteerPrefix)], mainAgentSteerPrefix) {
		return "", false
	}
	rest := trimmed[len(mainAgentSteerPrefix):]
	if rest == "" || !unicode.IsSpace(rune(rest[0])) {
		return "", false
	}
	rest = strings.TrimSpace(rest)
	if rest == "" {
		return "", false
	}
	return rest, true
}

// splitFirstToken returns the first whitespace-delimited token of s and
// the remainder (with leading whitespace trimmed).
func splitFirstToken(s string) (firstToken, remainder string) {
	end := 0
	for end < len(s) {
		if unicode.IsSpace(rune(s[end])) {
			break
		}
		end++
	}
	firstToken = s[:end]
	remainder = strings.TrimSpace(s[end:])
	return firstToken, remainder
}

// looksLikeSubagentID returns true when token is structurally a subagent id.
// Used only as a fallback when no registry is available.
func looksLikeSubagentID(token string) bool {
	if token == "" {
		return false
	}
	if len(token) <= 3 && isAllDigits(token) {
		return true
	}
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
