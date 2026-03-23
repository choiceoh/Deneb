import type { BackoffPolicy } from "../../infra/backoff.js";
import { generateSecureToken } from "../../infra/secure-random.js";
import type { ResolvedProviderAuth } from "../models/model-auth.js";
import { derivePromptTokens, normalizeUsage, type UsageLike } from "../usage.js";
import type { EmbeddedPiAgentMeta } from "./types.js";

export type ApiKeyInfo = ResolvedProviderAuth;

export type RuntimeAuthState = {
  sourceApiKey: string;
  authMode: string;
  profileId?: string;
  expiresAt?: number;
  refreshTimer?: ReturnType<typeof setTimeout>;
  refreshInFlight?: Promise<void>;
};

export const RUNTIME_AUTH_REFRESH_MARGIN_MS = 5 * 60 * 1000;
export const RUNTIME_AUTH_REFRESH_RETRY_MS = 60 * 1000;
export const RUNTIME_AUTH_REFRESH_MIN_DELAY_MS = 5 * 1000;
// Keep overload pacing noticeable enough to avoid tight retry bursts, but short
// enough that fallback still feels responsive within a single turn.
export const OVERLOAD_FAILOVER_BACKOFF_POLICY: BackoffPolicy = {
  initialMs: 250,
  maxMs: 1_500,
  factor: 2,
  jitter: 0.2,
};

// Avoid Anthropic's refusal test token poisoning session transcripts.
export const ANTHROPIC_MAGIC_STRING_TRIGGER_REFUSAL = "ANTHROPIC_MAGIC_STRING_TRIGGER_REFUSAL";
export const ANTHROPIC_MAGIC_STRING_REPLACEMENT =
  "ANTHROPIC MAGIC STRING TRIGGER REFUSAL (redacted)";

export function scrubAnthropicRefusalMagic(prompt: string): string {
  if (!prompt.includes(ANTHROPIC_MAGIC_STRING_TRIGGER_REFUSAL)) {
    return prompt;
  }
  return prompt.replaceAll(
    ANTHROPIC_MAGIC_STRING_TRIGGER_REFUSAL,
    ANTHROPIC_MAGIC_STRING_REPLACEMENT,
  );
}

export type UsageAccumulator = {
  input: number;
  output: number;
  cacheRead: number;
  cacheWrite: number;
  total: number;
  /** Cache fields from the most recent API call (not accumulated). */
  lastCacheRead: number;
  lastCacheWrite: number;
  lastInput: number;
};

export const createUsageAccumulator = (): UsageAccumulator => ({
  input: 0,
  output: 0,
  cacheRead: 0,
  cacheWrite: 0,
  total: 0,
  lastCacheRead: 0,
  lastCacheWrite: 0,
  lastInput: 0,
});

export function createCompactionDiagId(): string {
  return `ovf-${Date.now().toString(36)}-${generateSecureToken(4)}`;
}

// Defensive guard for the outer run loop across all retry branches.
export const BASE_RUN_RETRY_ITERATIONS = 24;
export const RUN_RETRY_ITERATIONS_PER_PROFILE = 8;
export const MIN_RUN_RETRY_ITERATIONS = 32;
export const MAX_RUN_RETRY_ITERATIONS = 160;

export function resolveMaxRunRetryIterations(profileCandidateCount: number): number {
  const scaled =
    BASE_RUN_RETRY_ITERATIONS +
    Math.max(1, profileCandidateCount) * RUN_RETRY_ITERATIONS_PER_PROFILE;
  return Math.min(MAX_RUN_RETRY_ITERATIONS, Math.max(MIN_RUN_RETRY_ITERATIONS, scaled));
}

export const hasUsageValues = (
  usage: ReturnType<typeof normalizeUsage>,
): usage is NonNullable<ReturnType<typeof normalizeUsage>> =>
  !!usage &&
  [usage.input, usage.output, usage.cacheRead, usage.cacheWrite, usage.total].some(
    (value) => typeof value === "number" && Number.isFinite(value) && value > 0,
  );

