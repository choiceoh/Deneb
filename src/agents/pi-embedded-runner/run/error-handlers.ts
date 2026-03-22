import type { DenebConfig } from "../../../config/config.js";
import { computeBackoff, sleepWithAbort } from "../../../infra/backoff.js";
import {
  coerceToFailoverError,
  describeFailoverError,
  FailoverError,
  resolveFailoverStatus,
} from "../../failover-error.js";
import {
  classifyFailoverReason,
  formatAssistantErrorText,
  formatBillingErrorMessage,
  isAuthAssistantError,
  isBillingAssistantError,
  isFailoverAssistantError,
  isFailoverErrorMessage,
  isRateLimitAssistantError,
  isTimeoutErrorMessage,
  parseImageDimensionError,
  parseImageSizeError,
  pickFallbackThinkingLevel,
  type FailoverReason,
} from "../../pi-embedded-helpers.js";
import type { NormalizedUsage } from "../../usage.js";
import { log } from "../logger.js";
import {
  buildErrorAgentMeta,
  OVERLOAD_FAILOVER_BACKOFF_POLICY,
  resolveActiveErrorContext,
  type UsageAccumulator,
} from "../run-usage.js";
import type { EmbeddedPiRunResult } from "../types.js";
import { describeUnknownError } from "../utils.js";
import type { AuthProfileOrchestrator } from "./auth-profile-orchestration.js";
import { createFailoverDecisionLogger } from "./failover-observation.js";
import type { RuntimeAuthManager } from "./runtime-auth.js";
import type { EmbeddedRunAttemptResult } from "./types.js";

export type ErrorHandlerContext = {
  params: {
    sessionId: string;
    sessionKey?: string;
    runId?: string;
    abortSignal?: AbortSignal;
  };
  provider: string;
  modelId: string;
  modelOriginalId: string;
  started: number;
  fallbackConfigured: boolean;
  usageAccumulator: UsageAccumulator;
  lastRunPromptUsage: NormalizedUsage | undefined;
  config: DenebConfig | undefined;
  authOrchestrator: AuthProfileOrchestrator;
  runtimeAuthManager: RuntimeAuthManager;
  isProbeSession: boolean;
};

export type PromptErrorResult =
  | { action: "continue"; authRetryPending?: boolean }
  | { action: "throw"; error: unknown }
  | { action: "return"; result: EmbeddedPiRunResult };

/**
 * Handle prompt-level errors (errors thrown when submitting the prompt to the API).
 */
