package directives

import (
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/autoreply/model"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/autoreply/types"
	"regexp"
	"strings"
)

var multiSpaceRe = regexp.MustCompile(`\s+`)

// InlineDirectives holds all parsed inline directives from a message body.
type InlineDirectives struct {
	Cleaned string // message body with all directives removed

	HasThinkDirective bool
	ThinkLevel        types.ThinkLevel
	RawThinkLevel     string

	HasVerboseDirective bool
	VerboseLevel        types.VerboseLevel
	RawVerboseLevel     string

	HasFastDirective bool
	FastMode         bool
	RawFastMode      string

	HasReasoningDirective bool
	ReasoningLevel        types.ReasoningLevel
	RawReasoningLevel     string

	HasElevatedDirective bool
	ElevatedLevel        types.ElevatedLevel
	RawElevatedLevel     string

	HasStatusDirective bool

	HasModelDirective bool
	RawModelDirective string
	RawModelProfile   string

	HasQueueDirective bool
	QueueReset        bool
	RawQueueMode      string

	HasDeepWorkDirective bool
}

// Regex patterns for directive extraction.
var (
	thinkDirectiveRe     = regexp.MustCompile(`(?i)(?:^|\s)/think(?:\s+([a-zA-Z0-9_-]+))?\s*`)
	verboseDirectiveRe   = regexp.MustCompile(`(?i)(?:^|\s)/verbose(?:\s+([a-zA-Z0-9_-]+))?\s*`)
	fastDirectiveRe      = regexp.MustCompile(`(?i)(?:^|\s)/fast(?:\s+([a-zA-Z0-9_-]+))?\s*`)
	reasoningDirectiveRe = regexp.MustCompile(`(?i)(?:^|\s)/reasoning(?:\s+([a-zA-Z0-9_-]+))?\s*`)
	elevatedDirectiveRe  = regexp.MustCompile(`(?i)(?:^|\s)/elevated(?:\s+([a-zA-Z0-9_-]+))?\s*`)
	statusDirectiveRe    = regexp.MustCompile(`(?i)(?:^|\s)/status\s*$`)
	queueDirectiveRe     = regexp.MustCompile(`(?i)(?:^|\s)/queue(?:\s+([a-zA-Z0-9_-]+))?\s*`)
	deepworkDirectiveRe  = regexp.MustCompile(`(?i)(?:^|\s)/deepwork\s*`)
)

