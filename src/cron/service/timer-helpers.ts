import type { CronConfig, CronRetryOn } from "../../config/types.cron.js";
import { resolveCronDeliveryPlan } from "../delivery.js";
import type {
  CronDeliveryStatus,
  CronJob,
  CronMessageChannel,
  CronRunStatus,
  CronRunTelemetry,
} from "../types.js";
import type { CronRunOutcome } from "../types.js";
import type { CronServiceState } from "./state.js";

// ─── Constants ───────────────────────────────────────────────────────────────

export const DEFAULT_FAILURE_ALERT_AFTER = 2;
export const DEFAULT_FAILURE_ALERT_COOLDOWN_MS = 60 * 60_000; // 1 hour

/**
 * Exponential backoff delays (in ms) indexed by consecutive error count.
 * After the last entry the delay stays constant.
 */
export const DEFAULT_BACKOFF_SCHEDULE_MS = [
  30_000, // 1st error  →  30 s
  60_000, // 2nd error  →   1 min
  5 * 60_000, // 3rd error  →   5 min
  15 * 60_000, // 4th error  →  15 min
  60 * 60_000, // 5th+ error →  60 min
];

/** Default max retries for one-shot jobs on transient errors (#24355). */
export const DEFAULT_MAX_TRANSIENT_RETRIES = 3;

const TRANSIENT_PATTERNS: Record<string, RegExp> = {
  rate_limit:
    /(rate[_ ]limit|too many requests|429|resource has been exhausted|cloudflare|tokens per day)/i,
  overloaded:
    /\b529\b|\boverloaded(?:_error)?\b|high demand|temporar(?:ily|y) overloaded|capacity exceeded/i,
  network: /(network|econnreset|econnrefused|fetch failed|socket)/i,
  timeout: /(timeout|etimedout)/i,
  server_error: /\b5\d{2}\b/,
};

// ─── Timeout helpers ─────────────────────────────────────────────────────────

export function timeoutErrorMessage(): string {
  return "cron: job execution timed out";
}

export function isAbortError(err: unknown): boolean {
  if (!(err instanceof Error)) {
    return false;
  }
  return err.name === "AbortError" || err.message === timeoutErrorMessage();
}

// ─── Backoff / retry helpers ─────────────────────────────────────────────────

export function errorBackoffMs(
  consecutiveErrors: number,
  scheduleMs = DEFAULT_BACKOFF_SCHEDULE_MS,
): number {
  const idx = Math.min(consecutiveErrors - 1, scheduleMs.length - 1);
  return scheduleMs[Math.max(0, idx)];
}

export function isTransientCronError(error: string | undefined, retryOn?: CronRetryOn[]): boolean {
  if (!error || typeof error !== "string") {
    return false;
  }
  const keys = retryOn?.length ? retryOn : (Object.keys(TRANSIENT_PATTERNS) as CronRetryOn[]);
  return keys.some((k) => TRANSIENT_PATTERNS[k]?.test(error));
}

export function resolveRetryConfig(cronConfig?: CronConfig) {
  const retry = cronConfig?.retry;
  return {
    maxAttempts:
      typeof retry?.maxAttempts === "number" ? retry.maxAttempts : DEFAULT_MAX_TRANSIENT_RETRIES,
    backoffMs:
      Array.isArray(retry?.backoffMs) && retry.backoffMs.length > 0
        ? retry.backoffMs
        : DEFAULT_BACKOFF_SCHEDULE_MS.slice(0, 3),
    retryOn: Array.isArray(retry?.retryOn) && retry.retryOn.length > 0 ? retry.retryOn : undefined,
  };
}

// ─── Delivery helpers ────────────────────────────────────────────────────────

export function resolveDeliveryStatus(params: {
  job: CronJob;
  delivered?: boolean;
}): CronDeliveryStatus {
  if (params.delivered === true) {
    return "delivered";
  }
  if (params.delivered === false) {
    return "not-delivered";
  }
  return resolveCronDeliveryPlan(params.job).requested ? "unknown" : "not-requested";
}