export const mergeUsageIntoAccumulator = (
  target: UsageAccumulator,
  usage: ReturnType<typeof normalizeUsage>,
) => {
  if (!hasUsageValues(usage)) {
    return;
  }
  target.input += usage.input ?? 0;
  target.output += usage.output ?? 0;
  target.cacheRead += usage.cacheRead ?? 0;
  target.cacheWrite += usage.cacheWrite ?? 0;
  target.total +=
    usage.total ??
    (usage.input ?? 0) + (usage.output ?? 0) + (usage.cacheRead ?? 0) + (usage.cacheWrite ?? 0);
  // Track the most recent API call's cache fields for accurate context-size reporting.
  // Accumulated cache totals inflate context size when there are multiple tool-call round-trips,
  // since each call reports cacheRead ≈ current_context_size.
  target.lastCacheRead = usage.cacheRead ?? 0;
  target.lastCacheWrite = usage.cacheWrite ?? 0;
  target.lastInput = usage.input ?? 0;
};

export const toNormalizedUsage = (usage: UsageAccumulator) => {
  const hasUsage =
    usage.input > 0 ||
    usage.output > 0 ||
    usage.cacheRead > 0 ||
    usage.cacheWrite > 0 ||
    usage.total > 0;
  if (!hasUsage) {
    return undefined;
  }
  // Use the LAST API call's cache fields for context-size calculation.
  // The accumulated cacheRead/cacheWrite inflate context size because each tool-call
  // round-trip reports cacheRead ≈ current_context_size, and summing N calls gives
  // N × context_size which gets clamped to contextWindow (e.g. 200k).
  // See: https://github.com/deneb/deneb/issues/13698
  //
  // We use lastInput/lastCacheRead/lastCacheWrite (from the most recent API call) for
  // cache-related fields, but keep accumulated output (total generated text this turn).
  const lastPromptTokens = usage.lastInput + usage.lastCacheRead + usage.lastCacheWrite;
  return {
    input: usage.lastInput || undefined,
    output: usage.output || undefined,
    cacheRead: usage.lastCacheRead || undefined,
    cacheWrite: usage.lastCacheWrite || undefined,
    total: lastPromptTokens + usage.output || undefined,
  };
};

export function resolveActiveErrorContext(params: {
  lastAssistant: { provider?: string; model?: string } | undefined;
  provider: string;
  model: string;
}): { provider: string; model: string } {
  return {
    provider: params.lastAssistant?.provider ?? params.provider,
    model: params.lastAssistant?.model ?? params.model,
  };
}

/**
 * Build agentMeta for error return paths, preserving accumulated usage so that
 * session totalTokens reflects the actual context size rather than going stale.
 * Without this, error returns omit usage and the session keeps whatever
 * totalTokens was set by the previous successful run.
 */
export function buildErrorAgentMeta(params: {
  sessionId: string;
  provider: string;
  model: string;
  usageAccumulator: UsageAccumulator;
  lastRunPromptUsage: ReturnType<typeof normalizeUsage> | undefined;
  lastAssistant?: { usage?: unknown } | null;
  /** API-reported total from the most recent call, mirroring the success path correction. */
  lastTurnTotal?: number;
}): EmbeddedPiAgentMeta {
  const usage = toNormalizedUsage(params.usageAccumulator);
  // Apply the same lastTurnTotal correction the success path uses so
  // usage.total reflects the API-reported context size, not accumulated totals.
  if (usage && params.lastTurnTotal && params.lastTurnTotal > 0) {
    usage.total = params.lastTurnTotal;
  }
  const lastCallUsage = params.lastAssistant
    ? normalizeUsage(params.lastAssistant.usage as UsageLike)
    : undefined;
  const promptTokens = derivePromptTokens(params.lastRunPromptUsage);
  return {
    sessionId: params.sessionId,
    provider: params.provider,
    model: params.model,
    // Only include usage fields when we have actual data from prior API calls.
    ...(usage ? { usage } : {}),
    ...(lastCallUsage ? { lastCallUsage } : {}),
    ...(promptTokens ? { promptTokens } : {}),
  };
}
