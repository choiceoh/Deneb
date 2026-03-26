// directive_handling_parse.go — Full directive parser with exec and queue options.
// Mirrors src/auto-reply/reply/directive-handling.parse.ts (229 LOC).
// Extends the basic ParseInlineDirectives with exec directive and queue options.
package autoreply

import (
	"regexp"
	"strconv"
	"strings"
)

// FullInlineDirectives extends InlineDirectives with exec directive and queue
// option fields that the basic ParseInlineDirectives does not populate.
type FullInlineDirectives struct {
	InlineDirectives

	// Exec directive fields.
	HasExecDirective bool
	ExecHost         ExecHost
	ExecSecurity     ExecSecurity
	ExecAsk          ExecAsk
	ExecNode         string
	RawExecHost      string
	RawExecSecurity  string
	RawExecAsk       string
	RawExecNode      string
	HasExecOptions   bool
	InvalidExecHost  bool
	InvalidExecSec   bool
	InvalidExecAsk   bool
	InvalidExecNode  bool

	// Queue options.
	QueueMode      QueueMode
	DebounceMs     int
	Cap            int
	DropPolicy     QueueDropPolicy
	RawDebounce    string
	RawCap         string
	RawDrop        string
	HasQueueOpts   bool
}

// FullDirectiveParseOptions configures the full directive parser.
type FullDirectiveParseOptions struct {
	ModelAliases         []string
	DisableElevated      bool
	AllowStatusDirective bool // defaults to true if unset
}

// queueOptionRe matches key=value options after the /queue directive.
var queueOptionRe = regexp.MustCompile(`(?i)\b(debounce|cap|drop)\s*[=:]\s*([^\s]+)`)

// ParseFullInlineDirectives parses all inline directives from a message body,
// including /exec and full /queue options. This matches the full TS pipeline
// from directive-handling.parse.ts.
func ParseFullInlineDirectives(body string, opts *FullDirectiveParseOptions) FullInlineDirectives {
	if opts == nil {
		opts = &FullDirectiveParseOptions{AllowStatusDirective: true}
	}
	allowStatus := opts.AllowStatusDirective

	// Step 1: Parse basic directives (think, verbose, fast, reasoning, elevated, status, model, queue).
	basic := ParseInlineDirectives(body, &DirectiveParseOptions{
		ModelAliases:    opts.ModelAliases,
		DisableElevated: opts.DisableElevated,
		DisableStatus:   !allowStatus,
	})

	result := FullInlineDirectives{
		InlineDirectives: basic,
	}

	// Step 2: Extract /exec directive from the cleaned output.
	execParsed := ExtractExecDirective(result.Cleaned)
	if execParsed.HasDirective {
		result.HasExecDirective = true
		result.ExecHost = execParsed.ExecHost
		result.ExecSecurity = execParsed.ExecSecurity
		result.ExecAsk = execParsed.ExecAsk
		result.ExecNode = execParsed.ExecNode
		result.RawExecHost = execParsed.RawExecHost
		result.RawExecSecurity = execParsed.RawExecSecurity
		result.RawExecAsk = execParsed.RawExecAsk
		result.RawExecNode = execParsed.RawExecNode
		result.HasExecOptions = execParsed.HasExecOptions
		result.InvalidExecHost = execParsed.InvalidHost
		result.InvalidExecSec = execParsed.InvalidSecurity
		result.InvalidExecAsk = execParsed.InvalidAsk
		result.InvalidExecNode = execParsed.InvalidNode
		result.Cleaned = execParsed.Cleaned
	}

	// Step 3: Parse queue options from the raw queue mode text.
	if result.HasQueueDirective {
		result.QueueMode = normalizeQueueMode(result.RawQueueMode)
		result.QueueReset = strings.EqualFold(result.RawQueueMode, "reset")

		// Parse additional queue options from cleaned text.
		remaining := result.Cleaned
		matches := queueOptionRe.FindAllStringSubmatch(remaining, -1)
		for _, m := range matches {
			key := strings.ToLower(m[1])
			val := m[2]
			switch key {
			case "debounce":
				result.RawDebounce = val
				if ms, err := strconv.Atoi(val); err == nil && ms >= 0 {
					result.DebounceMs = ms
					result.HasQueueOpts = true
				}
			case "cap":
				result.RawCap = val
				if c, err := strconv.Atoi(val); err == nil && c >= 0 {
					result.Cap = c
					result.HasQueueOpts = true
				}
			case "drop":
				result.RawDrop = val
				switch strings.ToLower(val) {
				case "oldest":
					result.DropPolicy = QueueDropOldest
					result.HasQueueOpts = true
				case "newest":
					result.DropPolicy = QueueDropNewest
					result.HasQueueOpts = true
				}
			}
		}
		// Strip matched queue options from cleaned text.
		if len(matches) > 0 {
			cleaned := queueOptionRe.ReplaceAllString(remaining, "")
			result.Cleaned = strings.TrimSpace(multiSpaceRe.ReplaceAllString(cleaned, " "))
		}
	}

	return result
}

func normalizeQueueMode(raw string) QueueMode {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "auto":
		return QueueModeAuto
	case "manual":
		return QueueModeManual
	case "off":
		return QueueModeOff
	}
	return ""
}

// IsFullDirectiveOnly returns true if the message contains only directives
// (no user text) after all directive extraction.
func IsFullDirectiveOnly(directives FullInlineDirectives, cleanedBody string, isGroup bool) bool {
	hasAny := directives.HasThinkDirective ||
		directives.HasVerboseDirective ||
		directives.HasFastDirective ||
		directives.HasReasoningDirective ||
		directives.HasElevatedDirective ||
		directives.HasExecDirective ||
		directives.HasModelDirective ||
		directives.HasQueueDirective
	if !hasAny {
		return false
	}

	stripped := StripStructuralPrefixes(cleanedBody)
	if isGroup {
		stripped = stripAllMentions(stripped)
	}
	return strings.TrimSpace(stripped) == ""
}

// stripAllMentions removes all @mention patterns from text.
func stripAllMentions(text string) string {
	return MentionPattern.ReplaceAllString(text, "")
}
