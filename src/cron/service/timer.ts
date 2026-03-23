import { DEFAULT_AGENT_ID } from "../../routing/session-key.js";
import { sweepCronRunSessions } from "../session-reaper.js";
import { runConcurrentDueJobs } from "./execute-job.js";
import { applyOutcomeToStoredJob, emit } from "./job-result.js";
import { collectRunnableJobs, runMissedJobs } from "./job-runner.js";
import { recomputeNextRunsForMaintenance, nextWakeAtMs } from "./jobs.js";
import { locked } from "./locked.js";
import type { CronServiceState } from "./state.js";
import { ensureLoaded, persist } from "./store.js";

export { DEFAULT_JOB_TIMEOUT_MS } from "./timeout-policy.js";

// Re-exports for backwards compatibility with external callers.
export { applyJobResult, emit } from "./job-result.js";
export { executeJobCore, executeJobCoreWithTimeout, executeJob } from "./execute-job.js";
export { runDueJobs, runMissedJobs } from "./job-runner.js";

const MAX_TIMER_DELAY_MS = 60_000;

/**
 * Minimum gap between consecutive fires of the same cron job.  This is a
 * safety net that prevents spin-loops when `computeJobNextRunAtMs` returns
 * a value within the same second as the just-completed run.  The guard
 * is intentionally generous (2 s) so it never masks a legitimate schedule
 * but always breaks an infinite re-trigger cycle.  (See #17821)
 */
const MIN_REFIRE_GAP_MS = 2_000;

export function armTimer(state: CronServiceState) {
  if (state.timer) {
    clearTimeout(state.timer);
  }
  state.timer = null;
  if (!state.deps.cronEnabled) {
    state.deps.log.debug({}, "cron: armTimer skipped - scheduler disabled");
    return;
  }
  const nextAt = nextWakeAtMs(state);
  if (!nextAt) {
    const jobCount = state.store?.jobs.length ?? 0;
    const enabledCount = state.store?.jobs.filter((j) => j.enabled).length ?? 0;
    const withNextRun =
      state.store?.jobs.filter(
        (j) =>
          j.enabled &&
          typeof j.state.nextRunAtMs === "number" &&
          Number.isFinite(j.state.nextRunAtMs),
      ).length ?? 0;
    state.deps.log.debug(
      { jobCount, enabledCount, withNextRun },
      "cron: armTimer skipped - no jobs with nextRunAtMs",
    );
    return;
  }
  const now = state.deps.nowMs();
  const delay = Math.max(nextAt - now, 0);
  // Floor: when the next wake time is in the past (delay === 0), enforce a
  // minimum delay to prevent a tight setTimeout(0) loop.  This can happen
  // when a job has a stuck runningAtMs marker and a past-due nextRunAtMs:
  // findDueJobs skips the job (blocked by runningAtMs), while
  // recomputeNextRunsForMaintenance intentionally does not advance the
  // past-due nextRunAtMs (per #13992).  The finally block in onTimer then
  // re-invokes armTimer with delay === 0, creating an infinite hot-loop
  // that saturates the event loop and fills the log file to its size cap.
  const flooredDelay = delay === 0 ? MIN_REFIRE_GAP_MS : delay;
  // Wake at least once a minute to avoid schedule drift and recover quickly
  // when the process was paused or wall-clock time jumps.
  const clampedDelay = Math.min(flooredDelay, MAX_TIMER_DELAY_MS);
  // Intentionally avoid an `async` timer callback:
  // Vitest's fake-timer helpers can await async callbacks, which would block
  // tests that simulate long-running jobs. Runtime behavior is unchanged.
  state.timer = setTimeout(() => {
    void onTimer(state).catch((err) => {
      state.deps.log.error({ err: String(err) }, "cron: timer tick failed");
    });
  }, clampedDelay);
  state.deps.log.debug(
    { nextAt, delayMs: clampedDelay, clamped: delay > MAX_TIMER_DELAY_MS },
    "cron: timer armed",
  );
}

function armRunningRecheckTimer(state: CronServiceState) {
  if (state.timer) {
    clearTimeout(state.timer);
  }
  state.timer = setTimeout(() => {
    void onTimer(state).catch((err) => {
      state.deps.log.error({ err: String(err) }, "cron: timer tick failed");
    });
  }, MAX_TIMER_DELAY_MS);
}

