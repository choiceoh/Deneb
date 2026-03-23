import type { HeartbeatRunResult } from "../../infra/heartbeat-wake.js";
import { sleep } from "../../utils.js";
import type { CronRunOutcome, CronRunStatus, CronRunTelemetry } from "../types.js";
import type { CronJob } from "../types.js";
import { applyJobResult, emit, emitJobFinished } from "./job-result.js";
import { resolveJobPayloadTextForMain } from "./jobs.js";
import type { CronServiceState } from "./state.js";
import { resolveCronJobTimeoutMs } from "./timeout-policy.js";
import { isAbortError, timeoutErrorMessage } from "./timer-helpers.js";
import type { TimedCronRunOutcome } from "./timer-helpers.js";

export type { TimedCronRunOutcome };

function resolveRunConcurrency(state: CronServiceState): number {
  const raw = state.deps.cronConfig?.maxConcurrentRuns;
  if (typeof raw !== "number" || !Number.isFinite(raw)) {
    return 1;
  }
  return Math.max(1, Math.floor(raw));
}

export async function executeJobCoreWithTimeout(
  state: CronServiceState,
  job: CronJob,
): Promise<Awaited<ReturnType<typeof executeJobCore>>> {
  const jobTimeoutMs = resolveCronJobTimeoutMs(job);
  if (typeof jobTimeoutMs !== "number") {
    return await executeJobCore(state, job);
  }

  const runAbortController = new AbortController();
  let timeoutId: NodeJS.Timeout | undefined;
  try {
    return await Promise.race([
      executeJobCore(state, job, runAbortController.signal),
      new Promise<never>((_, reject) => {
        timeoutId = setTimeout(() => {
          runAbortController.abort(timeoutErrorMessage());
          reject(new Error(timeoutErrorMessage()));
        }, jobTimeoutMs);
      }),
    ]);
  } finally {
    if (timeoutId) {
      clearTimeout(timeoutId);
    }
  }
}

export async function executeJobCore(
  state: CronServiceState,
  job: CronJob,
  abortSignal?: AbortSignal,
): Promise<
  CronRunOutcome & CronRunTelemetry & { delivered?: boolean; deliveryAttempted?: boolean }
> {
  const resolveAbortError = () => ({
    status: "error" as const,
    error: timeoutErrorMessage(),
  });
  const waitWithAbort = async (ms: number) => {
    if (!abortSignal) {
      await sleep(ms);
      return;
    }
    if (abortSignal.aborted) {
      return;
    }
    await new Promise<void>((resolve) => {
      const timer = setTimeout(() => {
        abortSignal.removeEventListener("abort", onAbort);
        resolve();
      }, ms);
      const onAbort = () => {
        clearTimeout(timer);
        abortSignal.removeEventListener("abort", onAbort);
        resolve();
      };
      abortSignal.addEventListener("abort", onAbort, { once: true });
    });
  };

  if (abortSignal?.aborted) {
    return resolveAbortError();
  }
  if (job.sessionTarget === "main") {
    const text = resolveJobPayloadTextForMain(job);
    if (!text) {
      const kind = job.payload.kind;
      return {
        status: "skipped",
        error:
          kind === "systemEvent"
            ? "main job requires non-empty systemEvent text"
            : 'main job requires payload.kind="systemEvent"',
      };
    }
    // Preserve the job session namespace for main-target reminders so heartbeat
    // routing can deliver follow-through in the originating channel/thread.
    // Downstream gateway wiring canonicalizes/guards this key per agent.
    const targetMainSessionKey = job.sessionKey;
    state.deps.enqueueSystemEvent(text, {
      agentId: job.agentId,
      sessionKey: targetMainSessionKey,
      contextKey: `cron:${job.id}`,
    });
    if (job.wakeMode === "now" && state.deps.runHeartbeatOnce) {
      const reason = `cron:${job.id}`;
      const maxWaitMs = state.deps.wakeNowHeartbeatBusyMaxWaitMs ?? 2 * 60_000;
      const retryDelayMs = state.deps.wakeNowHeartbeatBusyRetryDelayMs ?? 250;
      const waitStartedAt = state.deps.nowMs();

      let heartbeatResult: HeartbeatRunResult;
      for (;;) {
        if (abortSignal?.aborted) {
          return resolveAbortError();
        }
        heartbeatResult = await state.deps.runHeartbeatOnce({
          reason,
          agentId: job.agentId,
          sessionKey: targetMainSessionKey,
          // Cron-triggered heartbeats should deliver to the last active channel.
          // Without this override, heartbeat target defaults to "none" (since
          // e2362d35) and cron main-session responses are silently swallowed.
          // See: https://github.com/deneb/deneb/issues/28508
          heartbeat: { target: "last" },
        });
        if (
          heartbeatResult.status !== "skipped" ||
          heartbeatResult.reason !== "requests-in-flight"
        ) {
          break;
        }
        if (abortSignal?.aborted) {
          return resolveAbortError();
        }
        if (state.deps.nowMs() - waitStartedAt > maxWaitMs) {
          if (abortSignal?.aborted) {
            return resolveAbortError();
          }
          state.deps.requestHeartbeatNow({
            reason,
            agentId: job.agentId,
            sessionKey: targetMainSessionKey,
          });
          return { status: "ok", summary: text };
        }
        await waitWithAbort(retryDelayMs);
      }

      if (heartbeatResult.status === "ran") {
        return { status: "ok", summary: text };
      } else if (heartbeatResult.status === "skipped") {
        return { status: "skipped", error: heartbeatResult.reason, summary: text };
      } else {
        return { status: "error", error: heartbeatResult.reason, summary: text };
      }
    } else {
      if (abortSignal?.aborted) {
        return resolveAbortError();
      }
      state.deps.requestHeartbeatNow({
        reason: `cron:${job.id}`,
        agentId: job.agentId,
        sessionKey: targetMainSessionKey,
      });
      return { status: "ok", summary: text };
    }
  }

  if (job.payload.kind !== "agentTurn") {
    return { status: "skipped", error: "isolated job requires payload.kind=agentTurn" };
  }
  if (abortSignal?.aborted) {
    return resolveAbortError();
  }

  const res = await state.deps.runIsolatedAgentJob({
    job,
    message: job.payload.message,
    abortSignal,
  });

  if (abortSignal?.aborted) {
    return { status: "error", error: timeoutErrorMessage() };
  }

  return {
    status: res.status,
    error: res.error,
    summary: res.summary,
    delivered: res.delivered,
    deliveryAttempted: res.deliveryAttempted,
    sessionId: res.sessionId,
    sessionKey: res.sessionKey,
    model: res.model,
    provider: res.provider,
    usage: res.usage,
  };
}

