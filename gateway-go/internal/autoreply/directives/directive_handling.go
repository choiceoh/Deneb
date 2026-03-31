// directive_handling.go — Full directive handling implementation.
// Mirrors src/auto-reply/reply/directive-handling.impl.ts (517 LOC),
// directive-handling.model.ts (506 LOC), directive-handling.auth.ts (246 LOC),
// directive-handling.persist.ts (225 LOC), directive-handling.levels.ts (46 LOC),
// directive-handling.fast-lane.ts (99 LOC), directive-handling.shared.ts (80 LOC),
// directive-handling.params.ts (56 LOC), directive-handling.model-picker.ts (28 LOC),
// directive-handling.queue-validation.ts (78 LOC).
package directives

import (
	"fmt"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/model"
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/pipeline"
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/queue"
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/session"
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/types"
)

// DirectiveHandlingResult holds the full outcome of processing inline directives.
type DirectiveHandlingResult struct {
	// Cleaned message body after directive removal.
	CleanedBody string
	// Whether the message is directive-only (no user text).
	IsDirectiveOnly bool
	// Session modifications to apply.
	SessionMod *types.SessionModification
	// Immediate reply (e.g., for /status in directive form).
	ImmediateReply *types.ReplyPayload
	// Acknowledgment text for mode changes.
	AckText string
	// Errors from invalid directives.
	Errors []string
	// Model resolution result.
	ModelResolution *DirectiveModelResolution
	// Queue changes.
	QueueChanges *DirectiveQueueChanges
}

// DirectiveModelResolution holds the result of model directive resolution.
type DirectiveModelResolution struct {
	Provider    string
	Model       string
	AuthProfile string
	IsValid     bool
	ErrorText   string
	// Fuzzy match info.
	FuzzyMatched   bool
	FuzzyCandidate string
	FuzzyScore     int
}

// DirectiveQueueChanges describes queue modifications from directives.
type DirectiveQueueChanges struct {
	Mode       queue.QueueMode
	Reset      bool
	DebounceMs int
	Cap        int
	DropPolicy queue.QueueDropPolicy
}

// HandleDirectives processes all inline directives in a message body.
// This is the main orchestrator matching directive-handling.impl.ts.
func HandleDirectives(body string, session *types.SessionState, opts DirectiveHandlingOptions) DirectiveHandlingResult {
	result := DirectiveHandlingResult{}

	// 1. Parse inline directives.
	parseOpts := &DirectiveParseOptions{
		ModelAliases:    opts.ModelAliases,
		DisableElevated: opts.DisableElevated,
		DisableStatus:   opts.DisableStatus,
	}
	directives := ParseInlineDirectives(body, parseOpts)
	result.CleanedBody = directives.Cleaned
	result.IsDirectiveOnly = IsDirectiveOnly(directives)

	// 2. Validate and resolve each directive.
	mod := &types.SessionModification{}
	hasAnyChange := false

	// Think level.
	if directives.HasThinkDirective {
		hasAnyChange = true
		mod.ThinkLevel = directives.ThinkLevel
		if directives.ThinkLevel != "" {
			result.AckText += formatDirectiveAck("Thinking", string(directives.ThinkLevel))
		}
	}

	// Verbose level.
	if directives.HasVerboseDirective {
		hasAnyChange = true
		mod.VerboseLevel = directives.VerboseLevel
		result.AckText += formatDirectiveAck("Verbose", string(directives.VerboseLevel))
	}

	// Fast mode.
	if directives.HasFastDirective {
		hasAnyChange = true
		mod.FastMode = &directives.FastMode
		result.AckText += formatDirectiveAck("Fast", boolToOnOff(directives.FastMode))
	}

	// Reasoning level.
	if directives.HasReasoningDirective {
		hasAnyChange = true
		mod.ReasoningLevel = directives.ReasoningLevel
		result.AckText += formatDirectiveAck("Reasoning", string(directives.ReasoningLevel))
	}

	// Elevated level.
	if directives.HasElevatedDirective {
		// Auth check: elevated changes may require authorization.
		if opts.RequireAuthForElevated && !opts.IsAuthorized {
			result.Errors = append(result.Errors, "⚠️ Elevated mode changes require authorization.")
		} else {
			hasAnyChange = true
			mod.ElevatedLevel = directives.ElevatedLevel
			result.AckText += formatDirectiveAck("Elevated", string(directives.ElevatedLevel))
		}
	}

	// Model directive.
	if directives.HasModelDirective {
		resolution := resolveModelDirective(directives.RawModelDirective, directives.RawModelProfile, opts)
		result.ModelResolution = &resolution
		if resolution.IsValid {
			hasAnyChange = true
			mod.Model = resolution.Model
			mod.Provider = resolution.Provider
			result.AckText += formatDirectiveAck("Model", model.FormatProviderModelRef(resolution.Provider, resolution.Model))
		} else if resolution.ErrorText != "" {
			result.Errors = append(result.Errors, resolution.ErrorText)
		}
	}

	// Queue directive.
	if directives.HasQueueDirective {
		queueChanges := resolveQueueDirective(directives)
		result.QueueChanges = &queueChanges
		if queueChanges.Reset {
			result.AckText += "Queue reset. "
		}
	}

	// Status directive (handled as immediate reply).
	if directives.HasStatusDirective && opts.StatusHandler != nil {
		statusReply := opts.StatusHandler(session)
		if statusReply != "" {
			result.ImmediateReply = &types.ReplyPayload{Text: statusReply}
		}
	}

	if hasAnyChange {
		result.SessionMod = mod
	}
	return result
}

