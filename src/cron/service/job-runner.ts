import type { CronJob } from "../types.js";
import { executeJob, executeJobCoreWithTimeout } from "./execute-job.js";
import type { TimedCronRunOutcome } from "./execute-job.js";
import { applyOutcomeToStoredJob, emit } from "./job-result.js";
import { computeJobPreviousRunAtMs, recomputeNextRunsForMaintenance } from "./jobs.js";
import { locked } from "./locked.js";
import type { CronServiceState } from "./state.js";
import { ensureLoaded, persist } from "./store.js";
import { errorBackoffMs } from "./timer-helpers.js";

const DEFAULT_MISSED_JOB_STAGGER_MS = 5_000;
const DEFAULT_MAX_MISSED_JOBS_PER_RESTART = 5;

type StartupCatchupCandidate = {
  jobId: string;
  job: CronJob;
};

type StartupCatchupPlan = {
  candidates: StartupCatchupCandidate[];
  deferredJobIds: string[];
};

function isErrorBackoffPending(job: CronJob, nowMs: number): boolean {
  if (job.schedule.kind === "at" || job.state.lastStatus !== "error") {
    return false;
  }
  const lastRunAtMs = job.state.lastRunAtMs;
  if (typeof lastRunAtMs !== "number" || !Number.isFinite(lastRunAtMs)) {
    return false;
  }
  const consecutiveErrorsRaw = job.state.consecutiveErrors;
  const consecutiveErrors =
    typeof consecutiveErrorsRaw === "number" && Number.isFinite(consecutiveErrorsRaw)
      ? Math.max(1, Math.floor(consecutiveErrorsRaw))
      : 1;
  return nowMs < lastRunAtMs + errorBackoffMs(consecutiveErrors);
}

function isRunnableJob(params: {
  job: CronJob;
  nowMs: number;
  skipJobIds?: ReadonlySet<string>;
  skipAtIfAlreadyRan?: boolean;
  allowCronMissedRunByLastRun?: boolean;
}): boolean {
  const { job, nowMs } = params;
  if (!job.state) {
    job.state = {};
  }
  if (!job.enabled) {
    return false;
  }
  if (params.skipJobIds?.has(job.id)) {
    return false;
  }
  if (typeof job.state.runningAtMs === "number") {
    return false;
  }
  if (params.skipAtIfAlreadyRan && job.schedule.kind === "at" && job.state.lastStatus) {
    // One-shot with terminal status: skip unless it's a transient-error retry.
    // Retries have nextRunAtMs > lastRunAtMs (scheduled after the failed run) (#24355).
    // ok/skipped or error-without-retry always skip (#13845).
    const lastRun = job.state.lastRunAtMs;
    const nextRun = job.state.nextRunAtMs;
    if (
      job.state.lastStatus === "error" &&
      job.enabled &&
      typeof nextRun === "number" &&
      typeof lastRun === "number" &&
      nextRun > lastRun
    ) {
      return nowMs >= nextRun;
    }
    return false;
  }
  const next = job.state.nextRunAtMs;
  if (typeof next === "number" && Number.isFinite(next) && nowMs >= next) {
    return true;
  }
  if (
    typeof next === "number" &&
    Number.isFinite(next) &&
    next > nowMs &&
    isErrorBackoffPending(job, nowMs)
  ) {
    // Respect active retry backoff windows on restart, but allow missed-slot
    // replay once the backoff window has elapsed.
    return false;
  }
  if (!params.allowCronMissedRunByLastRun || job.schedule.kind !== "cron") {
    return false;
  }
  let previousRunAtMs: number | undefined;
  try {
    previousRunAtMs = computeJobPreviousRunAtMs(job, nowMs);
  } catch {
    return false;
  }
  if (typeof previousRunAtMs !== "number" || !Number.isFinite(previousRunAtMs)) {
    return false;
  }
  const lastRunAtMs = job.state.lastRunAtMs;
  if (typeof lastRunAtMs !== "number" || !Number.isFinite(lastRunAtMs)) {
    // Only replay a "missed slot" when there is concrete run history.
    return false;
  }
  return previousRunAtMs > lastRunAtMs;
}

export function collectRunnableJobs(
  state: CronServiceState,
  nowMs: number,
  opts?: {
    skipJobIds?: ReadonlySet<string>;
    skipAtIfAlreadyRan?: boolean;
    allowCronMissedRunByLastRun?: boolean;
  },
): CronJob[] {
  if (!state.store) {
    return [];
  }
  return state.store.jobs.filter((job) =>
    isRunnableJob({
      job,
      nowMs,
      skipJobIds: opts?.skipJobIds,
      skipAtIfAlreadyRan: opts?.skipAtIfAlreadyRan,
      allowCronMissedRunByLastRun: opts?.allowCronMissedRunByLastRun,
    }),
  );
}

export async function runDueJobs(state: CronServiceState) {
  if (!state.store) {
    return;
  }
  const now = state.deps.nowMs();
  const due = collectRunnableJobs(state, now);
  for (const job of due) {
    await executeJob(state, job, now, { forced: false });
  }
}

