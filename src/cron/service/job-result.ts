import { resolveFailoverReasonFromError } from "../../agents/failover-error.js";
import type { CronJob, CronRunStatus, CronRunTelemetry } from "../types.js";
import type { CronRunOutcome } from "../types.js";
import { computeJobNextRunAtMs, recordScheduleComputeError } from "./jobs.js";
import type { CronEvent, CronServiceState } from "./state.js";
import { persist } from "./store.js";
import {
  DEFAULT_BACKOFF_SCHEDULE_MS,
  emitFailureAlert,
  errorBackoffMs,
  isTransientCronError,
  resolveDeliveryStatus,
  resolveFailureAlert,
  resolveRetryConfig,
  type TimedCronRunOutcome,
} from "./timer-helpers.js";

/**
 * Minimum gap between consecutive fires of the same cron job.  This is a
 * safety net that prevents spin-loops when `computeJobNextRunAtMs` returns
 * a value within the same second as the just-completed run.  The guard
 * is intentionally generous (2 s) so it never masks a legitimate schedule
 * but always breaks an infinite re-trigger cycle.  (See #17821)
 */
export const MIN_REFIRE_GAP_MS = 2_000;

export function emit(state: CronServiceState, evt: CronEvent) {
  try {
    state.deps.onEvent?.(evt);
  } catch {
    /* ignore */
  }
}

export function emitJobFinished(
  state: CronServiceState,
  job: CronJob,
  result: {
    status: CronRunStatus;
    delivered?: boolean;
  } & CronRunOutcome &
    CronRunTelemetry,
  runAtMs: number,
) {
  emit(state, {
    jobId: job.id,
    action: "finished",
    status: result.status,
    error: result.error,
    summary: result.summary,
    delivered: result.delivered,
    deliveryStatus: job.state.lastDeliveryStatus,
    deliveryError: job.state.lastDeliveryError,
    sessionId: result.sessionId,
    sessionKey: result.sessionKey,
    runAtMs,
    durationMs: job.state.lastDurationMs,
    nextRunAtMs: job.state.nextRunAtMs,
    model: result.model,
    provider: result.provider,
    usage: result.usage,
  });
}

/**
 * Apply the result of a job execution to the job's state.
 * Handles consecutive error tracking, exponential backoff, one-shot disable,
 * and nextRunAtMs computation. Returns `true` if the job should be deleted.
 */