// DirectiveHandlingOptions configures directive handling.
type DirectiveHandlingOptions struct {
	ModelAliases           []string
	ModelCandidates        []model.ModelCandidate
	DisableElevated        bool
	DisableStatus          bool
	RequireAuthForElevated bool
	IsAuthorized           bool
	StatusHandler          func(session *types.SessionState) string
}

// PersistDirectives applies directive results to the session store.
// Mirrors directive-handling.persist.ts persistInlineDirectives().
func PersistDirectives(session *types.SessionState, result DirectiveHandlingResult) {
	if result.SessionMod == nil {
		return
	}
	mod := result.SessionMod
	if mod.ThinkLevel != "" {
		session.ThinkLevel = mod.ThinkLevel
	}
	if mod.VerboseLevel != "" {
		session.VerboseLevel = mod.VerboseLevel
	}
	if mod.FastMode != nil {
		session.FastMode = *mod.FastMode
	}
	if mod.ReasoningLevel != "" {
		session.ReasoningLevel = mod.ReasoningLevel
	}
	if mod.ElevatedLevel != "" {
		session.ElevatedLevel = mod.ElevatedLevel
	}
	if mod.Model != "" {
		session.Model = mod.Model
	}
	if mod.Provider != "" {
		session.Provider = mod.Provider
	}
	if mod.SendPolicy != "" {
		session.SendPolicy = mod.SendPolicy
	}
	if mod.GroupActivation != "" {
		session.GroupActivation = mod.GroupActivation
	}
}

// ResolveCurrentDirectiveLevels resolves effective levels from session + config.
// Mirrors directive-handling.levels.ts resolveCurrentDirectiveLevels().
type ResolvedLevels struct {
	ThinkLevel     types.ThinkLevel
	VerboseLevel   types.VerboseLevel
	FastMode       bool
	ReasoningLevel types.ReasoningLevel
	ElevatedLevel  types.ElevatedLevel
}

func ResolveCurrentDirectiveLevels(session *types.SessionState, defaults ResolvedLevels) ResolvedLevels {
	result := defaults

	if session == nil {
		return result
	}
	if session.ThinkLevel != "" {
		result.ThinkLevel = session.ThinkLevel
	}
	if session.VerboseLevel != "" {
		result.VerboseLevel = session.VerboseLevel
	}
	result.FastMode = session.FastMode
	if session.ReasoningLevel != "" {
		result.ReasoningLevel = session.ReasoningLevel
	}
	if session.ElevatedLevel != "" {
		result.ElevatedLevel = session.ElevatedLevel
	}
	return result
}

// --- Model directive resolution ---

