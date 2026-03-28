// model_compat.go — re-exports from the autoreply/model subpackage.
// TODO: Remove after all callers are updated to import autoreply/model directly.
package autoreply

import "github.com/choiceoh/deneb/gateway-go/internal/autoreply/model"

// Type aliases — fallback.go
type FallbackAttempt = model.FallbackAttempt
type FallbackNoticeState = model.FallbackNoticeState
type FallbackTransition = model.FallbackTransition

// Type aliases — model_selection.go
type ModelSelection = model.ModelSelection
type ModelCandidate = model.ModelCandidate

// Type aliases — model_selection_full.go
type ModelSelectionState = model.ModelSelectionState
type ModelSelectionConfig = model.ModelSelectionConfig

// Type alias — model_runtime.go
type ModelRuntimeInfo = model.ModelRuntimeInfo

// Type alias — model_directive.go
type ModelDirective = model.ModelDirective

// Const re-exports — model_runtime.go
// (Go does not support const aliases; copy values here.)
const DefaultContextTokens = model.DefaultContextTokens
const DefaultMaxTokens = model.DefaultMaxTokens

// Var re-export — model_selection_full.go
// Note: this aliases the slice variable; mutations would affect the model package.
var VariantTokens = model.VariantTokens

// Function re-exports — fallback.go
var FormatProviderModelRef = model.FormatProviderModelRef
var FormatFallbackAttemptReason = model.FormatFallbackAttemptReason
var BuildFallbackReasonSummary = model.BuildFallbackReasonSummary
var BuildFallbackAttemptSummaries = model.BuildFallbackAttemptSummaries
var BuildFallbackNotice = model.BuildFallbackNotice
var BuildFallbackClearedNotice = model.BuildFallbackClearedNotice
var ResolveActiveFallbackState = model.ResolveActiveFallbackState
var ResolveFallbackTransition = model.ResolveFallbackTransition

// Function re-exports — model_selection.go
var ResolveModelFromDirective = model.ResolveModelFromDirective
var ResolveModelOverride = model.ResolveModelOverride
var ScoreFuzzyMatch = model.ScoreFuzzyMatch

// Function re-exports — model_selection_full.go
var ResolveModelSelection = model.ResolveModelSelection
var ResolveStoredModelOverride = model.ResolveStoredModelOverride
var IsVariantToken = model.IsVariantToken
var StripVariantTokens = model.StripVariantTokens

// Function re-exports — model_runtime.go
var ResolveContextTokens = model.ResolveContextTokens
var ResolveMaxTokens = model.ResolveMaxTokens
var EstimateTokens = model.EstimateTokens

// Function re-export — model_directive.go
var ExtractModelDirective = model.ExtractModelDirective
