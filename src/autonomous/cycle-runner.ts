import type { CliDeps } from "../cli/outbound-send-deps.js";
import type { DenebConfig } from "../config/config.js";
import { runCronIsolatedAgentTurn } from "../cron/isolated-agent/run.js";
import type { CronJob } from "../cron/types.js";
import { createSubsystemLogger } from "../logging/subsystem.js";
import { AttentionManager } from "./attention.js";
import { buildCyclePrompt } from "./prompt.js";
import { loadAutonomousState, saveAutonomousState } from "./state-store.js";
import type { AutonomousConfig, AutonomousState, CycleOutcome } from "./types.js";
import { clampDelay, safeMaxPerHour, isFinitePositive, MAX_TIMEOUT_MS } from "./validation.js";

const log = createSubsystemLogger("autonomous");

const DEFAULT_CYCLE_INTERVAL_MS = 300_000; // 5 minutes
const DEFAULT_MAX_CYCLES_PER_HOUR = 12;
const DEFAULT_TIMEOUT_SECONDS = 120;

/**
 * Execute a single autonomous decision cycle.
 * Loads state, builds context, runs an isolated agent turn, and persists updates.
 */
export async function runAutonomousCycle(params: {
  cfg: DenebConfig;
  deps: CliDeps;
  attention: AttentionManager;
  agentId: string;
  storePath?: string;
  abortSignal?: AbortSignal;
}): Promise<CycleOutcome> {
  const { cfg, deps, attention, agentId, storePath, abortSignal } = params;
  const startedAt = Date.now();

  // Check abort signal BEFORE doing any work.
  if (abortSignal?.aborted) {
    log.info("autonomous cycle skipped: already aborted");
    return {
      cycleNumber: 0,
      startedAt,
      finishedAt: Date.now(),
      actionsTaken: [],
      error: "aborted",
    };
  }

  const autonomousCfg = cfg.autonomous;

  // Load state.
  const state = await loadAutonomousState(storePath);

  // Rate limit check — use safeMaxPerHour to prevent division by zero / NaN.
  const maxPerHour = safeMaxPerHour(autonomousCfg?.maxCyclesPerHour, DEFAULT_MAX_CYCLES_PER_HOUR);
  if (isRateLimited(state, maxPerHour)) {
    log.info("autonomous cycle skipped: rate limit reached");
    return {
      cycleNumber: state.cycleCount,
      startedAt,
      finishedAt: Date.now(),
      actionsTaken: [],
      error: "rate-limited",
    };
  }

  // Restore any persisted signals from the previous gateway session.
  attention.restoreFromState(state);

  // Derive state-based attention signals.
  attention.deriveSignalsFromState(state);

  // Get top signals for the prompt.
  const signals = attention.getTopSignals(10);

  // Snapshot pre-cycle state for ignore detection after the agent runs.
  const preCycleSnapshot = snapshotStateForComparison(state);

  // Build prompt.
  const prompt = buildCyclePrompt(state, signals, {
    defaultChannel: autonomousCfg?.defaultChannel,
    defaultTarget: autonomousCfg?.defaultChannelTarget,
    dryRun: autonomousCfg?.dryRun,
  });

  // Build a synthetic cron job for the isolated agent runner.
  const syntheticJob = buildSyntheticCronJob(autonomousCfg, agentId);

  log.info(`autonomous cycle #${state.cycleCount + 1} starting (${signals.length} signals)`);

  let outputText: string | undefined;
  let error: string | undefined;
  let finishedAt = 0;

  try {
    const result = await runCronIsolatedAgentTurn({
      cfg,
      deps,
      job: syntheticJob,
      message: prompt,
      abortSignal,
      sessionKey: `agent:${agentId}:autonomous/cycle`,
      agentId,
      lane: "autonomous",
    });
    outputText = result.outputText;
  } catch (err) {
    // Classify the error type for better diagnostics.
    if (abortSignal?.aborted) {
      error = "aborted";
      log.info("autonomous cycle aborted by signal");
    } else if (err instanceof Error && err.name === "TimeoutError") {
      error = `timeout: ${err.message}`;
      log.error(`autonomous cycle timed out: ${err.message}`);
    } else if (err instanceof DOMException && err.name === "AbortError") {
      error = "aborted";
      log.info("autonomous cycle aborted (AbortError)");
    } else {
      error = err instanceof Error ? err.message : String(err);
      log.error(`autonomous cycle error: ${error}`);
    }
  } finally {
    // Always persist state, even if the agent turn threw — prevents lost cycle tracking.
    finishedAt = Date.now();
    state.lastCycleAt = finishedAt;
    state.cycleCount += 1;

    // Set next cycle time (default interval unless the agent requested something else).
    // Validate the interval to guard against NaN/Infinity from config.
    const rawInterval = autonomousCfg?.cycleIntervalMs ?? DEFAULT_CYCLE_INTERVAL_MS;
    const defaultInterval = clampDelay(rawInterval, 1000, MAX_TIMEOUT_MS);
    if (!state.nextCycleAt || state.nextCycleAt <= finishedAt) {
      state.nextCycleAt = finishedAt + defaultInterval;
    }

    // Detect ignored signals: re-load state (agent may have changed it via tool calls)
    // and compare with pre-cycle snapshot to re-escalate unaddressed items.
    try {
      const postCycleState = await loadAutonomousState(storePath);
      attention.reEscalateIgnoredSignals(preCycleSnapshot, postCycleState);
      // Merge any state changes the agent made back into our state object.
      state.goals = postCycleState.goals;
      state.observations = postCycleState.observations;
      state.plans = postCycleState.plans;
      state.socialContext = postCycleState.socialContext;
    } catch {
      // Non-fatal: if re-load fails, just skip re-escalation.
    }

    // Drain unconsumed signals back to state so they survive restarts.
    attention.drainToState(state);

    // Persist cycle outcome so the agent can review previous results.
    const outcome: CycleOutcome = {
      cycleNumber: state.cycleCount,
      startedAt,
      finishedAt,
      actionsTaken: outputText ? ["agent-turn"] : [],
      error,
    };
    state.lastCycleOutcome = outcome;

    try {
      await saveAutonomousState(state, storePath);
    } catch (saveErr) {
      const saveMsg = saveErr instanceof Error ? saveErr.message : String(saveErr);
      log.error(`failed to save autonomous state: ${saveMsg}`);
    }
  }

  const outcome: CycleOutcome = {
    cycleNumber: state.cycleCount,
    startedAt,
    finishedAt,
    actionsTaken: outputText ? ["agent-turn"] : [],
    error,
  };

  log.info(
    `autonomous cycle #${state.cycleCount} completed in ${finishedAt - startedAt}ms` +
      (error ? ` (error: ${error})` : ""),
  );

  return outcome;
}

