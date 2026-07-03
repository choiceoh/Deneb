// thinking.go — the single source for shaping RAW (non-chat-pipeline) LLM
// requests so their output budget goes to the ANSWER, not chain-of-thought.
//
// Three raw-call surfaces share this: the localai hub (mergeRequestBody), the
// pilot direct role path (CallRoleLLM), and the wiki dreamer's wiring
// (server/chat_pipeline.go dreamerLLMShape). Before consolidation each had its
// own partial policy, and the gap between them is exactly what broke the dream
// cycle on 2026-07-02/03: dual-mode deepseek-v4 keeps Profile.Reasoning=false
// BY DESIGN (profile.go — its thinking channel is toggled per request), so the
// hub's "non-reasoning → enable_thinking=false" rule matched it but sent the
// Qwen-family kwarg SPELLING, which dsv4 templates silently ignore. Thinking
// stayed on and consumed the whole output budget (finish_reason=length,
// empty content).
package modelrole

import "github.com/choiceoh/deneb/gateway-go/internal/ai/modelcaps"

// NoThinkingBody is the Qwen-family spelled thinking-off request kwargs for
// non-reasoning models (their templates either honor it or ignore it; Qwen3
// *-instruct variants ship with thinking already off). localai.NoThinking
// aliases this var so the two packages cannot drift.
var NoThinkingBody = map[string]any{
	"chat_template_kwargs": map[string]any{
		"enable_thinking": false,
	},
}

// ThinkingOffExtraBody returns the extra-body kwargs a raw LLM call should
// attach for the given provider/model, or nil when thinking cannot be
// disabled (the caller must budget MaxTokens for reasoning + answer instead).
//
// Decision order — the template toggle comes FIRST:
//
//  1. Dual-mode models with a per-request template off-switch (deepseek-v4 →
//     chat_template_kwargs.thinking=false, provider-gated to vLLM-backed
//     servings by modelcaps.ThinkingToggleKwarg). These deliberately report
//     Profile.Reasoning=false, so an IsReasoningModel check first would
//     mis-route them into the enable_thinking branch their template ignores.
//  2. Reasoning models with no off-switch (step3, qwen3 non-instruct, r1…):
//     nil — the channel is always on; attaching enable_thinking risks a 400
//     on thinking-only templates.
//  3. Non-reasoning models on vLLM-backed providers: NoThinkingBody,
//     byte-identical to the hub's historical behavior. Direct cloud
//     providers get nil instead — chat_template_kwargs is a vLLM serving
//     feature, and an unknown top-level field can 400 on strict
//     OpenAI-compat APIs (wormhole-fronted models still count as
//     vLLM-backed, preserving today's passthrough behavior).
func ThinkingOffExtraBody(providerID, model string) map[string]any {
	if kw := modelcaps.ThinkingToggleKwarg(providerID, model); kw != "" {
		return map[string]any{"chat_template_kwargs": map[string]any{kw: false}}
	}
	if IsReasoningModel(model) {
		return nil
	}
	if !modelcaps.ServesVllmBacked(providerID) {
		return nil
	}
	return NoThinkingBody
}

// ThinkingOffExtraBodyFor is the registry-aware variant of
// ThinkingOffExtraBody: the off-switch name comes from the resolved routing
// profile (builtin capability + deneb.json routing.toggleKwarg override), so
// an operator-declared dual-mode model shapes raw calls the same way the
// chat effort router shapes foreground turns. Falls back to the package
// heuristics when the profile names no toggle. Nil-receiver safe.
func (r *Registry) ThinkingOffExtraBodyFor(providerID, model string) map[string]any {
	if r != nil {
		if kw := r.RoutingProfileForModel(providerID, model).ToggleKwarg; kw != "" {
			return map[string]any{"chat_template_kwargs": map[string]any{kw: false}}
		}
	}
	return ThinkingOffExtraBody(providerID, model)
}
