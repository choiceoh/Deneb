// directive_handling_parse.go — Full directive parser with exec and queue options.
// Mirrors src/auto-reply/reply/directive-handling.parse.ts (229 LOC).
//
// Chains extraction in the same order as TS:
// think → verbose → fast → reasoning → elevated → exec → status → model → queue
//
// The basic ParseInlineDirectives already handles think through queue (basic).
// This layer adds:
// - Full /exec directive parsing with key=value args
// - Full /queue directive parsing with debounce/cap/drop options
// - isDirectiveOnly with structural prefix + mention stripping
package autoreply

import (
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

	// Queue options (full, from QueueDirective).
	QueueModeResolved FollowupQueueMode
	DebounceMs        int
	Cap               int
	DropPolicy        FollowupDropPolicy
	RawDebounce       string
	RawCap            string
	RawDrop           string
	HasQueueOpts      bool
}

// FullDirectiveParseOptions configures the full directive parser.
type FullDirectiveParseOptions struct {
	ModelAliases         []string
	DisableElevated      bool
	AllowStatusDirective bool // defaults to true if unset
}

// ParseFullInlineDirectives parses all inline directives from a message body,
// including /exec and full /queue options. This matches the full TS pipeline
// from directive-handling.parse.ts.
//
// The chain order mirrors TS: think → verbose → fast → reasoning → elevated
// → exec → status → model → queue. The basic ParseInlineDirectives handles
// think through model+queue(basic). This function then re-extracts /exec with
// full key=value args and /queue with debounce/cap/drop options.
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

	// Step 3: Re-extract /queue from the original body with full options.
	// The basic parser only captured HasQueueDirective + RawQueueMode.
	// Now extract debounce/cap/drop args using the token-based parser
	// which returns QueueDirective with FollowupQueueMode/FollowupDropPolicy.
	if result.HasQueueDirective {
		qd := ExtractQueueDirective(body)
		if qd.HasDirective {
			result.QueueModeResolved = qd.QueueMode
			result.QueueReset = qd.QueueReset
			result.DebounceMs = qd.DebounceMs
			result.Cap = qd.Cap
			result.DropPolicy = qd.DropPolicy
			result.RawDebounce = qd.RawDebounce
			result.RawCap = qd.RawCap
			result.RawDrop = qd.RawDrop
			result.HasQueueOpts = qd.HasOptions
		}
	}

	return result
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
