import {
  abortEmbeddedPiRun,
  getActiveEmbeddedRunCount,
  resetActiveEmbeddedRunState,
  waitForActiveEmbeddedRuns,
} from "../../agents/pi-embedded-runner/runs.js";
import { resetFollowupQueueDrainState } from "../../auto-reply/reply/queue/drain.js";
import type { startGatewayServer } from "../../gateway/server.js";
import { acquireGatewayLock } from "../../infra/gateway-lock.js";
import { restartGatewayProcessWithFreshPid } from "../../infra/process-respawn.js";
import {
  consumeGatewaySigusr1RestartAuthorization,
  isGatewaySigusr1RestartExternallyAllowed,
  markGatewaySigusr1RestartHandled,
  scheduleGatewaySigusr1Restart,
} from "../../infra/restart.js";
import { createSubsystemLogger } from "../../logging/subsystem.js";
import {
  getActiveTaskCount,
  markGatewayDraining,
  resetAllLanes,
  waitForActiveTasks,
} from "../../process/command-queue.js";
import { createRestartIterationHook } from "../../process/restart-recovery.js";
import type { defaultRuntime } from "../../runtime.js";

const gatewayLog = createSubsystemLogger("gateway");

type GatewayRunSignalAction = "stop" | "restart";

export async function runGatewayLoop(params: {
  start: () => Promise<Awaited<ReturnType<typeof startGatewayServer>>>;
  runtime: typeof defaultRuntime;
  lockPort?: number;
}) {
  let lock = await acquireGatewayLock({ port: params.lockPort });
  let server: Awaited<ReturnType<typeof startGatewayServer>> | null = null;
  let shuttingDown = false;
  let restartResolver: (() => void) | null = null;

  const cleanupSignals = () => {
    process.removeListener("SIGTERM", onSigterm);
    process.removeListener("SIGINT", onSigint);
    process.removeListener("SIGUSR1", onSigusr1);
  };
  const exitProcess = (code: number) => {
    cleanupSignals();
    params.runtime.exit(code);
  };
  const releaseLockIfHeld = async (): Promise<boolean> => {
    if (!lock) {
      return false;
    }
    await lock.release();
    lock = null;
    return true;
  };
  const reacquireLockForInProcessRestart = async (): Promise<boolean> => {
    try {
      lock = await acquireGatewayLock({ port: params.lockPort });
      return true;
    } catch (err) {
      gatewayLog.error(`failed to reacquire gateway lock for in-process restart: ${String(err)}`);
      exitProcess(1);
      return false;
    }
  };
  const handleRestartAfterServerClose = async () => {
    const hadLock = await releaseLockIfHeld();
    // Release the lock BEFORE spawning so the child can acquire it immediately.
    const respawn = restartGatewayProcessWithFreshPid();
    if (respawn.mode === "spawned" || respawn.mode === "supervised") {
      const modeLabel =
        respawn.mode === "spawned"
          ? `spawned pid ${respawn.pid ?? "unknown"}`
          : "supervisor restart";
      gatewayLog.info(`restart mode: full process restart (${modeLabel})`);
      exitProcess(0);
      return;
    }
    if (respawn.mode === "failed") {
      gatewayLog.warn(
        `full process restart failed (${respawn.detail ?? "unknown error"}); falling back to in-process restart`,
      );
    } else {
      gatewayLog.info(`restart mode: in-process restart (${respawn.detail ?? "DENEB_NO_RESPAWN"})`);
    }
    if (hadLock && !(await reacquireLockForInProcessRestart())) {
      return;
    }
    shuttingDown = false;
    restartResolver?.();
  };
  const handleStopAfterServerClose = async () => {
    await releaseLockIfHeld();
    exitProcess(0);
  };

  const DRAIN_TIMEOUT_MS = 90_000;
  const SHUTDOWN_TIMEOUT_MS = 5_000;

  // Auto-retry startup on non-initial failures with exponential backoff.
  const MAX_STARTUP_RETRIES = 5;
  const INITIAL_RETRY_DELAY_MS = 5_000;
  const MAX_RETRY_DELAY_MS = 120_000;

  const request = (action: GatewayRunSignalAction, signal: string) => {
    if (shuttingDown) {
      gatewayLog.info(`received ${signal} during shutdown; ignoring`);
      return;
    }
    shuttingDown = true;
    const isRestart = action === "restart";
    gatewayLog.info(`received ${signal}; ${isRestart ? "restarting" : "shutting down"}`);

    // Allow extra time for draining active turns on restart.
    const forceExitMs = isRestart ? DRAIN_TIMEOUT_MS + SHUTDOWN_TIMEOUT_MS : SHUTDOWN_TIMEOUT_MS;
    const forceExitTimer = setTimeout(() => {
      gatewayLog.error("shutdown timed out; exiting without full cleanup");
      // Exit non-zero on restart timeout so launchd/systemd treats it as a
      // failure and triggers a clean process restart instead of assuming the
      // shutdown was intentional. Stop-timeout stays at 0 (graceful). (#36822)
      exitProcess(isRestart ? 1 : 0);
    }, forceExitMs);

    void (async () => {
      try {
        // On restart, wait for in-flight agent turns to finish before
        // tearing down the server so buffered messages are delivered.
        if (isRestart) {
          // Reject new enqueues immediately during the drain window so
          // sessions get an explicit restart error instead of silent task loss.
          markGatewayDraining();
          const activeTasks = getActiveTaskCount();
          const activeRuns = getActiveEmbeddedRunCount();

          // Best-effort abort for compacting runs so long compaction operations
          // don't hold session write locks across restart boundaries.
          if (activeRuns > 0) {
            abortEmbeddedPiRun(undefined, { mode: "compacting" });
          }

          if (activeTasks > 0 || activeRuns > 0) {
            gatewayLog.info(
              `draining ${activeTasks} active task(s) and ${activeRuns} active embedded run(s) before restart (timeout ${DRAIN_TIMEOUT_MS}ms)`,
            );
            const [tasksDrain, runsDrain] = await Promise.all([
              activeTasks > 0
                ? waitForActiveTasks(DRAIN_TIMEOUT_MS)
                : Promise.resolve({ drained: true }),
              activeRuns > 0
                ? waitForActiveEmbeddedRuns(DRAIN_TIMEOUT_MS)
                : Promise.resolve({ drained: true }),
            ]);
            if (tasksDrain.drained && runsDrain.drained) {
              gatewayLog.info("all active work drained");
            } else {
              gatewayLog.warn("drain timeout reached; proceeding with restart");
              // Final best-effort abort to avoid carrying active runs into the
              // next lifecycle when drain time budget is exhausted.
              abortEmbeddedPiRun(undefined, { mode: "all" });
            }
          }
        }

        await server?.close({
          reason: isRestart ? "gateway restarting" : "gateway stopping",
          restartExpectedMs: isRestart ? 1500 : null,
        });
      } catch (err) {
        gatewayLog.error(`shutdown error: ${String(err)}`);
      } finally {
        clearTimeout(forceExitTimer);
        server = null;
        if (isRestart) {
          await handleRestartAfterServerClose();
        } else {
          await handleStopAfterServerClose();
        }
      }
    })();
  };

  const onSigterm = () => {
    gatewayLog.info("signal SIGTERM received");
    request("stop", "SIGTERM");
  };
  const onSigint = () => {
    gatewayLog.info("signal SIGINT received");
    request("stop", "SIGINT");
  };
  const onSigusr1 = () => {
    gatewayLog.info("signal SIGUSR1 received");
    const authorized = consumeGatewaySigusr1RestartAuthorization();
    if (!authorized) {
      if (!isGatewaySigusr1RestartExternallyAllowed()) {
        gatewayLog.warn(
          "SIGUSR1 restart ignored (not authorized; commands.restart=false or use gateway tool).",
        );
        return;
      }
      if (shuttingDown) {
        gatewayLog.info("received SIGUSR1 during shutdown; ignoring");
        return;
      }
      // External SIGUSR1 requests should still reuse the in-process restart
      // scheduler so idle drain and restart coalescing stay consistent.
      scheduleGatewaySigusr1Restart({ delayMs: 0, reason: "SIGUSR1" });
      return;
    }
    markGatewaySigusr1RestartHandled();
    request("restart", "SIGUSR1");
  };

  process.on("SIGTERM", onSigterm);
  process.on("SIGINT", onSigint);
  process.on("SIGUSR1", onSigusr1);

  try {
    const onIteration = createRestartIterationHook(() => {
      // After an in-process restart (SIGUSR1), reset command-queue lane state.
      // Interrupted tasks from the previous lifecycle may have left `active`
      // counts elevated (their finally blocks never ran), permanently blocking
      // new work from draining. This must happen here — at the restart
      // coordinator level — rather than inside individual subsystem init
      // functions, to avoid surprising cross-cutting side effects.
      resetAllLanes();
      resetActiveEmbeddedRunState();
      resetFollowupQueueDrainState();
    });

    // Keep process alive; SIGUSR1 triggers an in-process restart (no supervisor required).
    // SIGTERM/SIGINT still exit after a graceful shutdown.
    let isFirstStart = true;
    // eslint-disable-next-line no-constant-condition
    while (true) {
      onIteration();
      let startupRetries = 0;
      // Inner retry loop: on non-initial startup failures, retry with backoff
      // instead of sitting idle forever waiting for a manual SIGUSR1.
      // eslint-disable-next-line no-constant-condition
      while (true) {
        // If a shutdown signal arrived during retry sleep, stop retrying.
        if (shuttingDown) {
          break;
        }
        try {
          server = await params.start();
          isFirstStart = false;
          break; // success — proceed to wait for restart signal
        } catch (err) {
          // On initial startup, let the error propagate so the outer handler
          // can report "Gateway failed to start" and exit non-zero. Only
          // swallow errors on subsequent in-process restarts to keep the
          // process alive (a crash would lose macOS TCC permissions). (#35862)
          if (isFirstStart) {
            throw err;
          }
          server = null;
          startupRetries++;
          const errMsg = err instanceof Error ? err.message : String(err);
          const errStack = err instanceof Error && err.stack ? `\n${err.stack}` : "";

          if (startupRetries > MAX_STARTUP_RETRIES) {
            // Release the gateway lock so that `daemon restart/stop` (which
            // discovers PIDs via the gateway port) can still manage the process.
            // Without this, the process holds the lock but is not listening,
            // forcing manual cleanup. (#35862)
            await releaseLockIfHeld();
            gatewayLog.error(
              `gateway startup failed after ${MAX_STARTUP_RETRIES} retries: ${errMsg}. ` +
                `Process will stay alive; fix the issue and restart.${errStack}`,
            );
            break; // fall through to wait for manual restart signal
          }

          const retryDelay = Math.min(
            INITIAL_RETRY_DELAY_MS * Math.pow(2, startupRetries - 1),
            MAX_RETRY_DELAY_MS,
          );
          gatewayLog.warn(
            `gateway startup failed (attempt ${startupRetries}/${MAX_STARTUP_RETRIES}): ${errMsg}. ` +
              `Retrying in ${Math.round(retryDelay / 1000)}s...${errStack}`,
          );
          // Cancellable sleep: resolve immediately if shutdown signal arrives
          // so we don't delay SIGTERM/SIGINT response by up to 120 seconds.
          await new Promise<void>((resolve) => {
            const cleanup = () => {
              clearTimeout(retryTimer);
              process.removeListener("SIGTERM", onSignal);
              process.removeListener("SIGINT", onSignal);
              resolve();
            };
            const onSignal = () => cleanup();
            const retryTimer = setTimeout(() => cleanup(), retryDelay);
            if (typeof retryTimer === "object" && "unref" in retryTimer) {
              retryTimer.unref();
            }
            process.once("SIGTERM", onSignal);
            process.once("SIGINT", onSignal);
          });
        }
      }
      await new Promise<void>((resolve) => {
        restartResolver = resolve;
      });
    }
  } finally {
    await releaseLockIfHeld();
    cleanupSignals();
  }
}
