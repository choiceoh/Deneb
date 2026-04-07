// queue_directive.go — Parsing /queue directive from message text.
// Simplified: mode and drop policy options removed (single-user bot
// always uses collect + summarize). Only reset, debounce, and cap
// are supported.
package queue

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

// QueueDirective holds the parsed result of a /queue directive.
type QueueDirective struct {
	Cleaned      string
	HasDirective bool
	QueueReset   bool
	DebounceMs   int
	Cap          int
	RawDebounce  string
	RawCap       string
	HasOptions   bool
}

var queueDirectiveRe2 = regexp.MustCompile(`(?i)(?:^|\s)/queue(?:$|\s|:)`)

// ExtractQueueDirective extracts and removes a /queue directive from the message body.
func ExtractQueueDirective(body string) QueueDirective {
	if body == "" {
		return QueueDirective{}
	}

	match := queueDirectiveRe2.FindStringIndex(body)
	if match == nil {
		return QueueDirective{Cleaned: strings.TrimSpace(body)}
	}

	// Find where "/queue" starts within the match.
	matchText := body[match[0]:match[1]]
	queueIdx := strings.Index(strings.ToLower(matchText), "/queue")
	start := match[0] + queueIdx
	argsStart := start + len("/queue")
	argsText := body[argsStart:]

	parsed := parseQueueDirectiveArgs(argsText)

	// Reconstruct cleaned body.
	before := body[:start]
	after := body[argsStart+parsed.consumed:]
	cleaned := strings.TrimSpace(compactSpaces(before + " " + after))

	return QueueDirective{
		Cleaned:      cleaned,
		HasDirective: true,
		QueueReset:   parsed.queueReset,
		DebounceMs:   parsed.debounceMs,
		Cap:          parsed.cap,
		RawDebounce:  parsed.rawDebounce,
		RawCap:       parsed.rawCap,
		HasOptions:   parsed.hasOptions,
	}
}

type queueDirectiveParseResult struct {
	consumed    int
	queueReset  bool
	debounceMs  int
	cap         int
	rawDebounce string
	rawCap      string
	hasOptions  bool
}

func parseQueueDirectiveArgs(raw string) queueDirectiveParseResult {
	result := queueDirectiveParseResult{}
	i := skipDirectiveArgPrefix(raw)
	result.consumed = i

	for i < len(raw) {
		token, nextIndex := takeDirectiveToken(raw, i)
		if token == "" {
			break
		}
		i = nextIndex
		lowered := strings.TrimSpace(strings.ToLower(token))

		// Reset/clear keyword.
		if lowered == "default" || lowered == "reset" || lowered == "clear" {
			result.queueReset = true
			result.consumed = i
			break
		}

		// debounce:VALUE
		if strings.HasPrefix(lowered, "debounce:") || strings.HasPrefix(lowered, "debounce=") {
			parts := strings.SplitN(token, string(token[len("debounce")]), 2)
			if len(parts) > 1 {
				result.rawDebounce = parts[1]
				result.debounceMs = parseQueueDebounce(parts[1])
			}
			result.hasOptions = true
			result.consumed = i
			continue
		}

		// cap:VALUE
		if strings.HasPrefix(lowered, "cap:") || strings.HasPrefix(lowered, "cap=") {
			parts := strings.SplitN(token, string(token[3]), 2)
			if len(parts) > 1 {
				result.rawCap = parts[1]
				result.cap = parseQueueCap(parts[1])
			}
			result.hasOptions = true
			result.consumed = i
			continue
		}

		// Mode and drop policy tokens are silently ignored (single-user bot,
		// always collect + summarize).
		if isKnownQueueToken(lowered) {
			result.consumed = i
			continue
		}

		// Unrecognized token — stop.
		break
	}
	return result
}

// isKnownQueueToken returns true for formerly-valid mode/drop tokens so that
// old /queue directives don't break (they're just ignored).
func isKnownQueueToken(lowered string) bool {
	switch lowered {
	case "queue", "queued", "steer", "steering", "interrupt", "interrupts",
		"abort", "followup", "follow-ups", "followups", "collect", "coalesce",
		"steer+backlog", "steer-backlog", "steer_backlog":
		return true
	}
	if strings.HasPrefix(lowered, "drop:") || strings.HasPrefix(lowered, "drop=") {
		return true
	}
	return false
}

// parseQueueDebounce parses a duration string into milliseconds.
func parseQueueDebounce(raw string) int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	// Try plain number (ms).
	if ms, err := strconv.Atoi(raw); err == nil && ms >= 0 {
		return ms
	}
	// Try Go duration.
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0
	}
	ms := int(d.Milliseconds())
	if ms < 0 {
		return 0
	}
	return ms
}

// parseQueueCap parses a capacity string into an integer.
func parseQueueCap(raw string) int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		return 0
	}
	return n
}

// skipDirectiveArgPrefix skips leading whitespace and optional `:` after the directive.
func skipDirectiveArgPrefix(raw string) int {
	i := 0
	for i < len(raw) && (raw[i] == ' ' || raw[i] == '\t') {
		i++
	}
	if i < len(raw) && raw[i] == ':' {
		i++
	}
	for i < len(raw) && (raw[i] == ' ' || raw[i] == '\t') {
		i++
	}
	return i
}

// takeDirectiveToken extracts the next whitespace-delimited token.
func takeDirectiveToken(raw string, start int) (token string, end int) {
	i := start
	for i < len(raw) && (raw[i] == ' ' || raw[i] == '\t') {
		i++
	}
	if i >= len(raw) || raw[i] == '\n' || raw[i] == '\r' {
		return "", i
	}
	j := i
	for j < len(raw) && raw[j] != ' ' && raw[j] != '\t' && raw[j] != '\n' && raw[j] != '\r' {
		j++
	}
	return raw[i:j], j
}

// compactSpaces replaces runs of whitespace with a single space.
func compactSpaces(s string) string {
	var b strings.Builder
	inSpace := false
	for _, r := range s {
		if r == ' ' || r == '\t' {
			if !inSpace {
				b.WriteRune(' ')
				inSpace = true
			}
		} else {
			b.WriteRune(r)
			inSpace = false
		}
	}
	return b.String()
}
