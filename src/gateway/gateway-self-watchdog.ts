import { scheduleGatewaySigusr1Restart } from "../infra/restart.js";
import { createSubsystemLogger } from "../logging/subsystem.js";

const log = createSubsystemLogger("gateway/self-watchdog");

const DEFAULT_CHECK_INTERVAL_MS = 2 * 60_000; // 2 minutes
const DEFAULT_STARTUP_GRACE_MS = 3 * 60_000; // 3 minutes grace for initial channel connect
const DEFAULT_STALE_THRESHOLD_MS = 30 * 60_000; // 30 minutes without any activity
const DEFAULT_MAX_AUTO_RESTARTS = 3;
const AUTO_RESTART_WINDOW_MS = 60 * 60_000; // 1 hour

export type GatewaySelfWatchdogDeps = {
  /** Returns true when the gateway server is listening and able to accept connections. */
  isServerListening: () => boolean;
  /** Returns total count of active channels that should be connected. */
  getExpectedChannelCount: () => number;
  /** Returns count of channels currently connected/running. */
  getConnectedChannelCount: () => number;
  /** Returns the timestamp of the last successfully processed inbound or outbound event. */
  getLastActivityAt: () => number;
  checkIntervalMs?: number;
  /** Grace period after watchdog starts before health checks activate. */
  startupGraceMs?: number;
  staleThresholdMs?: number;
  maxAutoRestarts?: number;
  abortSignal?: AbortSignal;
};

export type GatewaySelfWatchdog = {
  stop: () => void;
  /** Record activity to prevent stale-gateway restarts. */
  touch: () => void;
};

/**
 * Periodically verifies the gateway itself is healthy (server listening,
 * channels connected, processing events). If the gateway appears stuck,
 * triggers a SIGUSR1 restart to self-heal.
 */
export function startGatewaySelfWatchdog(deps: GatewaySelfWatchdogDeps): GatewaySelfWatchdog {
  const {
    isServerListening,
    getExpectedChannelCount,
    getConnectedChannelCount,
    getLastActivityAt,
    checkIntervalMs = DEFAULT_CHECK_INTERVAL_MS,
    startupGraceMs = DEFAULT_STARTUP_GRACE_MS,
    staleThresholdMs = DEFAULT_STALE_THRESHOLD_MS,
    maxAutoRestarts = DEFAULT_MAX_AUTO_RESTARTS,
    abortSignal,
  } = deps;

  let stopped = false;
  let timer: ReturnType<typeof setInterval> | null = null;
  let lastTouchAt = Date.now();
  const createdAt = Date.now();
  const autoRestartTimestamps: number[] = [];

  function touch() {
    lastTouchAt = Date.now();
  }

  function pruneOldRestarts(now: number) {
    while (
      autoRestartTimestamps.length > 0 &&
      now - autoRestartTimestamps[0] > AUTO_RESTART_WINDOW_MS
    ) {
      autoRestartTimestamps.shift();
    }
  }

  function requestSelfRestart(reason: string): void {
    const now = Date.now();
    pruneOldRestarts(now);
    if (autoRestartTimestamps.length >= maxAutoRestarts) {
      log.error(
        `self-watchdog: would restart (${reason}) but already hit ${maxAutoRestarts} auto-restarts this hour; skipping`,
      );
      return;
    }
    autoRestartTimestamps.push(now);
    log.warn(`self-watchdog: triggering restart — ${reason}`);
    scheduleGatewaySigusr1Restart({ delayMs: 0, reason: `self-watchdog: ${reason}` });
  }

  function runCheck() {
    if (stopped) {
      return;
    }
    const now = Date.now();

    // Skip checks during startup grace period so channels have time to connect.
    if (now - createdAt < startupGraceMs) {
      return;
    }

    // Check 1: server not listening
    if (!isServerListening()) {
      requestSelfRestart("server is not listening");
      return;
    }

    // Check 2: all expected channels disconnected
    const expected = getExpectedChannelCount();
    const connected = getConnectedChannelCount();
    if (expected > 0 && connected === 0) {
      requestSelfRestart(`0/${expected} channels connected`);
      return;
    }

    // Check 3: no activity for a long time (gateway might be stuck).
    // Skip when no channels are configured (API-only / headless mode) since
    // there is no expected inbound traffic and the gateway is legitimately idle.
    if (expected === 0) {
      return;
    }
    const lastActivity = Math.max(getLastActivityAt(), lastTouchAt);
    const idleMs = now - lastActivity;
    if (idleMs > staleThresholdMs) {
      requestSelfRestart(
        `no activity for ${Math.round(idleMs / 60_000)}m (threshold: ${Math.round(staleThresholdMs / 60_000)}m)`,
      );
      return;
    }
  }

  function stop() {
    if (stopped) {
      return;
    }
    stopped = true;
    if (timer) {
      clearInterval(timer);
      timer = null;
    }
  }

  if (abortSignal?.aborted) {
    stopped = true;
  } else {
    abortSignal?.addEventListener("abort", stop, { once: true });
    // Wait 2 minutes before first check to allow startup to complete.
    timer = setInterval(() => runCheck(), checkIntervalMs);
    if (typeof timer === "object" && "unref" in timer) {
      timer.unref();
    }
    log.info(
      `started (interval: ${Math.round(checkIntervalMs / 1000)}s, stale-threshold: ${Math.round(staleThresholdMs / 60_000)}m)`,
    );
  }

  return { stop, touch };
}