export async function onTimer(state: CronServiceState) {
  if (state.running) {
    // Re-arm the timer so the scheduler keeps ticking even when a job is
    // still executing.  Without this, a long-running job (e.g. an agentTurn
    // exceeding MAX_TIMER_DELAY_MS) causes the clamped 60 s timer to fire
    // while `running` is true.  The early return then leaves no timer set,
    // silently killing the scheduler until the next gateway restart.
    //
    // We use MAX_TIMER_DELAY_MS as a fixed re-check interval to avoid a
    // zero-delay hot-loop when past-due jobs are waiting for the current
    // execution to finish.
    // See: https://github.com/deneb/deneb/issues/12025
    armRunningRecheckTimer(state);
    return;
  }
  state.running = true;
  // Keep a watchdog timer armed while a tick is executing. If execution hangs
  // (for example in a provider call), the scheduler still wakes to re-check.
  armRunningRecheckTimer(state);
  try {
    const dueJobs = await locked(state, async () => {
      await ensureLoaded(state, { forceReload: true, skipRecompute: true });
      const dueCheckNow = state.deps.nowMs();
      const due = collectRunnableJobs(state, dueCheckNow);

      if (due.length === 0) {
        // Use maintenance-only recompute to avoid advancing past-due nextRunAtMs
        // values without execution. This prevents jobs from being silently skipped
        // when the timer wakes up but findDueJobs returns empty (see #13992).
        const changed = recomputeNextRunsForMaintenance(state, {
          recomputeExpired: true,
          nowMs: dueCheckNow,
        });
        if (changed) {
          await persist(state);
        }
        return [];
      }

      const now = state.deps.nowMs();
      for (const job of due) {
        job.state.runningAtMs = now;
        job.state.lastError = undefined;
      }
      await persist(state);

      return due.map((j) => ({
        id: j.id,
        job: j,
      }));
    });

    const completedResults = await runConcurrentDueJobs(state, dueJobs);

    if (completedResults.length > 0) {
      await locked(state, async () => {
        await ensureLoaded(state, { forceReload: true, skipRecompute: true });
        for (const result of completedResults) {
          applyOutcomeToStoredJob(state, result);
        }

        // Use maintenance-only recompute to avoid advancing past-due
        // nextRunAtMs values that became due between findDueJobs and this
        // locked block.  The full recomputeNextRuns would silently skip
        // those jobs (advancing nextRunAtMs without execution), causing
        // daily cron schedules to jump 48 h instead of 24 h (#17852).
        recomputeNextRunsForMaintenance(state);
        await persist(state);
      });
    }
  } finally {
    // Piggyback session reaper on timer tick (self-throttled to every 5 min).
    // Placed in `finally` so the reaper runs even when a long-running job keeps
    // `state.running` true across multiple timer ticks — the early return at the
    // top of onTimer would otherwise skip the reaper indefinitely.
    const storePaths = new Set<string>();
    if (state.deps.resolveSessionStorePath) {
      const defaultAgentId = state.deps.defaultAgentId ?? DEFAULT_AGENT_ID;
      if (state.store?.jobs?.length) {
        for (const job of state.store.jobs) {
          const agentId =
            typeof job.agentId === "string" && job.agentId.trim() ? job.agentId : defaultAgentId;
          storePaths.add(state.deps.resolveSessionStorePath(agentId));
        }
      } else {
        storePaths.add(state.deps.resolveSessionStorePath(defaultAgentId));
      }
    } else if (state.deps.sessionStorePath) {
      storePaths.add(state.deps.sessionStorePath);
    }

    if (storePaths.size > 0) {
      const nowMs = state.deps.nowMs();
      for (const storePath of storePaths) {
        try {
          await sweepCronRunSessions({
            cronConfig: state.deps.cronConfig,
            sessionStorePath: storePath,
            nowMs,
            log: state.deps.log,
          });
        } catch (err) {
          state.deps.log.warn({ err: String(err), storePath }, "cron: session reaper sweep failed");
        }
      }
    }

    state.running = false;
    armTimer(state);
  }
}

export function wake(
  state: CronServiceState,
  opts: { mode: "now" | "next-heartbeat"; text: string },
) {
  const text = opts.text.trim();
  if (!text) {
    return { ok: false } as const;
  }
  state.deps.enqueueSystemEvent(text);
  if (opts.mode === "now") {
    state.deps.requestHeartbeatNow({ reason: "wake" });
  }
  return { ok: true } as const;
}

export function stopTimer(state: CronServiceState) {
  if (state.timer) {
    clearTimeout(state.timer);
  }
  state.timer = null;
}