async function planStartupCatchup(
  state: CronServiceState,
  opts?: { skipJobIds?: ReadonlySet<string> },
): Promise<StartupCatchupPlan> {
  const maxImmediate = Math.max(
    0,
    state.deps.maxMissedJobsPerRestart ?? DEFAULT_MAX_MISSED_JOBS_PER_RESTART,
  );
  return locked(state, async () => {
    await ensureLoaded(state, { skipRecompute: true });
    if (!state.store) {
      return { candidates: [], deferredJobIds: [] };
    }

    const now = state.deps.nowMs();
    const missed = collectRunnableJobs(state, now, {
      skipJobIds: opts?.skipJobIds,
      skipAtIfAlreadyRan: true,
      allowCronMissedRunByLastRun: true,
    });
    if (missed.length === 0) {
      return { candidates: [], deferredJobIds: [] };
    }
    const sorted = missed.toSorted(
      (a, b) => (a.state.nextRunAtMs ?? 0) - (b.state.nextRunAtMs ?? 0),
    );
    const startupCandidates = sorted.slice(0, maxImmediate);
    const deferred = sorted.slice(maxImmediate);
    if (deferred.length > 0) {
      state.deps.log.info(
        {
          immediateCount: startupCandidates.length,
          deferredCount: deferred.length,
          totalMissed: missed.length,
        },
        "cron: staggering missed jobs to prevent gateway overload",
      );
    }
    if (startupCandidates.length > 0) {
      state.deps.log.info(
        { count: startupCandidates.length, jobIds: startupCandidates.map((j) => j.id) },
        "cron: running missed jobs after restart",
      );
    }
    for (const job of startupCandidates) {
      job.state.runningAtMs = now;
      job.state.lastError = undefined;
    }
    await persist(state);

    return {
      candidates: startupCandidates.map((job) => ({ jobId: job.id, job })),
      deferredJobIds: deferred.map((job) => job.id),
    };
  });
}

async function runStartupCatchupCandidate(
  state: CronServiceState,
  candidate: StartupCatchupCandidate,
): Promise<TimedCronRunOutcome> {
  const startedAt = state.deps.nowMs();
  emit(state, { jobId: candidate.job.id, action: "started", runAtMs: startedAt });
  try {
    const result = await executeJobCoreWithTimeout(state, candidate.job);
    return {
      jobId: candidate.jobId,
      status: result.status,
      error: result.error,
      summary: result.summary,
      delivered: result.delivered,
      sessionId: result.sessionId,
      sessionKey: result.sessionKey,
      model: result.model,
      provider: result.provider,
      usage: result.usage,
      startedAt,
      endedAt: state.deps.nowMs(),
    };
  } catch (err) {
    return {
      jobId: candidate.jobId,
      status: "error",
      error: String(err),
      startedAt,
      endedAt: state.deps.nowMs(),
    };
  }
}

async function executeStartupCatchupPlan(
  state: CronServiceState,
  plan: StartupCatchupPlan,
): Promise<TimedCronRunOutcome[]> {
  const outcomes: TimedCronRunOutcome[] = [];
  for (const candidate of plan.candidates) {
    outcomes.push(await runStartupCatchupCandidate(state, candidate));
  }
  return outcomes;
}

async function applyStartupCatchupOutcomes(
  state: CronServiceState,
  plan: StartupCatchupPlan,
  outcomes: TimedCronRunOutcome[],
): Promise<void> {
  const staggerMs = Math.max(0, state.deps.missedJobStaggerMs ?? DEFAULT_MISSED_JOB_STAGGER_MS);
  await locked(state, async () => {
    await ensureLoaded(state, { forceReload: true, skipRecompute: true });
    if (!state.store) {
      return;
    }

    for (const result of outcomes) {
      applyOutcomeToStoredJob(state, result);
    }

    if (plan.deferredJobIds.length > 0) {
      const baseNow = state.deps.nowMs();
      let offset = staggerMs;
      for (const jobId of plan.deferredJobIds) {
        const job = state.store.jobs.find((entry) => entry.id === jobId);
        if (!job || !job.enabled) {
          continue;
        }
        job.state.nextRunAtMs = baseNow + offset;
        offset += staggerMs;
      }
    }

    // Preserve any new past-due nextRunAtMs values that became due while
    // startup catch-up was running. They should execute on a future tick
    // instead of being silently advanced.
    recomputeNextRunsForMaintenance(state);
    await persist(state);
  });
}

export async function runMissedJobs(
  state: CronServiceState,
  opts?: { skipJobIds?: ReadonlySet<string> },
) {
  const plan = await planStartupCatchup(state, opts);
  if (plan.candidates.length === 0 && plan.deferredJobIds.length === 0) {
    return;
  }

  const outcomes = await executeStartupCatchupPlan(state, plan);
  await applyStartupCatchupOutcomes(state, plan, outcomes);
}
