// queue_directive.go — Full /queue directive extraction with token-based argument parsing.
// Mirrors src/auto-reply/reply/queue/directive.ts (120 LOC).
// Supports: /queue <mode> [debounce=<ms>] [cap=<n>] [drop=<policy>]
package autoreply

import (
	"math"
	"regexp"
	"strconv"
	"strings"
)

// QueueDirectiveResult holds the result of extracting a /queue directive.
type QueueDirectiveResult struct {
	Cleaned      string
	HasDirective bool
	QueueMode    QueueMode
	QueueReset   bool
	RawMode      string
	DebounceMs   *int // nil = not specified
	Cap          *int // nil = not specified
	DropPolicy   QueueDropPolicy
	RawDebounce  string
	RawCap       string
	RawDrop      string
	HasOptions   bool
}

var queueDirectiveRe2 = regexp.MustCompile(`(?i)(?:^|\s)/queue(?:$|\s|:)`)

// NormalizeQueueMode normalizes a raw queue mode string.
// Supports the full set: auto, manual, off, steer, followup, collect, interrupt, queue.
func NormalizeQueueMode(raw string) QueueMode {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "auto":
		return QueueModeAuto
	case "manual":
		return QueueModeManual
	case "off", "disable", "disabled":
		return QueueModeOff
	}
	return ""
}

// NormalizeQueueDropPolicy normalizes a drop policy string.
func NormalizeQueueDropPolicy(raw string) QueueDropPolicy {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "old", "oldest":
		return QueueDropOldest
	case "new", "newest":
		return QueueDropNewest
	}
	return ""
}

// parseQueueDebounce parses a debounce value (number or duration string).
func parseQueueDebounce(raw string) *int {
	if raw == "" {
		return nil
	}
	trimmed := strings.TrimSpace(raw)
	// Try parsing as plain number (milliseconds).
	if ms, err := strconv.Atoi(trimmed); err == nil && ms >= 0 {
		return &ms
	}
	// Try duration suffixes: 500ms, 1s, 2m.
	lower := strings.ToLower(trimmed)
	if strings.HasSuffix(lower, "ms") {
		if v, err := strconv.ParseFloat(lower[:len(lower)-2], 64); err == nil && v >= 0 {
			ms := int(math.Round(v))
			return &ms
		}
	}
	if strings.HasSuffix(lower, "s") && !strings.HasSuffix(lower, "ms") {
		if v, err := strconv.ParseFloat(lower[:len(lower)-1], 64); err == nil && v >= 0 {
			ms := int(math.Round(v * 1000))
			return &ms
		}
	}
	return nil
}

// parseQueueCap parses a cap value.
func parseQueueCap(raw string) *int {
	if raw == "" {
		return nil
	}
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || n < 1 {
		return nil
	}
	return &n
}

// parseQueueDirectiveArgs parses token-based arguments after /queue.
// Uses SkipDirectiveArgPrefix + TakeDirectiveToken for sequential parsing.
func parseQueueDirectiveArgs(raw string) (result QueueDirectiveResult, consumed int) {
	n := len(raw)
	i := SkipDirectiveArgPrefix(raw)
	consumed = i

	for i < n {
		token, nextI := TakeDirectiveToken(raw, i)
		if token == "" {
			break
		}
		lowered := strings.ToLower(strings.TrimSpace(token))

		// Reset/clear.
		if lowered == "default" || lowered == "reset" || lowered == "clear" {
			result.QueueReset = true
			consumed = nextI
			break
		}

		// Key=value or key:value options.
		if strings.HasPrefix(lowered, "debounce:") || strings.HasPrefix(lowered, "debounce=") {
			parts := strings.SplitN(token, string([]byte{token[len("debounce")]}), 2)
			if len(parts) > 1 {
				result.RawDebounce = parts[1]
				result.DebounceMs = parseQueueDebounce(parts[1])
				result.HasOptions = true
			}
			i = nextI
			consumed = i
			continue
		}
		if strings.HasPrefix(lowered, "cap:") || strings.HasPrefix(lowered, "cap=") {
			parts := strings.SplitN(token, string([]byte{token[len("cap")]}), 2)
			if len(parts) > 1 {
				result.RawCap = parts[1]
				result.Cap = parseQueueCap(parts[1])
				result.HasOptions = true
			}
			i = nextI
			consumed = i
			continue
		}
		if strings.HasPrefix(lowered, "drop:") || strings.HasPrefix(lowered, "drop=") {
			parts := strings.SplitN(token, string([]byte{token[len("drop")]}), 2)
			if len(parts) > 1 {
				result.RawDrop = parts[1]
				result.DropPolicy = NormalizeQueueDropPolicy(parts[1])
				result.HasOptions = true
			}
			i = nextI
			consumed = i
			continue
		}

		// Try as queue mode.
		mode := NormalizeQueueMode(token)
		if mode != "" {
			result.QueueMode = mode
			result.RawMode = token
			i = nextI
			consumed = i
			continue
		}

		// Unrecognized token — stop consuming.
		break
	}

	return result, consumed
}

// ExtractQueueDirective extracts a /queue directive from the message body.
func ExtractQueueDirective(body string) QueueDirectiveResult {
	if body == "" {
		return QueueDirectiveResult{Cleaned: ""}
	}

	loc := queueDirectiveRe2.FindStringIndex(body)
	if loc == nil {
		return QueueDirectiveResult{Cleaned: strings.TrimSpace(body)}
	}

	// Find exact position of "/queue" in match.
	matchStr := body[loc[0]:loc[1]]
	qIdx := strings.Index(strings.ToLower(matchStr), "/queue")
	start := loc[0] + qIdx
	argsStart := start + len("/queue")

	parsed, consumed := parseQueueDirectiveArgs(body[argsStart:])

	cleanedRaw := body[:start] + " " + body[argsStart+consumed:]
	cleaned := strings.TrimSpace(multiSpaceRe.ReplaceAllString(cleanedRaw, " "))

	parsed.Cleaned = cleaned
	parsed.HasDirective = true
	return parsed
}
