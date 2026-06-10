package modelrole

import (
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/modelcaps"
)

// vllmRefreshMinInterval rate-limits on-demand vLLM re-discovery so a flapping
// fallback path cannot hammer the local server (or burn 3s probe timeouts on
// every attempt while it is down).
const vllmRefreshMinInterval = time.Minute

// CapabilityForModel resolves the effective capability for a provider/model
// pair by layering, lowest to highest precedence:
//
//  1. modelcaps builtin defaults (reasoning prefixes, Kimi cache rejection)
//  2. vLLM /models discovery (max_model_len → ContextWindow, provider "vllm")
//  3. deneb.json provider catalog overrides (contextWindow/reasoning/vision/
//     promptCache on the models.providers.<id> entry)
//
// Zero/absent values at every layer mean "unknown — keep current behavior",
// so an unconfigured deployment resolves to the permissive zero Capability.
func (r *Registry) CapabilityForModel(providerID, model string) modelcaps.Capability {
	caps := modelcaps.Builtin(providerID, model)

	r.mu.RLock()
	if providerID == "vllm" {
		if w, ok := r.vllmWindows[model]; ok && w > 0 {
			caps.ContextWindow = w
		}
	}
	p, hasProvider := r.providers[providerID]
	r.mu.RUnlock()

	if hasProvider {
		if p.ContextWindow > 0 {
			caps.ContextWindow = p.ContextWindow
		}
		if p.Reasoning != nil {
			caps.Reasoning = *p.Reasoning
		}
		if p.Vision != nil {
			caps.NoVision = !*p.Vision
		}
		if p.PromptCache != nil {
			caps.RejectsCacheControl = !*p.PromptCache
		}
	}
	return caps
}

// RefreshVllmRole re-runs served-model discovery for a vLLM-backed role and
// returns the (possibly updated) config. Startup discovery is otherwise the
// only reconciliation point, so a model swapped on the local server mid-run
// used to require a gateway restart; the chat fallback path calls this before
// trying a role so it targets what the server is serving NOW.
//
// No-op (returns the current config) for non-vllm roles and when the last
// probe for this role is fresher than vllmRefreshMinInterval. The network
// probe runs outside the registry lock.
func (r *Registry) RefreshVllmRole(role Role) ModelConfig {
	r.mu.RLock()
	cfg, ok := r.models[role]
	last := r.vllmProbedAt[role]
	r.mu.RUnlock()
	if !ok || cfg.ProviderID != "vllm" || time.Since(last) < vllmRefreshMinInterval {
		return cfg
	}

	probed := cfg
	served := reconcileVllmModel(r.logger, &probed) // network probe — lock NOT held

	r.mu.Lock()
	defer r.mu.Unlock()
	r.vllmProbedAt[role] = time.Now()
	if served == nil {
		return r.models[role] // probe failed; keep whatever is configured
	}
	for _, info := range served {
		if info.MaxModelLen > 0 {
			r.vllmWindows[info.ID] = info.MaxModelLen
		}
	}
	// Re-check against the current config: another goroutine may have changed
	// the role while we probed. Only apply the discovered name when the role
	// still points at the same vLLM endpoint we probed.
	current := r.models[role]
	if current.ProviderID == "vllm" && current.BaseURL == cfg.BaseURL && current.Model != probed.Model {
		current.Model = probed.Model
		r.models[role] = current
		r.logger.Info("modelrole: vllm role model refreshed",
			"role", role, "model", current.Model)
	}
	return r.models[role]
}