// ParseInlineDirectives extracts all inline directives from a message body.
// This is the Go equivalent of parseInlineDirectives() from the TS codebase.
func ParseInlineDirectives(body string, opts *DirectiveParseOptions) InlineDirectives {
	if opts == nil {
		opts = &DirectiveParseOptions{}
	}
	result := InlineDirectives{}
	text := body

	// Extract /think directive.
	text, result.HasThinkDirective, result.ThinkLevel, result.RawThinkLevel = extractLevelDirective(
		text, thinkDirectiveRe, func(raw string) (types.ThinkLevel, bool) { return types.NormalizeThinkLevel(raw) },
		types.ThinkLow, // default when no arg: /think → low
	)

	// Extract /verbose directive.
	text, result.HasVerboseDirective, result.VerboseLevel, result.RawVerboseLevel = extractLevelDirective(
		text, verboseDirectiveRe, func(raw string) (types.VerboseLevel, bool) { return types.NormalizeVerboseLevel(raw) },
		types.VerboseOn, // default: /verbose → on
	)

	// Extract /fast directive.
	var fastVal bool
	text, result.HasFastDirective, fastVal, result.RawFastMode = extractBoolDirective(
		text, fastDirectiveRe, true,
	)
	result.FastMode = fastVal

	// Extract /reasoning directive.
	text, result.HasReasoningDirective, result.ReasoningLevel, result.RawReasoningLevel = extractLevelDirective(
		text, reasoningDirectiveRe, func(raw string) (types.ReasoningLevel, bool) { return types.NormalizeReasoningLevel(raw) },
		types.ReasoningOn,
	)

	// Extract /elevated directive (unless disabled).
	if !opts.DisableElevated {
		text, result.HasElevatedDirective, result.ElevatedLevel, result.RawElevatedLevel = extractLevelDirective(
			text, elevatedDirectiveRe, func(raw string) (types.ElevatedLevel, bool) { return types.NormalizeElevatedLevel(raw) },
			types.ElevatedOn,
		)
	}

	// Extract /status directive.
	if !opts.DisableStatus {
		if statusDirectiveRe.MatchString(text) {
			result.HasStatusDirective = true
			text = statusDirectiveRe.ReplaceAllString(text, " ")
		}
	}

	// Extract /model directive.
	modelResult := model.ExtractModelDirective(text, opts.ModelAliases)
	if modelResult.HasDirective {
		result.HasModelDirective = true
		result.RawModelDirective = modelResult.RawModel
		result.RawModelProfile = modelResult.RawProfile
		text = modelResult.Cleaned
	}

	// Extract /deepwork directive.
	if deepworkDirectiveRe.MatchString(text) {
		result.HasDeepWorkDirective = true
		text = deepworkDirectiveRe.ReplaceAllString(text, " ")
	}

	// Extract /queue directive.
	if m := queueDirectiveRe.FindStringSubmatchIndex(text); m != nil {
		result.HasQueueDirective = true
		if m[2] >= 0 {
			result.RawQueueMode = text[m[2]:m[3]]
		}
		if strings.ToLower(result.RawQueueMode) == "reset" {
			result.QueueReset = true
		}
		text = text[:m[0]] + " " + text[m[1]:]
	}

	result.Cleaned = cleanDirectiveOutput(text)
	return result
}

// DirectiveParseOptions configures directive parsing.
type DirectiveParseOptions struct {
	ModelAliases    []string
	DisableElevated bool
	DisableStatus   bool
}

// IsDirectiveOnly returns true if the message contains only directives (no user text).
func IsDirectiveOnly(directives InlineDirectives) bool {
	if !directives.HasThinkDirective &&
		!directives.HasVerboseDirective &&
		!directives.HasFastDirective &&
		!directives.HasReasoningDirective &&
		!directives.HasElevatedDirective &&
		!directives.HasModelDirective &&
		!directives.HasQueueDirective &&
		!directives.HasDeepWorkDirective {
		return false
	}
	return strings.TrimSpace(directives.Cleaned) == ""
}

// extractLevelDirective extracts a directive with an optional level argument.
func extractLevelDirective[T ~string](
	text string,
	re *regexp.Regexp,
	normalize func(string) (T, bool),
	defaultLevel T,
) (cleaned string, hasDirective bool, level T, rawLevel string) {
	m := re.FindStringSubmatchIndex(text)
	if m == nil {
		return text, false, level, ""
	}
	hasDirective = true
	if m[2] >= 0 {
		rawLevel = text[m[2]:m[3]]
		if resolved, ok := normalize(rawLevel); ok {
			level = resolved
		} else {
			level = defaultLevel
		}
	} else {
		level = defaultLevel
	}
	cleaned = text[:m[0]] + " " + text[m[1]:]
	return
}

// extractBoolDirective extracts a directive with an optional boolean argument.
func extractBoolDirective(
	text string,
	re *regexp.Regexp,
	defaultVal bool,
) (cleaned string, hasDirective bool, value bool, rawValue string) {
	m := re.FindStringSubmatchIndex(text)
	if m == nil {
		return text, false, false, ""
	}
	hasDirective = true
	value = defaultVal
	if m[2] >= 0 {
		rawValue = text[m[2]:m[3]]
		if v, ok := types.NormalizeFastMode(rawValue); ok {
			value = v
		}
	}
	cleaned = text[:m[0]] + " " + text[m[1]:]
	return
}

func cleanDirectiveOutput(text string) string {
	return strings.TrimSpace(multiSpaceRe.ReplaceAllString(text, " "))
}
