// queue_directive.go — Parsing /queue directive from message text.
// Mirrors src/auto-reply/reply/queue/directive.ts (177 LOC).
package autoreply

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
	QueueMode    FollowupQueueMode
	QueueReset   bool
	RawMode      string
	DebounceMs   int
	Cap          int
	DropPolicy   FollowupDropPolicy
	RawDebounce  string
	RawCap       string
	RawDrop      string
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
		QueueMode:    parsed.queueMode,
		QueueReset:   parsed.queueReset,
		RawMode:      parsed.rawMode,
		DebounceMs:   parsed.debounceMs,
		Cap:          parsed.cap,
		DropPolicy:   parsed.dropPolicy,
		RawDebounce:  parsed.rawDebounce,
		RawCap:       parsed.rawCap,
		RawDrop:      parsed.rawDrop,
		HasOptions:   parsed.hasOptions,
	}
}

type queueDirectiveParseResult struct {
	consumed    int
	queueMode   FollowupQueueMode
	queueReset  bool
	rawMode     string
	debounceMs  int
	cap         int
	dropPolicy  FollowupDropPolicy
	rawDebounce string
	rawCap      string
	rawDrop     string
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

		// drop:VALUE
		if strings.HasPrefix(lowered, "drop:") || strings.HasPrefix(lowered, "drop=") {
			parts := strings.SplitN(token, string(token[4]), 2)
			if len(parts) > 1 {
				result.rawDrop = parts[1]
				result.dropPolicy = NormalizeFollowupDropPolicy(parts[1])
			}
			result.hasOptions = true
			result.consumed = i
			continue
		}

		// Try as mode.
		mode := NormalizeFollowupQueueMode(token)
		if mode != "" {
			result.queueMode = mode
			result.rawMode = token
			result.consumed = i
			continue
		}

		// Unrecognized token — stop.
		break
	}
	return result
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
func takeDirectiveToken(raw string, start int) (string, int) {
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