function isRateLimited(state: AutonomousState, maxPerHour: number): boolean {
  // safeMaxPerHour guarantees maxPerHour >= 1, but guard defensively anyway.
  if (!isFinitePositive(maxPerHour)) {
    return true;
  }
  if (!isFinitePositive(state.lastCycleAt)) {
    return false;
  }
  const minInterval = 3_600_000 / maxPerHour;
  const elapsed = Date.now() - state.lastCycleAt;
  if (!Number.isFinite(elapsed)) {
    return false;
  }
  return elapsed < minInterval;
}

function buildSyntheticCronJob(
  autonomousCfg: AutonomousConfig | undefined,
  agentId: string,
): CronJob {
  // Validate interval for the schedule field.
  const rawInterval = autonomousCfg?.cycleIntervalMs ?? DEFAULT_CYCLE_INTERVAL_MS;
  const safeInterval = clampDelay(rawInterval, 1000, MAX_TIMEOUT_MS);

  // Validate timeout; fall back to default if invalid.
  const rawTimeout = autonomousCfg?.timeoutSeconds;
  const safeTimeout = isFinitePositive(rawTimeout) ? rawTimeout : DEFAULT_TIMEOUT_SECONDS;

  const now = Date.now();
  return {
    id: "autonomous-cycle",
    name: "Autonomous Cycle",
    enabled: true,
    agentId: agentId || "autonomous",
    schedule: {
      kind: "every",
      everyMs: safeInterval,
    },
    sessionTarget: "isolated",
    wakeMode: "now",
    payload: {
      kind: "agentTurn",
      message: "", // Filled by caller.
      model: autonomousCfg?.model ?? undefined,
      thinking: autonomousCfg?.thinking ?? undefined,
      timeoutSeconds: safeTimeout,
    },
    delivery: { mode: "none" },
    failureAlert: false,
    createdAtMs: now,
    updatedAtMs: now,
    state: {},
  };
}

/** Resolve the interval until the next cycle should run (in ms). */
export function resolveNextCycleDelay(state: AutonomousState, cfg?: AutonomousConfig): number {
  const rawInterval = cfg?.cycleIntervalMs ?? DEFAULT_CYCLE_INTERVAL_MS;
  const defaultInterval = clampDelay(rawInterval, 1000, MAX_TIMEOUT_MS);
  if (!state.nextCycleAt || !isFinitePositive(state.nextCycleAt)) {
    return defaultInterval;
  }
  const remaining = state.nextCycleAt - Date.now();
  // Clamp the return value to a safe setTimeout range.
  return clampDelay(Math.max(0, remaining), 0, MAX_TIMEOUT_MS);
}

/** Shallow-copy state arrays for pre/post cycle comparison. */
function snapshotStateForComparison(state: AutonomousState): AutonomousState {
  return {
    ...state,
    goals: state.goals.map((g) => ({ ...g })),
    observations: state.observations.map((o) => ({ ...o })),
    plans: state.plans.map((p) => ({ ...p, steps: [...p.steps] })),
    socialContext: state.socialContext.map((s) => ({ ...s })),
    pendingSignals: [...state.pendingSignals],
  };
}
