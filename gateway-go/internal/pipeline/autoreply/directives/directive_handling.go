// directive_handling.go — Full directive handling implementation.
// Mirrors src/auto-reply/reply/directive-handling.impl.ts (517 LOC),
// directive-handling.model.ts (506 LOC),
// directive-handling.persist.ts (225 LOC), directive-handling.levels.ts (46 LOC),
// directive-handling.fast-lane.ts (99 LOC), directive-handling.shared.ts (80 LOC),
// directive-handling.params.ts (56 LOC), directive-handling.model-picker.ts (28 LOC),
// directive-handling.queue-validation.ts (78 LOC).
package directives

import (
	"fmt"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/autoreply/model"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/autoreply/session"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/autoreply/types"
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

// HandleDirectives processes all inline directives in a message body.
// This is the main orchestrator matching directive-handling.impl.ts.
func HandleDirectives(body string, sess *types.SessionState, opts DirectiveHandlingOptions) DirectiveHandlingResult {
	result := DirectiveHandlingResult{}

	// 1. Parse inline directives.
	parseOpts := &DirectiveParseOptions{
		ModelAliases:  opts.ModelAliases,
		DisableStatus: opts.DisableStatus,
	}
	directives := ParseInlineDirectives(body, parseOpts)
	result.CleanedBody = directives.Cleaned
	result.IsDirectiveOnly = IsDirectiveOnly(directives)

	// 2. Validate and resolve each directive.
	mod := &types.SessionModification{}
	hasAnyChange := false

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

	// Status directive (handled as immediate reply).
	if directives.HasStatusDirective && opts.StatusHandler != nil {
		statusReply := opts.StatusHandler(sess)
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
	ModelAliases    []string
	ModelCandidates []model.ModelCandidate
	DisableStatus   bool
	StatusHandler   func(session *types.SessionState) string
}

// PersistDirectives applies directive results to the session store.
// Mirrors directive-handling.persist.ts persistInlineDirectives().
// Delegates to session.ApplySessionUpdate for fields that overlap with
// the structured SessionUpdate type.
func PersistDirectives(sess *types.SessionState, result DirectiveHandlingResult) {
	if result.SessionMod == nil {
		return
	}
	mod := result.SessionMod

	// Build a SessionUpdate from the directive modification and apply it.
	// This ensures timestamp tracking and field-level consistency via the
	// session sub-package.
	update := session.SessionUpdate{}
	if mod.Model != "" {
		update.Model = &mod.Model
	}
	if mod.Provider != "" {
		update.Provider = &mod.Provider
	}
	if mod.FastMode != nil {
		update.FastMode = mod.FastMode
	}
	if mod.VerboseLevel != "" {
		update.VerboseLevel = &mod.VerboseLevel
	}
	if mod.ReasoningLevel != "" {
		update.ReasoningLevel = &mod.ReasoningLevel
	}
	session.ApplySessionUpdate(sess, update)
}

// ResolveCurrentDirectiveLevels resolves effective levels from session + config.
// Mirrors directive-handling.levels.ts resolveCurrentDirectiveLevels().
type ResolvedLevels struct {
	VerboseLevel   types.VerboseLevel
	FastMode       bool
	ReasoningLevel types.ReasoningLevel
}

func ResolveCurrentDirectiveLevels(sess *types.SessionState, defaults ResolvedLevels) ResolvedLevels {
	result := defaults

	if sess == nil {
		return result
	}
	if sess.VerboseLevel != "" {
		result.VerboseLevel = sess.VerboseLevel
	}
	result.FastMode = sess.FastMode
	if sess.ReasoningLevel != "" {
		result.ReasoningLevel = sess.ReasoningLevel
	}
	return result
}

// --- Model directive resolution ---

func resolveModelDirective(rawModel, rawProfile string, opts DirectiveHandlingOptions) DirectiveModelResolution {
	if rawModel == "" {
		return DirectiveModelResolution{IsValid: false, ErrorText: "No model specified."}
	}

	// Parse provider/model from the raw string.
	parts := model.SplitProviderModel(rawModel)
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

// --- Helpers ---

func formatDirectiveAck(name, value string) string {
	return fmt.Sprintf("%s → %s. ", name, value)
}

func boolToOnOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}