export async function handlePromptError(
  ctx: ErrorHandlerContext,
  attempt: EmbeddedRunAttemptResult,
  overloadFailoverAttempts: { value: number },
  runtimeAuthRetry = false,
): Promise<PromptErrorResult> {
  const { provider, modelId, authOrchestrator, runtimeAuthManager, fallbackConfigured } = ctx;
  const { lastAssistant, promptError, aborted, sessionIdUsed } = attempt;

  const activeErrorContext = resolveActiveErrorContext({
    lastAssistant,
    provider,
    model: modelId,
  });

  const normalizedPromptFailover = coerceToFailoverError(promptError, {
    provider: activeErrorContext.provider,
    model: activeErrorContext.model,
    profileId: authOrchestrator.lastProfileId,
  });
  const promptErrorDetails = normalizedPromptFailover
    ? describeFailoverError(normalizedPromptFailover)
    : describeFailoverError(promptError);
  const errorText = promptErrorDetails.message || describeUnknownError(promptError);

  // Try runtime auth refresh for auth errors
  if (await runtimeAuthManager.maybeRefreshForAuthError(errorText, runtimeAuthRetry)) {
    return { action: "continue", authRetryPending: true };
  }

  // Handle role ordering errors
  if (/incorrect role information|roles must alternate/i.test(errorText)) {
    return {
      action: "return",
      result: {
        payloads: [
          {
            text:
              "Message ordering conflict - please try again. " +
              "If this persists, use /new to start a fresh session.",
            isError: true,
          },
        ],
        meta: {
          durationMs: Date.now() - ctx.started,
          agentMeta: buildErrorAgentMeta({
            sessionId: sessionIdUsed,
            provider,
            model: ctx.modelOriginalId,
            usageAccumulator: ctx.usageAccumulator,
            lastRunPromptUsage: ctx.lastRunPromptUsage,
            lastAssistant,
            lastTurnTotal: undefined,
          }),
          systemPromptReport: attempt.systemPromptReport,
          error: { kind: "role_ordering", message: errorText },
        },
      },
    };
  }

  // Handle image size errors
  const imageSizeError = parseImageSizeError(errorText);
  if (imageSizeError) {
    const maxMb = imageSizeError.maxMb;
    const maxMbLabel = typeof maxMb === "number" && Number.isFinite(maxMb) ? `${maxMb}` : null;
    const maxBytesHint = maxMbLabel ? ` (max ${maxMbLabel}MB)` : "";
    return {
      action: "return",
      result: {
        payloads: [
          {
            text:
              `Image too large for the model${maxBytesHint}. ` +
              "Please compress or resize the image and try again.",
            isError: true,
          },
        ],
        meta: {
          durationMs: Date.now() - ctx.started,
          agentMeta: buildErrorAgentMeta({
            sessionId: sessionIdUsed,
            provider,
            model: ctx.modelOriginalId,
            usageAccumulator: ctx.usageAccumulator,
            lastRunPromptUsage: ctx.lastRunPromptUsage,
            lastAssistant,
            lastTurnTotal: undefined,
          }),
          systemPromptReport: attempt.systemPromptReport,
          error: { kind: "image_size", message: errorText },
        },
      },
    };
  }

  const promptFailoverReason = promptErrorDetails.reason ?? classifyFailoverReason(errorText);
  const promptProfileFailureReason =
    authOrchestrator.resolveProfileFailureReason(promptFailoverReason);
  await authOrchestrator.markProfileFailure({
    profileId: authOrchestrator.lastProfileId,
    reason: promptProfileFailureReason,
  });
  const promptFailoverFailure = promptFailoverReason !== null || isFailoverErrorMessage(errorText);
  const failedPromptProfileId = authOrchestrator.lastProfileId;
  const logPromptFailoverDecision = createFailoverDecisionLogger({
    stage: "prompt",
    runId: ctx.params.runId,
    rawError: errorText,
    failoverReason: promptFailoverReason,
    profileFailureReason: promptProfileFailureReason,
    provider,
    model: modelId,
    profileId: failedPromptProfileId,
    fallbackConfigured,
    aborted,
  });

  // Rotate auth profile on failover
  if (
    promptFailoverFailure &&
    promptFailoverReason !== "timeout" &&
    (await authOrchestrator.advanceAuthProfile())
  ) {
    logPromptFailoverDecision("rotate_profile");
    await maybeBackoffBeforeOverloadFailover(
      promptFailoverReason,
      overloadFailoverAttempts,
      provider,
      modelId,
      ctx.params.abortSignal,
    );
    return { action: "continue" };
  }

  // Try fallback thinking level
  const fallbackThinking = pickFallbackThinkingLevel({
    message: errorText,
    attempted: authOrchestrator.attemptedThinking,
  });
  if (fallbackThinking) {
    log.warn(
      `unsupported thinking level for ${provider}/${modelId}; retrying with ${fallbackThinking}`,
    );
    authOrchestrator.thinkLevel = fallbackThinking;
    return { action: "continue" };
  }

  // Throw FailoverError for model fallback
  if (fallbackConfigured && promptFailoverFailure) {
    const status = resolveFailoverStatus(promptFailoverReason ?? "unknown");
    logPromptFailoverDecision("fallback_model", { status });
    await maybeBackoffBeforeOverloadFailover(
      promptFailoverReason,
      overloadFailoverAttempts,
      provider,
      modelId,
      ctx.params.abortSignal,
    );
    return {
      action: "throw",
      error:
        normalizedPromptFailover ??
        new FailoverError(errorText, {
          reason: promptFailoverReason ?? "unknown",
          provider,
          model: modelId,
          profileId: authOrchestrator.lastProfileId,
          status: resolveFailoverStatus(promptFailoverReason ?? "unknown"),
        }),
    };
  }

  if (promptFailoverFailure || promptFailoverReason) {
    logPromptFailoverDecision("surface_error");
  }
  return { action: "throw", error: promptError };
}

