import type { CliDeps } from "../cli/outbound-send-deps.js";
import type { DenebConfig } from "../config/config.js";
import { runCronIsolatedAgentTurn } from "../cron/isolated-agent/run.js";
import type { CronJob } from "../cron/types.js";
import { createSubsystemLogger } from "../logging/subsystem.js";
import { AttentionManager } from "./attention.js";
import { buildCyclePrompt } from "./prompt.js";
import { loadAutonomousState, saveAutonomousState } from "./state-store.js";
import type { AutonomousConfig, AutonomousState, CycleOutcome } from "./types.js";

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
  const autonomousCfg = cfg.autonomous;
  const startedAt = Date.now();

  // Load state.
  const state = await loadAutonomousState(storePath);

  // Rate limit check.
  const maxPerHour = autonomousCfg?.maxCyclesPerHour ?? DEFAULT_MAX_CYCLES_PER_HOUR;
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

  // Derive state-based attention signals.
  attention.deriveSignalsFromState(state);

  // Get top signals for the prompt.
  const signals = attention.getTopSignals(10);

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

  try {
    const result = await runCronIsolatedAgentTurn({
      cfg,
      deps,
      job: syntheticJob,
      message: prompt,
      abortSignal,
      sessionKey: `agent:${agentId}@autonomous/cycle`,
      agentId,
      lane: "autonomous",
    });
    outputText = result.outputText;
  } catch (err) {
    error = err instanceof Error ? err.message : String(err);
    log.error(`autonomous cycle error: ${error}`);
  }

  // Update state.
  const finishedAt = Date.now();
  state.lastCycleAt = finishedAt;
  state.cycleCount += 1;

  // Set next cycle time (default interval unless the agent requested something else).
  const defaultInterval = autonomousCfg?.cycleIntervalMs ?? DEFAULT_CYCLE_INTERVAL_MS;
  if (!state.nextCycleAt || state.nextCycleAt <= finishedAt) {
    state.nextCycleAt = finishedAt + defaultInterval;
  }

  // Persist state.
  await saveAutonomousState(state, storePath);

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
  if (maxPerHour <= 0) {
    return true;
  }
  if (state.lastCycleAt === 0) {
    return false;
  }
  const minInterval = 3_600_000 / maxPerHour;
  return Date.now() - state.lastCycleAt < minInterval;
}

function buildSyntheticCronJob(
  autonomousCfg: AutonomousConfig | undefined,
  agentId: string,
): CronJob {
  return {
    id: "autonomous-cycle",
    name: "Autonomous Cycle",
    enabled: true,
    agentId,
    schedule: {
      kind: "every",
      everyMs: autonomousCfg?.cycleIntervalMs ?? DEFAULT_CYCLE_INTERVAL_MS,
    },
    sessionTarget: "isolated",
    wakeMode: "now",
    payload: {
      kind: "agentTurn",
      message: "", // Filled by caller.
      model: autonomousCfg?.model,
      thinking: autonomousCfg?.thinking,
      timeoutSeconds: DEFAULT_TIMEOUT_SECONDS,
    },
    delivery: { mode: "none" },
    failureAlert: false,
    createdAtMs: Date.now(),
    updatedAtMs: Date.now(),
    state: {},
  };
}

/** Resolve the interval until the next cycle should run (in ms). */
export function resolveNextCycleDelay(state: AutonomousState, cfg?: AutonomousConfig): number {
  const defaultInterval = cfg?.cycleIntervalMs ?? DEFAULT_CYCLE_INTERVAL_MS;
  if (!state.nextCycleAt || state.nextCycleAt <= 0) {
    return defaultInterval;
  }
  const remaining = state.nextCycleAt - Date.now();
  return Math.max(0, remaining);
}