func resolveModelDirective(rawModel, rawProfile string, opts DirectiveHandlingOptions) DirectiveModelResolution {
	if rawModel == "" {
		return DirectiveModelResolution{IsValid: false, ErrorText: "No model specified."}
	}

	// Parse provider/model from the raw string.
	parts := pipeline.SplitProviderModel(rawModel)
	provider := parts[0]
	mdl := parts[1]

	// Try exact match from candidates.
	if len(opts.ModelCandidates) > 0 {
		resolved := model.ResolveModelFromDirective(rawModel, opts.ModelCandidates)
		if resolved != nil {
			return DirectiveModelResolution{
				Provider:    resolved.Provider,
				Model:       resolved.Model,
				AuthProfile: rawProfile,
				IsValid:     true,
			}
		}

		// Try fuzzy match.
		var bestCandidate *model.ModelCandidate
		bestScore := 0
		for i := range opts.ModelCandidates {
			score := model.ScoreFuzzyMatch(strings.ToLower(rawModel), opts.ModelCandidates[i])
			if score > bestScore {
				bestScore = score
				bestCandidate = &opts.ModelCandidates[i]
			}
		}
		if bestCandidate != nil && bestScore >= 40 {
			return DirectiveModelResolution{
				Provider:       bestCandidate.Provider,
				Model:          bestCandidate.Model,
				AuthProfile:    rawProfile,
				IsValid:        true,
				FuzzyMatched:   true,
				FuzzyCandidate: model.FormatProviderModelRef(bestCandidate.Provider, bestCandidate.Model),
				FuzzyScore:     bestScore,
			}
		}
	}

	// No candidates or no match — accept raw value (may be validated later).
	return DirectiveModelResolution{
		Provider:    provider,
		Model:       mdl,
		AuthProfile: rawProfile,
		IsValid:     true,
	}
}

// --- Queue directive resolution ---

func resolveQueueDirective(directives InlineDirectives) DirectiveQueueChanges {
	changes := DirectiveQueueChanges{}
	if directives.QueueReset {
		changes.Reset = true
	}
	if directives.RawQueueMode != "" {
		mode := strings.ToLower(directives.RawQueueMode)
		switch mode {
		case "auto":
			changes.Mode = queue.QueueModeAuto
		case "manual":
			changes.Mode = queue.QueueModeManual
		case "off":
			changes.Mode = queue.QueueModeOff
		}
	}
	return changes
}

// --- Fast lane ---

// IsFastLaneDirective returns true if the directive can be handled without
// running the full agent pipeline (pure mode toggle).
func IsFastLaneDirective(directives InlineDirectives) bool {
	if !IsDirectiveOnly(directives) {
		return false
	}
	// Fast lane: only mode toggles, no model changes or queue.
	return !directives.HasModelDirective && !directives.HasQueueDirective && !directives.HasStatusDirective
}

// BuildFastLaneReply creates a quick acknowledgment reply for fast-lane directives.
func BuildFastLaneReply(directives InlineDirectives) *types.ReplyPayload {
	var parts []string
	if directives.HasThinkDirective {
		parts = append(parts, fmt.Sprintf("🧠 Think: %s", directives.ThinkLevel))
	}
	if directives.HasFastDirective {
		parts = append(parts, fmt.Sprintf("⚡ Fast: %s", boolToOnOff(directives.FastMode)))
	}
	if directives.HasVerboseDirective {
		parts = append(parts, fmt.Sprintf("📝 Verbose: %s", directives.VerboseLevel))
	}
	if directives.HasReasoningDirective {
		parts = append(parts, fmt.Sprintf("💭 Reasoning: %s", directives.ReasoningLevel))
	}
	if directives.HasElevatedDirective {
		parts = append(parts, fmt.Sprintf("🔓 Elevated: %s", directives.ElevatedLevel))
	}
	if len(parts) == 0 {
		return nil
	}
	return &types.ReplyPayload{Text: strings.Join(parts, "\n")}
}

// --- Helpers ---

func formatDirectiveAck(name, value string) string {
	return fmt.Sprintf("%s → %s. ", name, value)
}

// DirectiveParams extracts @param-style parameters from directive text.
type DirectiveParams struct {
	Params map[string]string
}

// ParseDirectiveParams extracts @key=value pairs from text.
func ParseDirectiveParams(text string) DirectiveParams {
	params := DirectiveParams{Params: make(map[string]string)}
	words := strings.Fields(text)
	for _, word := range words {
		if !strings.HasPrefix(word, "@") {
			continue
		}
		kv := word[1:] // strip @
		if idx := strings.IndexByte(kv, '='); idx >= 0 {
			key := kv[:idx]
			value := kv[idx+1:]
			params.Params[key] = value
		} else {
			params.Params[kv] = "true"
		}
	}
	return params
}

// ValidateQueueDirective checks if queue directive values are valid.
func ValidateQueueDirective(changes DirectiveQueueChanges) []string {
	var errors []string
	if changes.DebounceMs < 0 {
		errors = append(errors, "Queue debounce must be non-negative.")
	}
	if changes.Cap < 0 {
		errors = append(errors, "Queue cap must be non-negative.")
	}
	return errors
}

// boolToOnOff converts a bool to "on" or "off".
func boolToOnOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

