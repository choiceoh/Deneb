import { resolveDefaultAgentId } from "../agents/agent-scope.js";
import type { CliDeps } from "../cli/outbound-send-deps.js";
import type { DenebConfig } from "../config/config.js";
import { createSubsystemLogger } from "../logging/subsystem.js";
import { AttentionManager } from "./attention.js";
import { resolveNextCycleDelay, runAutonomousCycle } from "./cycle-runner.js";
import { loadAutonomousState, saveAutonomousState, addGoal } from "./state-store.js";
import type { AutonomousConfig } from "./types.js";

const log = createSubsystemLogger("autonomous");

const DEFAULT_CYCLE_INTERVAL_MS = 300_000;

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
  const attention = new AttentionManager();
  const abortController = new AbortController();

  log.info(`autonomous service starting (agent=${agentId})`);

  // Seed initial goals from config if state is fresh.
  await seedInitialGoals(autonomousCfg, storePath);

  // Start the cycle loop.
  let timer: NodeJS.Timeout | null = null;
  let running = false;

  async function executeCycle(): Promise<void> {
    if (running || abortController.signal.aborted) {
      return;
    }
    running = true;
    try {
      await runAutonomousCycle({
        cfg,
        deps,
        attention,
        agentId,
        storePath,
        abortSignal: abortController.signal,
      });
    } catch (err) {
      log.error(`autonomous cycle failed: ${err instanceof Error ? err.message : String(err)}`);
    } finally {
      running = false;
      scheduleNext();
    }
  }

  function scheduleNext(): void {
    if (abortController.signal.aborted) {
      return;
    }
    // Read state to determine next cycle timing.
    loadAutonomousState(storePath)
      .then((state) => {
        const delay = resolveNextCycleDelay(state, autonomousCfg);
        const clampedDelay = Math.max(10_000, delay); // Minimum 10s between cycles.
        log.debug(`next autonomous cycle in ${Math.round(clampedDelay / 1000)}s`);
        timer = setTimeout(() => {
          timer = null;
          void executeCycle();
        }, clampedDelay);
        timer.unref?.();
      })
      .catch((err) => {
        log.error(`failed to schedule next cycle: ${String(err)}`);
        // Fallback: retry in default interval.
        const fallback = autonomousCfg?.cycleIntervalMs ?? DEFAULT_CYCLE_INTERVAL_MS;
        timer = setTimeout(() => {
          timer = null;
          void executeCycle();
        }, fallback);
        timer.unref?.();
      });
  }

  // Start first cycle after a short delay to let gateway finish booting.
  timer = setTimeout(() => {
    timer = null;
    void executeCycle();
  }, 5_000);
  timer.unref?.();

  return {
    stop() {
      abortController.abort("autonomous service stopping");
      if (timer) {
        clearTimeout(timer);
        timer = null;
      }
      log.info("autonomous service stopped");
    },
    triggerNow() {
      if (running) {
        log.debug("autonomous cycle already running, skipping trigger");
        return;
      }
      if (timer) {
        clearTimeout(timer);
        timer = null;
      }
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
  } // Already seeded.

  for (const goalDesc of cfg.goals) {
    addGoal(state, goalDesc, "medium");
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
