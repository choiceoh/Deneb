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

// ThinkingOffDirective describes the chat-template switch an adapter must set
// to false. It deliberately does not expose a raw request-body map: modelrole
// owns routing policy, while each raw LLM transport adapter owns its wire
// representation.
type ThinkingOffDirective struct {
	templateKwarg string
}

// TemplateKwarg returns the provider template's thinking toggle name.
func (d ThinkingOffDirective) TemplateKwarg() string {
	return d.templateKwarg
}

// ThinkingOffDirectiveFor returns the typed policy directive a raw LLM adapter
// should attach for the given provider/model, or nil when thinking cannot be
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
//  3. Non-reasoning models on vLLM-backed providers: enable_thinking=false,
//     byte-identical on the wire to the hub's historical behavior. Direct
//     cloud providers get nil instead — chat_template_kwargs is a vLLM
//     serving feature, and an unknown top-level field can 400 on strict
//     OpenAI-compat APIs (wormhole-fronted models still count as vLLM-backed,
//     preserving today's passthrough behavior).
func ThinkingOffDirectiveFor(providerID, model string) *ThinkingOffDirective {
	if kw := modelcaps.ThinkingToggleKwarg(providerID, model); kw != "" {
		return &ThinkingOffDirective{templateKwarg: kw}
	}
	if IsReasoningModel(model) {
		return nil
	}
	if !modelcaps.ServesVllmBacked(providerID) {
		return nil
	}
	return &ThinkingOffDirective{templateKwarg: "enable_thinking"}
}

// ThinkingOffDirectiveFor is the registry-aware variant of the package-level
// function: the off-switch name comes from the resolved routing profile
// (builtin capability + deneb.json routing.toggleKwarg override), so an
// operator-declared dual-mode model shapes raw calls the same way the chat
// effort router shapes foreground turns. It falls back to package heuristics
// when the profile names no toggle. Nil-receiver safe.
func (r *Registry) ThinkingOffDirectiveFor(providerID, model string) *ThinkingOffDirective {
	if r != nil {
		if kw := r.RoutingProfileForModel(providerID, model).ToggleKwarg; kw != "" {
			return &ThinkingOffDirective{templateKwarg: kw}
		}
	}
	return ThinkingOffDirectiveFor(providerID, model)
}

// roleForcesThinkingOff reports whether a role is defined by speed and
// concurrency over answer quality, so raw calls made for it must never spend
// latency or output budget on chain-of-thought. RoleTiny is that role — trivial
// classification/extraction (session titles, stage-1 extractors, the live
// "생각 중" chip summary): thinking there is pure overhead, and the role runs at
// high concurrency where the wasted tokens/latency compound.
func roleForcesThinkingOff(role Role) bool {
	return role == RoleTiny
}

// ThinkingOffDirectiveForRole is the ROLE-aware directive resolver for raw role
// calls. It first takes the per-model policy; when that would leave thinking on
// (nil — a reasoning model whose individual off-switch isn't recognized) AND the
// role forces thinking off, it forces the standard vLLM enable_thinking toggle
// anyway. This makes "thinking off" a property of the tiny ROLE rather than of
// each model it happens to point at: a dual-mode model swapped into the role
// later stays fast with no code change. The force is gated to vLLM-backed
// providers (chat_template_kwargs is a vLLM serving feature; off them, or for a
// truly thinking-only template, the caller must budget MaxTokens for thinking
// instead — but such models are never assigned to the tiny role). Nil-receiver
// safe.
func (r *Registry) ThinkingOffDirectiveForRole(role Role, providerID, model string) *ThinkingOffDirective {
	if d := r.ThinkingOffDirectiveFor(providerID, model); d != nil {
		return d
	}
	if roleForcesThinkingOff(role) && modelcaps.ServesVllmBacked(providerID) {
		return &ThinkingOffDirective{templateKwarg: "enable_thinking"}
	}
	return nil
}