/**
 * Execute a job. This version is used by the `run` command and other
 * places that need the full execution with state updates.
 */
export async function executeJob(
  state: CronServiceState,
  job: CronJob,
  _nowMs: number,
  _opts: { forced: boolean },
) {
  if (!job.state) {
    job.state = {};
  }
  const startedAt = state.deps.nowMs();
  job.state.runningAtMs = startedAt;
  job.state.lastError = undefined;
  emit(state, { jobId: job.id, action: "started", runAtMs: startedAt });

  let coreResult: {
    status: CronRunStatus;
    delivered?: boolean;
  } & CronRunOutcome &
    CronRunTelemetry;
  try {
    coreResult = await executeJobCore(state, job);
  } catch (err) {
    coreResult = { status: "error", error: String(err) };
  }

  const endedAt = state.deps.nowMs();
  const shouldDelete = applyJobResult(state, job, {
    status: coreResult.status,
    error: coreResult.error,
    delivered: coreResult.delivered,
    startedAt,
    endedAt,
  });

  emitJobFinished(state, job, coreResult, startedAt);

  if (shouldDelete && state.store) {
    state.store.jobs = state.store.jobs.filter((j) => j.id !== job.id);
    emit(state, { jobId: job.id, action: "removed" });
  }
}

export async function runConcurrentDueJobs(
  state: CronServiceState,
  dueJobs: { id: string; job: CronJob }[],
): Promise<TimedCronRunOutcome[]> {
  const runDueJob = async (params: { id: string; job: CronJob }): Promise<TimedCronRunOutcome> => {
    const { id, job } = params;
    const startedAt = state.deps.nowMs();
    job.state.runningAtMs = startedAt;
    emit(state, { jobId: job.id, action: "started", runAtMs: startedAt });
    const jobTimeoutMs = resolveCronJobTimeoutMs(job);

    try {
      const result = await executeJobCoreWithTimeout(state, job);
      return { jobId: id, ...result, startedAt, endedAt: state.deps.nowMs() };
    } catch (err) {
      const errorText = isAbortError(err) ? timeoutErrorMessage() : String(err);
      state.deps.log.warn(
        { jobId: id, jobName: job.name, timeoutMs: jobTimeoutMs ?? null },
        `cron: job failed: ${errorText}`,
      );
      return {
        jobId: id,
        status: "error",
        error: errorText,
        startedAt,
        endedAt: state.deps.nowMs(),
      };
    }
  };

  const concurrency = Math.min(resolveRunConcurrency(state), Math.max(1, dueJobs.length));
  const results: (TimedCronRunOutcome | undefined)[] = Array.from({ length: dueJobs.length });
  let cursor = 0;
  const workers = Array.from({ length: concurrency }, async () => {
    for (;;) {
      const index = cursor++;
      if (index >= dueJobs.length) {
        return;
      }
      const due = dueJobs[index];
      if (!due) {
        return;
      }
      results[index] = await runDueJob(due);
    }
  });
  await Promise.all(workers);

  return results.filter((entry): entry is TimedCronRunOutcome => entry !== undefined);
}
