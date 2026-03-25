// Defaults for agent metadata when upstream does not supply them.
// Model id uses pi-ai's built-in Anthropic catalog.
export const DEFAULT_PROVIDER = "anthropic";
export const DEFAULT_MODEL = "claude-opus-4-6";
// Conservative fallback used when model metadata is unavailable.
export const DEFAULT_CONTEXT_TOKENS = 200_000;

// Per-mode model defaults. Agents can override these per-agent via config.
// thinking: deep reasoning tasks (complex analysis, long-form generation)
export const DEFAULT_THINKING_MODEL = "claude-opus-4-6";
// fast: quick responses, side questions (/btw), simple lookups
export const DEFAULT_FAST_MODEL = "claude-sonnet-4-6";
// reasoning: structured reasoning with explicit chain-of-thought
export const DEFAULT_REASONING_MODEL = "claude-opus-4-6";

/** Model mode identifier for per-agent model selection. */
export type ModelMode = "default" | "thinking" | "fast" | "reasoning";

/** Per-agent model defaults configuration shape. */
export type AgentModelDefaults = {
  /** Default model for general use. Falls back to DEFAULT_MODEL. */
  model?: string;
  /** Model for deep thinking/analysis tasks. */
  thinkingModel?: string;
  /** Model for fast, lightweight responses (e.g., /btw). */
  fastModel?: string;
  /** Model for structured reasoning with explicit CoT. */
  reasoningModel?: string;
};

/**
 * Resolve the model ID for a given agent and mode.
 * Priority: agent-level override → global defaults → hardcoded constants.
 *
 * If the resolved model is not available (checked via `isModelAvailable`),
 * falls back to DEFAULT_MODEL with a warning.
 */
export function resolveAgentModel(
  mode: ModelMode,
  agentDefaults?: AgentModelDefaults,
  opts?: { isModelAvailable?: (modelId: string) => boolean },
): string {
  const modeToField: Record<ModelMode, keyof AgentModelDefaults> = {
    default: "model",
    thinking: "thinkingModel",
    fast: "fastModel",
    reasoning: "reasoningModel",
  };

  const modeToGlobalDefault: Record<ModelMode, string> = {
    default: DEFAULT_MODEL,
    thinking: DEFAULT_THINKING_MODEL,
    fast: DEFAULT_FAST_MODEL,
    reasoning: DEFAULT_REASONING_MODEL,
  };

  // Try agent-level override first.
  const field = modeToField[mode];
  const agentModel = agentDefaults?.[field];
  if (agentModel) {
    // Auto-revert: if the model is not available, fall back.
    if (opts?.isModelAvailable && !opts.isModelAvailable(agentModel)) {
      return DEFAULT_MODEL;
    }
    return agentModel;
  }

  // Fall back to global default for this mode.
  return modeToGlobalDefault[mode];
}
