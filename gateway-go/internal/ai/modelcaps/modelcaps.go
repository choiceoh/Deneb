// Package modelcaps centralizes per-provider/per-model capability knowledge:
// context window size, reasoning-endpoint behavior, vision support, and
// prompt-cache (cache_control) compatibility.
//
// Before this package the knowledge lived as scattered heuristics —
// isOpenAIReasoningModel in internal/ai/llm and isCacheIncompatibleProvider in
// internal/pipeline/chat — so adding a model meant hunting call sites. This is
// a dependency-free leaf package (stdlib only) so both llm and modelrole can
// import it without cycles.
//
// Resolution layering (lowest to highest precedence):
//
//  1. Builtin() — the heuristics in this file.
//  2. vLLM /models discovery (max_model_len) — overlaid by modelrole.Registry.
//  3. deneb.json models.providers.<id> capability overrides (contextWindow,
//     reasoning, vision, promptCache) — overlaid by modelrole.Registry.
//
// Zero values always mean "unknown — keep current behavior": ContextWindow 0
// performs no budget clamping, NoVision false sends image blocks as-is.
package modelcaps

import "strings"

// Capability describes what a provider/model combination supports.
// The zero value is fully permissive (unknown model, no special handling).
type Capability struct {
	// ContextWindow is the model's context length in tokens. 0 = unknown
	// (callers must not clamp budgets against an unknown window).
	ContextWindow int

	// Reasoning marks genuine OpenAI reasoning-endpoint models (o-series,
	// gpt-5) that require max_completion_tokens instead of max_tokens.
	Reasoning bool

	// NoVision is set when the model is known to reject image content
	// blocks. The chat pipeline strips image blocks to text stubs before
	// sending. False (default) means "assume vision works" — wrong stubs
	// on a vision-capable model are worse than a provider-side error.
	NoVision bool

	// RejectsCacheControl marks providers that speak the Anthropic wire but
	// fault with HTTP 400 when cache_control markers are present (instead
	// of merely ignoring them). Kimi Code is the known case.
	RejectsCacheControl bool
}

// Builtin returns the built-in capability defaults for a provider/model pair.
// Both heuristics below feed it; config overrides are layered on top by
// modelrole.Registry.CapabilityForModel.
func Builtin(providerID, model string) Capability {
	return Capability{
		Reasoning:           IsOpenAIReasoningModel(model),
		RejectsCacheControl: RejectsCacheControl(providerID),
	}
}

// IsOpenAIReasoningModel reports whether model is a genuine OpenAI reasoning
// model (o1/o3/o4 series, gpt-5) that requires max_completion_tokens instead
// of max_tokens. OpenAI-compatible servers such as self-hosted vLLM keep
// max_tokens and would reject max_tokens=0, so the reasoning remap must not
// apply to them. A provider prefix ("openai/o3-mini") is ignored.
func IsOpenAIReasoningModel(model string) bool {
	m := strings.ToLower(strings.TrimSpace(model))
	if i := strings.LastIndex(m, "/"); i >= 0 {
		m = m[i+1:]
	}
	return strings.HasPrefix(m, "o1") ||
		strings.HasPrefix(m, "o3") ||
		strings.HasPrefix(m, "o4") ||
		strings.HasPrefix(m, "gpt-5")
}

// RejectsCacheControl reports whether a provider speaks the Anthropic
// Messages wire but REJECTS cache_control markers with an HTTP 400 (not
// merely ignores them). Kimi's coding endpoint is the known case: it is
// routed through the Anthropic client yet faults when any cache_control
// field is present. MiMo/z.ai are NOT included — they accept the markers.
//
// Matches the bare "kimi" id and any Kimi alias/remap carrying it as a
// prefix: catalog aliases like "kimi-code"/"kimi-coding" and the
// "<provider>-subagent" remap applied to spawned sessions ("kimi-subagent").
func RejectsCacheControl(providerID string) bool {
	id := strings.ToLower(strings.TrimSpace(providerID))
	return id == "kimi" || strings.HasPrefix(id, "kimi-")
}