export type AssistantErrorResult =
  | { action: "continue"; authRetryPending?: boolean }
  | { action: "throw"; error: unknown }
  | { action: "none" };

/**
 * Handle assistant-level errors (errors in the response from the LLM).
 * Returns "none" if no error handling is needed (success path).
 */
export async function handleAssistantError(
  ctx: ErrorHandlerContext,
  attempt: EmbeddedRunAttemptResult,
  overloadFailoverAttempts: { value: number },
): Promise<AssistantErrorResult> {
  const { provider, modelId, config, authOrchestrator, runtimeAuthManager, fallbackConfigured } =
    ctx;
  const { lastAssistant, aborted, timedOut, timedOutDuringCompaction, cloudCodeAssistFormatError } =
    attempt;

  const activeErrorContext = resolveActiveErrorContext({
    lastAssistant,
    provider,
    model: modelId,
  });

  // Fallback thinking level
  const fallbackThinking = pickFallbackThinkingLevel({
    message: lastAssistant?.errorMessage,
    attempted: authOrchestrator.attemptedThinking,
  });
  if (fallbackThinking && !aborted) {
    log.warn(
      `unsupported thinking level for ${provider}/${modelId}; retrying with ${fallbackThinking}`,
    );
    authOrchestrator.thinkLevel = fallbackThinking;
    return { action: "continue" };
  }

  const authFailure = isAuthAssistantError(lastAssistant);
  const rateLimitFailure = isRateLimitAssistantError(lastAssistant);
  const billingFailure = isBillingAssistantError(lastAssistant);
  const failoverFailure = isFailoverAssistantError(lastAssistant);
  const assistantFailoverReason = classifyFailoverReason(lastAssistant?.errorMessage ?? "");
  const assistantProfileFailureReason =
    authOrchestrator.resolveProfileFailureReason(assistantFailoverReason);
  const imageDimensionError = parseImageDimensionError(lastAssistant?.errorMessage ?? "");
  const failedAssistantProfileId = authOrchestrator.lastProfileId;
  const logAssistantFailoverDecision = createFailoverDecisionLogger({
    stage: "assistant",
    runId: ctx.params.runId,
    rawError: lastAssistant?.errorMessage?.trim(),
    failoverReason: assistantFailoverReason,
    profileFailureReason: assistantProfileFailureReason,
    provider: activeErrorContext.provider,
    model: activeErrorContext.model,
    profileId: failedAssistantProfileId,
    fallbackConfigured,
    timedOut,
    aborted,
  });

  // Auth error runtime refresh
  if (
    authFailure &&
    (await runtimeAuthManager.maybeRefreshForAuthError(lastAssistant?.errorMessage ?? "", false))
  ) {
    return { action: "continue", authRetryPending: true };
  }

  // Image dimension warning
  if (imageDimensionError && authOrchestrator.lastProfileId) {
    const details = [
      imageDimensionError.messageIndex !== undefined
        ? `message=${imageDimensionError.messageIndex}`
        : null,
      imageDimensionError.contentIndex !== undefined
        ? `content=${imageDimensionError.contentIndex}`
        : null,
      imageDimensionError.maxDimensionPx !== undefined
        ? `limit=${imageDimensionError.maxDimensionPx}px`
        : null,
    ]
      .filter(Boolean)
      .join(" ");
    log.warn(
      `Profile ${authOrchestrator.lastProfileId} rejected image payload${details ? ` (${details})` : ""}.`,
    );
  }

  // Determine if we should rotate profiles
  const shouldRotate = (!aborted && failoverFailure) || (timedOut && !timedOutDuringCompaction);

  if (!shouldRotate) {
    return { action: "none" };
  }

  if (authOrchestrator.lastProfileId) {
    const reason = timedOut ? "timeout" : assistantProfileFailureReason;
    await authOrchestrator.markProfileFailure({
      profileId: authOrchestrator.lastProfileId,
      reason,
    });
    if (timedOut && !ctx.isProbeSession) {
      log.warn(`Profile ${authOrchestrator.lastProfileId} timed out. Trying next account...`);
    }
    if (cloudCodeAssistFormatError) {
      log.warn(
        `Profile ${authOrchestrator.lastProfileId} hit Cloud Code Assist format error. Tool calls will be sanitized on retry.`,
      );
    }
  }

  const rotated = await authOrchestrator.advanceAuthProfile();
  if (rotated) {
    logAssistantFailoverDecision("rotate_profile");
    await maybeBackoffBeforeOverloadFailover(
      assistantFailoverReason,
      overloadFailoverAttempts,
      provider,
      modelId,
      ctx.params.abortSignal,
    );
    return { action: "continue" };
  }

  if (fallbackConfigured) {
    await maybeBackoffBeforeOverloadFailover(
      assistantFailoverReason,
      overloadFailoverAttempts,
      provider,
      modelId,
      ctx.params.abortSignal,
    );
    const message =
      (lastAssistant
        ? formatAssistantErrorText(lastAssistant, {
            cfg: config,
            sessionKey: ctx.params.sessionKey ?? ctx.params.sessionId,
            provider: activeErrorContext.provider,
            model: activeErrorContext.model,
          })
        : undefined) ||
      lastAssistant?.errorMessage?.trim() ||
      (timedOut
        ? "LLM request timed out."
        : rateLimitFailure
          ? "LLM request rate limited."
          : billingFailure
            ? formatBillingErrorMessage(activeErrorContext.provider, activeErrorContext.model)
            : authFailure
              ? "LLM request unauthorized."
              : "LLM request failed.");
    const status =
      resolveFailoverStatus(assistantFailoverReason ?? "unknown") ??
      (isTimeoutErrorMessage(message) ? 408 : undefined);
    logAssistantFailoverDecision("fallback_model", { status });
    return {
      action: "throw",
      error: new FailoverError(message, {
        reason: assistantFailoverReason ?? "unknown",
        provider: activeErrorContext.provider,
        model: activeErrorContext.model,
        profileId: authOrchestrator.lastProfileId,
        status,
      }),
    };
  }

  logAssistantFailoverDecision("surface_error");
  return { action: "none" };
}

/**
 * Apply exponential backoff before overload failover to avoid tight retry bursts.
 */
export async function maybeBackoffBeforeOverloadFailover(
  reason: FailoverReason | null,
  counter: { value: number },
  provider: string,
  modelId: string,
  abortSignal?: AbortSignal,
): Promise<void> {
  if (reason !== "overloaded") {
    return;
  }
  counter.value += 1;
  const delayMs = computeBackoff(OVERLOAD_FAILOVER_BACKOFF_POLICY, counter.value);
  log.warn(
    `overload backoff before failover for ${provider}/${modelId}: attempt=${counter.value} delayMs=${delayMs}`,
  );
  try {
    await sleepWithAbort(delayMs, abortSignal);
  } catch (err) {
    if (abortSignal?.aborted) {
      const abortErr = new Error("Operation aborted", { cause: err });
      abortErr.name = "AbortError";
      throw abortErr;
    }
    throw err;
  }
}
