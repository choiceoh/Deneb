package chat

import (
	"log/slog"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/modelcaps"
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