export function normalizeCronMessageChannel(input: unknown): CronMessageChannel | undefined {
  if (typeof input !== "string") {
    return undefined;
  }
  const channel = input.trim().toLowerCase();
  return channel ? (channel as CronMessageChannel) : undefined;
}

export function normalizeTo(input: unknown): string | undefined {
  if (typeof input !== "string") {
    return undefined;
  }
  const to = input.trim();
  return to ? to : undefined;
}

export function clampPositiveInt(value: unknown, fallback: number): number {
  if (typeof value !== "number" || !Number.isFinite(value)) {
    return fallback;
  }
  const floored = Math.floor(value);
  return floored >= 1 ? floored : fallback;
}

export function clampNonNegativeInt(value: unknown, fallback: number): number {
  if (typeof value !== "number" || !Number.isFinite(value)) {
    return fallback;
  }
  const floored = Math.floor(value);
  return floored >= 0 ? floored : fallback;
}

// ─── Failure alert helpers ───────────────────────────────────────────────────

export function resolveFailureAlert(
  state: CronServiceState,
  job: CronJob,
): {
  after: number;
  cooldownMs: number;
  channel: CronMessageChannel;
  to?: string;
  mode?: "announce" | "webhook";
  accountId?: string;
} | null {
  const globalConfig = state.deps.cronConfig?.failureAlert;
  const jobConfig = job.failureAlert === false ? undefined : job.failureAlert;

  if (job.failureAlert === false) {
    return null;
  }
  if (!jobConfig && globalConfig?.enabled !== true) {
    return null;
  }

  const mode = jobConfig?.mode ?? globalConfig?.mode;
  const explicitTo = normalizeTo(jobConfig?.to);

  return {
    after: clampPositiveInt(jobConfig?.after ?? globalConfig?.after, DEFAULT_FAILURE_ALERT_AFTER),
    cooldownMs: clampNonNegativeInt(
      jobConfig?.cooldownMs ?? globalConfig?.cooldownMs,
      DEFAULT_FAILURE_ALERT_COOLDOWN_MS,
    ),
    channel:
      normalizeCronMessageChannel(jobConfig?.channel) ??
      normalizeCronMessageChannel(job.delivery?.channel) ??
      "last",
    to: mode === "webhook" ? explicitTo : (explicitTo ?? normalizeTo(job.delivery?.to)),
    mode,
    accountId: jobConfig?.accountId ?? globalConfig?.accountId,
  };
}

export function emitFailureAlert(
  state: CronServiceState,
  params: {
    job: CronJob;
    error?: string;
    consecutiveErrors: number;
    channel: CronMessageChannel;
    to?: string;
    mode?: "announce" | "webhook";
    accountId?: string;
  },
) {
  const safeJobName = params.job.name || params.job.id;
  const truncatedError = (params.error?.trim() || "unknown error").slice(0, 200);
  const text = [
    `Cron job "${safeJobName}" failed ${params.consecutiveErrors} times`,
    `Last error: ${truncatedError}`,
  ].join("\n");

  if (state.deps.sendCronFailureAlert) {
    void state.deps
      .sendCronFailureAlert({
        job: params.job,
        text,
        channel: params.channel,
        to: params.to,
        mode: params.mode,
        accountId: params.accountId,
      })
      .catch((err) => {
        state.deps.log.warn(
          { jobId: params.job.id, err: String(err) },
          "cron: failure alert delivery failed",
        );
      });
    return;
  }

  state.deps.enqueueSystemEvent(text, { agentId: params.job.agentId });
  if (params.job.wakeMode === "now") {
    state.deps.requestHeartbeatNow({ reason: `cron:${params.job.id}:failure-alert` });
  }
}

// ─── Shared types ────────────────────────────────────────────────────────────

export type TimedCronRunOutcome = CronRunOutcome &
  CronRunTelemetry & {
    jobId: string;
    delivered?: boolean;
    deliveryAttempted?: boolean;
    startedAt: number;
    endedAt: number;
  };

export type { CronRunStatus };
