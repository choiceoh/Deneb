// Package autoreply implements the auto-reply pipeline for the Go gateway.
//
// This mirrors src/auto-reply/ from the TypeScript codebase: command registry,
// dispatch, text chunking, inbound debouncing, heartbeat token handling,
// group activation, model directive extraction, and fallback state tracking.
package tokens

import (
	"regexp"
	"strings"
	"sync"
	"unicode"
)

// Well-known tokens used by the agent to signal special reply behavior.
const (
	HeartbeatToken   = "HEARTBEAT_OK"
	SilentReplyToken = "NO_REPLY"
)

// Cached compiled regexes for silent token detection, keyed by token string.
var (
	silentExactMu sync.RWMutex
	silentExactRe = map[string]*regexp.Regexp{}

	silentTrailingMu sync.RWMutex
	silentTrailingRe = map[string]*regexp.Regexp{}
)

func getSilentExactRegex(token string) *regexp.Regexp {
	silentExactMu.RLock()
	re, ok := silentExactRe[token]
	silentExactMu.RUnlock()
	if ok {
		return re
	}
	escaped := regexp.QuoteMeta(token)
	re = regexp.MustCompile(`^\s*` + escaped + `\s*$`)
	silentExactMu.Lock()
	silentExactRe[token] = re
	silentExactMu.Unlock()
	return re
}

func getSilentTrailingRegex(token string) *regexp.Regexp {
	silentTrailingMu.RLock()
	re, ok := silentTrailingRe[token]
	silentTrailingMu.RUnlock()
	if ok {
		return re
	}
	escaped := regexp.QuoteMeta(token)
	re = regexp.MustCompile(`(?:^|\s+|\*+)` + escaped + `\s*$`)
	silentTrailingMu.Lock()
	silentTrailingRe[token] = re
	silentTrailingMu.Unlock()
	return re
}

// IsSilentReplyText returns true if text is exactly the given silent token
// (with optional surrounding whitespace). An empty token defaults to NO_REPLY.
func IsSilentReplyText(text, token string) bool {
	if text == "" {
		return false
	}
	if token == "" {
		token = SilentReplyToken
	}
	return getSilentExactRegex(token).MatchString(text)
}

// StripSilentToken removes a trailing silent reply token from mixed-content
// text. Returns the remaining text with the token removed (trimmed).
func StripSilentToken(text, token string) string {
	if token == "" {
		token = SilentReplyToken
	}
	return strings.TrimSpace(getSilentTrailingRegex(token).ReplaceAllString(text, ""))
}

// IsSilentReplyPrefixText returns true if text is an uppercase prefix fragment
// of the silent token (e.g., "NO" from a streaming NO_REPLY).
func IsSilentReplyPrefixText(text, token string) bool {
	if text == "" {
		return false
	}
	if token == "" {
		token = SilentReplyToken
	}
	trimmed := strings.TrimLeftFunc(text, unicode.IsSpace)
	if trimmed == "" || len(trimmed) < 2 {
		return false
	}
	// Guard against suppressing natural-language "No..." text while still
	// catching uppercase lead fragments like "NO" from streamed NO_REPLY.
	if trimmed != strings.ToUpper(trimmed) {
		return false
	}
	normalized := strings.ToUpper(trimmed)
	for _, r := range normalized {
		if r != '_' && (r < 'A' || r > 'Z') {
			return false
		}
	}
	tokenUpper := strings.ToUpper(token)
	if !strings.HasPrefix(tokenUpper, normalized) {
		return false
	}
	if strings.Contains(normalized, "_") {
		return true
	}
	// Allow bare "NO" only for NO_REPLY streaming.
	return tokenUpper == strings.ToUpper(SilentReplyToken) && normalized == "NO"
}
