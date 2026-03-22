import { resolveDefaultAgentId } from "../agents/agent-scope.js";
import type { CliDeps } from "../cli/outbound-send-deps.js";
import type { DenebConfig } from "../config/config.js";
import { createSubsystemLogger } from "../logging/subsystem.js";
import { AttentionManager } from "./attention.js";
import { resolveNextCycleDelay, runAutonomousCycle } from "./cycle-runner.js";
import { addGoal, loadAutonomousState, saveAutonomousState } from "./state-store.js";
import type { AutonomousConfig } from "./types.js";
import { clampDelay, MAX_TIMEOUT_MS } from "./validation.js";

const log = createSubsystemLogger("autonomous");

const DEFAULT_CYCLE_INTERVAL_MS = 300_000;
const MIN_CYCLE_DELAY_MS = 10_000;
const STARTUP_DELAY_MS = 5_000;
const MAX_CONSECUTIVE_FAILURES = 10;
const INITIAL_BACKOFF_MS = 30_000; // 30s initial backoff after failure
const MAX_BACKOFF_MS = 600_000; // 10 minute max backoff

export type AutonomousServiceHandle = {
  stop: () => void;
  /** Trigger an immediate cycle. */
  triggerNow: () => void;
  /** Get current attention manager (for feeding external signals). */
  attention: AttentionManager;
};

/**
 * Start the autonomous loop service. This runs as a background process
 * within the gateway, scheduling and executing decision cycles.
 */
export async function startAutonomousService(params: {
  cfg: DenebConfig;
  deps: CliDeps;
  storePath?: string;
}): Promise<AutonomousServiceHandle> {
  const { cfg, deps, storePath } = params;
  const autonomousCfg = cfg.autonomous;

  if (!autonomousCfg?.enabled) {
    log.info("autonomous service disabled");
    return createNoopHandle();
  }

  const agentId = autonomousCfg.agentId ?? resolveDefaultAgentId(cfg);
  if (!agentId) {
    log.error("autonomous service cannot start: no agent ID configured or available");
    return createNoopHandle();
  }

  const attention = new AttentionManager();
  const abortController = new AbortController();
  let stopped = false;

  log.info(`autonomous service starting (agent=${agentId})`);

  // Seed initial goals from config if state is fresh.
  try {
    await seedInitialGoals(autonomousCfg, storePath);
  } catch (err) {
    log.error(`failed to seed initial goals: ${err instanceof Error ? err.message : String(err)}`);
    // Non-fatal: continue without initial goals.
  }

  // Start the cycle loop.
  let timer: NodeJS.Timeout | null = null;
  let running = false;
  let consecutiveFailures = 0;

  function clearTimer(): void {
    if (timer) {
      clearTimeout(timer);
      timer = null;
    }
  }

  function setTimer(delayMs: number, callback: () => void): void {
    clearTimer();
    const clamped = clampDelay(delayMs, MIN_CYCLE_DELAY_MS, MAX_TIMEOUT_MS);
    timer = setTimeout(() => {
      timer = null;
      callback();
    }, clamped);
    timer.unref?.();
  }

  async function executeCycle(): Promise<void> {
    // Double-check both flags atomically.
    if (running || stopped || abortController.signal.aborted) {
      return;
    }
    running = true;
    try {
      const outcome = await runAutonomousCycle({
        cfg,
        deps,
        attention,
        agentId,
        storePath,
        abortSignal: abortController.signal,
      });
      if (outcome.error) {
        consecutiveFailures++;
        log.warn(
          `autonomous cycle error (${consecutiveFailures} consecutive failures): ${outcome.error}`,
        );
      } else {
        if (consecutiveFailures > 0) {
          log.info(`autonomous cycle recovered after ${consecutiveFailures} consecutive failures`);
        }
        consecutiveFailures = 0;
      }
    } catch (err) {
      consecutiveFailures++;
      log.error(
        `autonomous cycle failed (${consecutiveFailures} consecutive): ${err instanceof Error ? err.message : String(err)}`,
      );
    } finally {
      running = false;
      // Only schedule next if we haven't been stopped.
      if (!stopped && !abortController.signal.aborted) {
        scheduleNext();
      }
    }
  }

  function scheduleNext(): void {
    if (stopped || abortController.signal.aborted) {
      return;
    }

    // Apply exponential backoff when consecutive failures occur,
    // so we don't hammer a failing LLM/service and allow time to recover.
    if (consecutiveFailures > 0) {
      const backoffMs = Math.min(
        INITIAL_BACKOFF_MS *
          Math.pow(2, Math.min(consecutiveFailures - 1, MAX_CONSECUTIVE_FAILURES)),
        MAX_BACKOFF_MS,
      );
      log.info(
        `scheduling retry after ${Math.round(backoffMs / 1000)}s backoff (${consecutiveFailures} failures)`,
      );
      setTimer(backoffMs, () => {
        void executeCycle();
      });
      return;
    }

    loadAutonomousState(storePath)
      .then((state) => {
        // Re-check after async load.
        if (stopped || abortController.signal.aborted) {
          return;
        }
        const delay = resolveNextCycleDelay(state, autonomousCfg);
        const clampedDelay = Math.max(MIN_CYCLE_DELAY_MS, delay);
        log.debug(`next autonomous cycle in ${Math.round(clampedDelay / 1000)}s`);
        setTimer(clampedDelay, () => {
          void executeCycle();
        });
      })
      .catch((err) => {
        if (stopped || abortController.signal.aborted) {
          return;
        }
        log.error(`failed to schedule next cycle: ${String(err)}`);
        // Fallback: retry in default interval, clamped for safety.
        const rawFallback = autonomousCfg?.cycleIntervalMs ?? DEFAULT_CYCLE_INTERVAL_MS;
        const fallback = clampDelay(rawFallback, MIN_CYCLE_DELAY_MS, MAX_TIMEOUT_MS);
        setTimer(fallback, () => {
          void executeCycle();
        });
      });
  }

  // Start first cycle after a short delay to let gateway finish booting.
  setTimer(STARTUP_DELAY_MS, () => {
    void executeCycle();
  });

  return {
    stop() {
      if (stopped) {
        return;
      }
      stopped = true;
      clearTimer();
      abortController.abort("autonomous service stopping");
      log.info("autonomous service stopped");
    },
    triggerNow() {
      if (stopped || abortController.signal.aborted) {
        return;
      }
      if (running) {
        log.debug("autonomous cycle already running, skipping trigger");
        return;
      }
      clearTimer();
      void executeCycle();
    },
    attention,
  };
}

async function seedInitialGoals(
  cfg: AutonomousConfig | undefined,
  storePath?: string,
): Promise<void> {
  if (!cfg?.goals || cfg.goals.length === 0) {
    return;
  }

  const state = await loadAutonomousState(storePath);
  if (state.goals.length > 0) {
    return;
  }

  for (const goalDesc of cfg.goals) {
    if (typeof goalDesc === "string" && goalDesc.trim().length > 0) {
      addGoal(state, goalDesc.trim(), "medium");
    }
  }
  await saveAutonomousState(state, storePath);
  log.info(`seeded ${cfg.goals.length} initial goals from config`);
}

function createNoopHandle(): AutonomousServiceHandle {
  return {
    stop() {},
    triggerNow() {},
    attention: new AttentionManager(),
  };
}
