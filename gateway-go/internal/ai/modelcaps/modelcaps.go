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

	// ThinkingToggleKwarg names the vLLM chat_template_kwargs boolean that
	// disables the model's thinking phase per request ("" = no template
	// toggle). DeepSeek V4 templates use "thinking"; Qwen3-family templates
	// use "enable_thinking" (cf. localai.NoThinking — the kwarg SPELLING is
	// per-family, and the wrong one fails at template render time). Only
	// self-hosted vLLM serving honors chat_template_kwargs, so the builtin
	// is gated to vllm provider ids; other providers silently drop or 400
	// on unknown fields, which is why this must stay provider-aware.
	ThinkingToggleKwarg string
}

// Builtin returns the built-in capability defaults for a provider/model pair.
// Both heuristics below feed it; config overrides are layered on top by
// modelrole.Registry.CapabilityForModel.
func Builtin(providerID, model string) Capability {
	return Capability{
		Reasoning:           IsOpenAIReasoningModel(model),
		RejectsCacheControl: RejectsCacheControl(providerID),
		ThinkingToggleKwarg: ThinkingToggleKwarg(providerID, model),
	}
}

// ThinkingToggleKwarg reports the chat_template_kwargs boolean that disables
// the thinking phase for a provider/model pair, or "" when the pair has no
// per-request template toggle. Gated to vLLM-backed providers because
// chat_template_kwargs is a vLLM serving feature: the same model name via
// OpenRouter or an Anthropic-wire relay must NOT receive the field. The
// model-name gate below still discriminates, so a cloud model fronted by a
// vLLM-backed proxy never gets the toggle.
func ThinkingToggleKwarg(providerID, model string) string {
	if !ServesVllmBacked(providerID) {
		return ""
	}
	m := strings.ToLower(model)
	if strings.Contains(m, "deepseek-v4") || strings.Contains(m, "deepseek_v4") {
		return "thinking"
	}
	return ""
}

// ServesVllmBacked reports whether a provider id forwards requests to a vLLM
// serving: a direct vllm provider, or wormhole — Deneb's first-party
// byte-transparent model router (cmd/wormhole), which passes the request body
// straight through to a vLLM backend for a local model. The two capability
// layers that key off the underlying serving treat both alike:
//
//   - thinking toggle: a vLLM backend honors chat_template_kwargs (the model-name
//     gate in ThinkingToggleKwarg still excludes cloud models fronted by wormhole,
//     so the field never leaks to a non-vLLM upstream);
//   - context window: a vLLM backend reports a real max_model_len, applied by
//     served model id in modelrole.CapabilityForModel.
//
// Without recognizing the proxy here, routing the main model through wormhole
// (agents.defaultModel = "wormhole/deepseek-v4-…") silently strips both — the
// effort router goes inert (thinking always on) and the context window resolves
// to 0 (deferred compaction disabled).
func ServesVllmBacked(providerID string) bool {
	p := strings.ToLower(strings.TrimSpace(providerID))
	switch {
	case p == "vllm", strings.HasPrefix(p, "vllm-"), strings.HasPrefix(p, "vllm_"):
		return true
	case p == "wormhole", strings.HasPrefix(p, "wormhole-"), strings.HasPrefix(p, "wormhole_"):
		return true
	default:
		return false
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
