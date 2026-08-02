package chat

import (
	"log/slog"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/leafbind"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/modelrole"
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
	// dualModeDefaultThinkingBudget is the thinking budget attached when a
	// dual-mode model runs with no session/caller thinking config (the
	// "adaptive" tier of resolveThinkingConfig). The exact number only
	// matters for budget→effort bucketing on non-dsv4 models; deepseek-v4
	// maps any enabled budget to reasoning_effort "high" (openai.go).
	dualModeDefaultThinkingBudget = 16384
)

// modelCapability resolves the effective capability for the run's
// provider/model pair. The registry layers builtin defaults, vLLM discovery,
// and deneb.json provider overrides; without a registry (tests, minimal
// deployments) only the builtin defaults apply.
func modelCapability(deps runDeps, providerID, model string) leafbind.Capability {
	if deps.registry != nil {
		return deps.registry.CapabilityForModel(providerID, model)
	}
	return leafbind.Builtin(providerID, model)
}

// routingProfileForRun resolves the effort-routing profile for the run's
// provider/model pair. The registry layers builtin defaults (router.Default
// Profile + the model's capability toggle) with deneb.json routing overrides;
// without a registry (tests, minimal deployments) only the builtin default plus
// the model's toggle apply — so an unconfigured deployment still routes the
// current main model exactly as before.
func routingProfileForRun(deps runDeps, providerID, model string) leafbind.Profile {
	if deps.briefcaseMode {
		return leafbind.Profile{}
	}
	if deps.registry != nil {
		return deps.registry.RoutingProfileForModel(providerID, model)
	}
	p := leafbind.DefaultProfile()
	p.ToggleKwarg = leafbind.Builtin(providerID, model).ThinkingToggleKwarg
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
//     kwarg is the only per-request off-switch. The effort router builds its
//     own disabled config with the kwarg already set; this covers the
//     config-driven path.
//   - Thinking default: a dual-mode model with NO thinking config at all gets
//     enabled-adaptive thinking (fillDualModeDefaultThinking) — see that
//     helper for why the serving default can no longer be relied on.
func applyModelTuning(cfg *agent.AgentConfig, deps runDeps, params RunParams, providerID, model string) {
	if deps.briefcaseMode {
		temperature, topP, zero := 0.0, 1.0, 0.0
		cfg.Temperature = &temperature
		cfg.TopP = &topP
		cfg.TopK = nil
		cfg.FrequencyPenalty = &zero
		cfg.PresencePenalty = &zero
		return
	}
	if t := cfg.Thinking; t != nil && t.Type == "disabled" && t.TemplateKwarg == "" {
		t.TemplateKwarg = modelCapability(deps, providerID, model).ThinkingToggleKwarg
	}
	fillDualModeDefaultThinking(cfg, deps, providerID, model)
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

// fillDualModeDefaultThinking gives a dual-mode model (one with a per-request
// chat-template thinking toggle — deepseek-v4 today) an enabled-adaptive
// thinking config when the session/caller specified none.
//
// Historically an unset session level inherited "always thinking" from the
// dual-mode SERVING default, so nil meant thinking-on. The 0731 serving
// (tokenizer_mode=deepseek_v4) flipped that default to non-thinking, which
// silently turned every unconfigured dsv4 chat run — most importantly the
// fallback-from-cloud path — into a no-thinking run. Filling the default at
// the model layer restores the intended semantics without touching sessions
// that chose a level ("off" stays off) and without affecting models that have
// no toggle (cloud mains stay exactly as configured). The model is adaptive:
// easy turns emit near-zero reasoning, so the cost is self-limiting.
//
// Called from applyModelTuning (initial model) AND the fallback chain
// (run_fallback.go), which reuses the original config with the model swapped
// and therefore never re-runs applyModelTuning.
func fillDualModeDefaultThinking(cfg *agent.AgentConfig, deps runDeps, providerID, model string) {
	if cfg.Thinking != nil {
		return
	}
	if modelCapability(deps, providerID, model).ThinkingToggleKwarg == "" {
		return
	}
	cfg.Thinking = &llm.ThinkingConfig{Type: "enabled", BudgetTokens: dualModeDefaultThinkingBudget}
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
// can run as-is (it fits the window). Callers that can safely rely on the
// configured history budget for unknown-window providers must apply that
// fallback explicitly.
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
