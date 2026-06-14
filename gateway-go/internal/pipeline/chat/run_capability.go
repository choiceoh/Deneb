package chat

import (
	"log/slog"

	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/modelcaps"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/modelrole"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/router"
)

// Context-budget clamping constants.
const (
	// defaultOutputReserve is the output-token reserve subtracted from a
	// known context window when no explicit max-tokens cap is configured.
	defaultOutputReserve = 8192
	// minClampedContextBudget is the floor for a window-clamped budget so a
	// tiny (or misconfigured) window can never collapse the history budget
	// to something the protected head/tail zones cannot fit into.
	minClampedContextBudget = 4096
)

// modelCapability resolves the effective capability for the run's
// provider/model pair. The registry layers builtin defaults, vLLM discovery,
// and deneb.json provider overrides; without a registry (tests, minimal
// deployments) only the builtin defaults apply.
func modelCapability(deps runDeps, providerID, model string) modelcaps.Capability {
	if deps.registry != nil {
		return deps.registry.CapabilityForModel(providerID, model)
	}
	return modelcaps.Builtin(providerID, model)
}

// routingProfileForRun resolves the effort-routing profile for the run's
// provider/model pair. The registry layers builtin defaults (router.Default
// Profile + the model's capability toggle) with deneb.json routing overrides;
// without a registry (tests, minimal deployments) only the builtin default plus
// the model's toggle apply — so an unconfigured deployment still routes the
// current main model exactly as before.
func routingProfileForRun(deps runDeps, providerID, model string) router.Profile {
	if deps.registry != nil {
		return deps.registry.RoutingProfileForModel(providerID, model)
	}
	p := router.DefaultProfile()
	p.ToggleKwarg = modelcaps.Builtin(providerID, model).ThinkingToggleKwarg
	p.Enabled = p.ToggleKwarg != ""
	return p
}

// applyModelTuning fills model-derived defaults into the agent config after
// model resolution:
//
//   - Sampling: vendor-recommended (or deneb.json-overridden) temperature /
//     top_p from the model profile, applied only when the request did not
//     specify them — an explicit caller value always wins.
//   - MaxTokens: the background model tuner's floor for models that keep
//     hitting the output ceiling (raise-only, skipped when the caller set an
//     explicit max). Request-level parameters only, so per-model variation
//     never touches the prompt cache.
//   - Thinking toggle kwarg: an explicitly-disabled thinking config (session
//     level "off" or a cron payload override) gets the model's
//     chat_template_kwargs toggle attached — on dual-mode vLLM models that
//     kwarg is the only effective off-switch (the reasoning_effort field is a
//     no-op on deepseek-v4). The effort router builds its own disabled config
//     with the kwarg already set; this covers the config-driven path.
func applyModelTuning(cfg *agent.AgentConfig, deps runDeps, params RunParams, providerID, model string) {
	if t := cfg.Thinking; t != nil && t.Type == "disabled" && t.TemplateKwarg == "" {
		t.TemplateKwarg = modelCapability(deps, providerID, model).ThinkingToggleKwarg
	}
	if deps.registry == nil {
		profile := modelrole.ProfileFor(model)
		fillSamplingDefaults(cfg, profile)
		return
	}
	fillSamplingDefaults(cfg, deps.registry.ProfileForModel(providerID, model))
	if params.MaxTokens == nil {
		if floor := deps.registry.TunedMaxTokens(model); floor > cfg.MaxTokens {
			cfg.MaxTokens = floor
		}
	}
}

// fillSamplingDefaults copies profile sampling values into unset config
// fields.
func fillSamplingDefaults(cfg *agent.AgentConfig, profile modelrole.Profile) {
	if cfg.Temperature == nil {
		cfg.Temperature = profile.Temperature
	}
	if cfg.TopP == nil {
		cfg.TopP = profile.TopP
	}
	if cfg.TopK == nil {
		cfg.TopK = profile.TopK
	}
}

// effectiveContextBudget returns the token budget for transcript history,
// clamped to the model's context window when it is known.
//
// The configured budget (MemoryTokenBudget - SystemPromptBudget) was sized
// for large-window remote models; a small local model (e.g. an 8K vLLM serve)
// run with that budget overflows on every long session and burns the mid-loop
// compaction retries instead of compacting up front. When the window is
// known, the history budget must leave room for the system prompt and the
// output reserve inside that window. Unknown window (0) keeps the configured
// budget — never guess.
func effectiveContextBudget(deps runDeps, providerID, model string, logger *slog.Logger) int {
	budget := int(deps.contextCfg.MemoryTokenBudget - deps.contextCfg.SystemPromptBudget) //nolint:gosec // G115
	caps := modelCapability(deps, providerID, model)
	if caps.ContextWindow <= 0 {
		return budget
	}
	reserve := deps.maxTokens
	if reserve <= 0 {
		reserve = defaultOutputReserve
	}
	avail := caps.ContextWindow - int(deps.contextCfg.SystemPromptBudget) - reserve //nolint:gosec // G115
	if avail < minClampedContextBudget {
		avail = minClampedContextBudget
	}
	if avail >= budget {
		return budget
	}
	if logger != nil {
		logger.Info("context budget clamped to model window",
			"provider", providerID, "model", model,
			"window", caps.ContextWindow, "configured", budget, "clamped", avail)
	}
	return avail
}

// contextWindowCeiling returns the maximum history tokens that still fit the
// model's context window (window - system-prompt budget - output reserve), or 0
// when the window is unknown. This is the unclamped `avail` from
// effectiveContextBudget: a turn whose assembled raw history is at or below this
// can run as-is (it fits the window), which is the precondition for deferring
// compaction to the background instead of doing it synchronously. Returning 0
// for an unknown window makes the deferral conservatively fall back to the
// synchronous path.
func contextWindowCeiling(deps runDeps, providerID, model string) int {
	caps := modelCapability(deps, providerID, model)
	if caps.ContextWindow <= 0 {
		return 0
	}
	reserve := deps.maxTokens
	if reserve <= 0 {
		reserve = defaultOutputReserve
	}
	avail := caps.ContextWindow - int(deps.contextCfg.SystemPromptBudget) - reserve //nolint:gosec // G115
	if avail < 0 {
		avail = 0
	}
	return avail
}
