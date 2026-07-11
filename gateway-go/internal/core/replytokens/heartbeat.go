package tokens

import (
	"regexp"
	"strings"
)

// Default heartbeat configuration constants.
const (
	HeartbeatPrompt          = "Read HEARTBEAT.md if it exists (workspace context). Follow it strictly. Do not infer or repeat old tasks from prior chats. If nothing needs attention, reply HEARTBEAT_OK."
	DefaultHeartbeatEvery    = "30m"
	DefaultHeartbeatAckChars = 300
)

// StripHeartbeatMode controls how heartbeat token stripping handles residual text.
type StripHeartbeatMode string

const (
	StripModeHeartbeat StripHeartbeatMode = "heartbeat"
	StripModeMessage   StripHeartbeatMode = "message"
)

// IsHeartbeatContentEffectivelyEmpty returns true if the HEARTBEAT.md content
// has no actionable tasks (only whitespace, markdown headers, and empty list items).
func IsHeartbeatContentEffectivelyEmpty(content string) bool {
	if content == "" {
		return true
	}
	headerRe := regexp.MustCompile(`^#+(\s|$)`)
	emptyListRe := regexp.MustCompile(`^[-*+]\s*(\[[\sXx]?\]\s*)?$`)
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if headerRe.MatchString(trimmed) {
			continue
		}
		if emptyListRe.MatchString(trimmed) {
			continue
		}
		return false
	}
	return true
}

// ResolveHeartbeatPrompt returns the configured heartbeat prompt or the default.
func ResolveHeartbeatPrompt(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return HeartbeatPrompt
	}
	return trimmed
}

// StripHeartbeatResult holds the outcome of stripping HEARTBEAT_OK from a reply.
type StripHeartbeatResult struct {
	ShouldSkip bool
	Text       string
	DidStrip   bool
}

// stripTokenAtEdges recursively removes HEARTBEAT_OK from the start and end
// of text, including up to 4 trailing non-word characters.
func stripTokenAtEdges(raw string) (string, bool) {
	text := strings.TrimSpace(raw)
	if text == "" {
		return "", false
	}
	if !strings.Contains(text, HeartbeatToken) {
		return text, false
	}

	tokenEndRe := regexp.MustCompile(regexp.QuoteMeta(HeartbeatToken) + `[^a-zA-Z0-9_]{0,4}$`)
	didStrip := false

	for {
		changed := false
		next := strings.TrimSpace(text)

		if strings.HasPrefix(next, HeartbeatToken) {
			after := strings.TrimLeftFunc(next[len(HeartbeatToken):], func(r rune) bool {
				return r == ' ' || r == '\t'
			})
			text = after
			didStrip = true
			changed = true
			continue
		}

		if tokenEndRe.MatchString(next) {
			idx := strings.LastIndex(next, HeartbeatToken)
			before := strings.TrimRightFunc(next[:idx], func(r rune) bool {
				return r == ' ' || r == '\t'
			})
			if before == "" {
				text = ""
			} else {
				after := strings.TrimLeftFunc(next[idx+len(HeartbeatToken):], func(r rune) bool {
					return r == ' ' || r == '\t'
				})
				text = strings.TrimRight(before+after, " \t")
			}
			didStrip = true
			changed = true
		}

		if !changed {
			break
		}
	}

	// Collapse whitespace.
	collapsed := collapseWhitespace(text)
	return collapsed, didStrip
}

var wsCollapseRe = regexp.MustCompile(`\s+`)

func collapseWhitespace(s string) string {
	return strings.TrimSpace(wsCollapseRe.ReplaceAllString(s, " "))
}

// StripHeartbeatToken removes the HEARTBEAT_OK token from agent output and
// decides whether the reply should be skipped (suppressed).
func StripHeartbeatToken(raw string, mode StripHeartbeatMode, maxAckChars int) StripHeartbeatResult {
	if raw == "" {
		return StripHeartbeatResult{ShouldSkip: true}
	}
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return StripHeartbeatResult{ShouldSkip: true}
	}

	if mode == "" {
		mode = StripModeMessage
	}
	if maxAckChars <= 0 {
		maxAckChars = DefaultHeartbeatAckChars
	}

	if !strings.Contains(trimmed, HeartbeatToken) {
		return StripHeartbeatResult{Text: trimmed}
	}

	stripped, didStrip := stripTokenAtEdges(trimmed)
	if !didStrip {
		return StripHeartbeatResult{Text: trimmed}
	}
	if stripped == "" {
		return StripHeartbeatResult{ShouldSkip: true, DidStrip: true}
	}

	rest := strings.TrimSpace(stripped)
	if mode == StripModeHeartbeat && len(rest) <= maxAckChars {
		return StripHeartbeatResult{ShouldSkip: true, DidStrip: true}
	}
	return StripHeartbeatResult{Text: rest, DidStrip: true}
}