export function applyJobResult(
  state: CronServiceState,
  job: CronJob,
  result: {
    status: CronRunStatus;
    error?: string;
    delivered?: boolean;
    startedAt: number;
    endedAt: number;
  },
  opts?: {
    // Preserve recurring "every" anchors for manual force runs.
    preserveSchedule?: boolean;
  },
): boolean {
  const prevLastRunAtMs = job.state.lastRunAtMs;
  const computeNextWithPreservedLastRun = (nowMs: number) => {
    const saved = job.state.lastRunAtMs;
    job.state.lastRunAtMs = prevLastRunAtMs;
    try {
      return computeJobNextRunAtMs(job, nowMs);
    } finally {
      job.state.lastRunAtMs = saved;
    }
  };
  job.state.runningAtMs = undefined;
  job.state.lastRunAtMs = result.startedAt;
  job.state.lastRunStatus = result.status;
  job.state.lastStatus = result.status;
  job.state.lastDurationMs = Math.max(0, result.endedAt - result.startedAt);
  job.state.lastError = result.error;
  job.state.lastErrorReason =
    result.status === "error" && typeof result.error === "string"
      ? (resolveFailoverReasonFromError(result.error) ?? undefined)
      : undefined;
  job.state.lastDelivered = result.delivered;
  const deliveryStatus = resolveDeliveryStatus({ job, delivered: result.delivered });
  job.state.lastDeliveryStatus = deliveryStatus;
  job.state.lastDeliveryError =
    deliveryStatus === "not-delivered" && result.error ? result.error : undefined;
  job.updatedAtMs = result.endedAt;

  // Track consecutive errors for backoff / auto-disable.
  if (result.status === "error") {
    job.state.consecutiveErrors = (job.state.consecutiveErrors ?? 0) + 1;
    const alertConfig = resolveFailureAlert(state, job);
    if (alertConfig && job.state.consecutiveErrors >= alertConfig.after) {
      const isBestEffort =
        job.delivery?.bestEffort === true ||
        (job.payload.kind === "agentTurn" && job.payload.bestEffortDeliver === true);
      if (!isBestEffort) {
        const now = state.deps.nowMs();
        const lastAlert = job.state.lastFailureAlertAtMs;
        const inCooldown =
          typeof lastAlert === "number" && now - lastAlert < Math.max(0, alertConfig.cooldownMs);
        if (!inCooldown) {
          emitFailureAlert(state, {
            job,
            error: result.error,
            consecutiveErrors: job.state.consecutiveErrors,
            channel: alertConfig.channel,
            to: alertConfig.to,
            mode: alertConfig.mode,
            accountId: alertConfig.accountId,
          });
          job.state.lastFailureAlertAtMs = now;
        }
      }
    }
  } else {
    job.state.consecutiveErrors = 0;
    job.state.lastFailureAlertAtMs = undefined;
  }

  const shouldDelete =
    job.schedule.kind === "at" && job.deleteAfterRun === true && result.status === "ok";

  if (!shouldDelete) {
    if (job.schedule.kind === "at") {
      if (result.status === "ok" || result.status === "skipped") {
        // One-shot done or skipped: disable to prevent tight-loop (#11452).
        job.enabled = false;
        job.state.nextRunAtMs = undefined;
      } else if (result.status === "error") {
        const retryConfig = resolveRetryConfig(state.deps.cronConfig);
        const transient = isTransientCronError(result.error, retryConfig.retryOn);
        // consecutiveErrors is always set to ≥1 by the increment block above.
        const consecutive = job.state.consecutiveErrors;
        if (transient && consecutive <= retryConfig.maxAttempts) {
          // Schedule retry with backoff (#24355).
          const backoff = errorBackoffMs(consecutive, retryConfig.backoffMs);
          job.state.nextRunAtMs = result.endedAt + backoff;
          state.deps.log.info(
            {
              jobId: job.id,
              jobName: job.name,
              consecutiveErrors: consecutive,
              backoffMs: backoff,
              nextRunAtMs: job.state.nextRunAtMs,
            },
            "cron: scheduling one-shot retry after transient error",
          );
        } else {
          // Permanent error or max retries exhausted: disable.
          // Note: deleteAfterRun:true only triggers on ok (see shouldDelete above),
          // so exhausted-retry jobs are disabled but intentionally kept in the store
          // to preserve the error state for inspection.
          job.enabled = false;
          job.state.nextRunAtMs = undefined;
          state.deps.log.warn(
            {
              jobId: job.id,
              jobName: job.name,
              consecutiveErrors: consecutive,
              error: result.error,
              reason: transient ? "max retries exhausted" : "permanent error",
            },
            "cron: disabling one-shot job after error",
          );
        }
      }
    } else if (result.status === "error" && job.enabled) {
      // Apply exponential backoff for errored jobs to prevent retry storms.
      const backoff = errorBackoffMs(job.state.consecutiveErrors ?? 1, DEFAULT_BACKOFF_SCHEDULE_MS);
      let normalNext: number | undefined;
      try {
        normalNext =
          opts?.preserveSchedule && job.schedule.kind === "every"
            ? computeNextWithPreservedLastRun(result.endedAt)
            : computeJobNextRunAtMs(job, result.endedAt);
      } catch (err) {
        // If the schedule expression/timezone throws (croner edge cases),
        // record the schedule error (auto-disables after repeated failures)
        // and fall back to backoff-only schedule so the state update is not lost.
        recordScheduleComputeError({ state, job, err });
      }
      const backoffNext = result.endedAt + backoff;
      // Use whichever is later: the natural next run or the backoff delay.
      job.state.nextRunAtMs =
        normalNext !== undefined ? Math.max(normalNext, backoffNext) : backoffNext;
      state.deps.log.info(
        {
          jobId: job.id,
          consecutiveErrors: job.state.consecutiveErrors,
          backoffMs: backoff,
          nextRunAtMs: job.state.nextRunAtMs,
        },
        "cron: applying error backoff",
      );
    } else if (job.enabled) {
      let naturalNext: number | undefined;
      try {
        naturalNext =
          opts?.preserveSchedule && job.schedule.kind === "every"
            ? computeNextWithPreservedLastRun(result.endedAt)
            : computeJobNextRunAtMs(job, result.endedAt);
      } catch (err) {
        // If the schedule expression/timezone throws (croner edge cases),
        // record the schedule error (auto-disables after repeated failures)
        // so a persistent throw doesn't cause a MIN_REFIRE_GAP_MS hot loop.
        recordScheduleComputeError({ state, job, err });
      }
      if (job.schedule.kind === "cron") {
        // Safety net: ensure the next fire is at least MIN_REFIRE_GAP_MS
        // after the current run ended.  Prevents spin-loops when the
        // schedule computation lands in the same second due to
        // timezone/croner edge cases (see #17821).
        const minNext = result.endedAt + MIN_REFIRE_GAP_MS;
        job.state.nextRunAtMs =
          naturalNext !== undefined ? Math.max(naturalNext, minNext) : minNext;
      } else {
        job.state.nextRunAtMs = naturalNext;
      }
    } else {
      job.state.nextRunAtMs = undefined;
    }
  }

  return shouldDelete;
}

export function applyOutcomeToStoredJob(
  state: CronServiceState,
  result: TimedCronRunOutcome,
): void {
  const store = state.store;
  if (!store) {
    return;
  }
  const jobs = store.jobs;
  const job = jobs.find((entry) => entry.id === result.jobId);
  if (!job) {
    state.deps.log.warn(
      { jobId: result.jobId },
      "cron: applyOutcomeToStoredJob — job not found after forceReload, result discarded",
    );
    return;
  }

  const shouldDelete = applyJobResult(state, job, {
    status: result.status,
    error: result.error,
    delivered: result.delivered,
    startedAt: result.startedAt,
    endedAt: result.endedAt,
  });

  emitJobFinished(state, job, result, result.startedAt);

  if (shouldDelete) {
    store.jobs = jobs.filter((entry) => entry.id !== job.id);
    emit(state, { jobId: job.id, action: "removed" });
  }
}

export { persist };
