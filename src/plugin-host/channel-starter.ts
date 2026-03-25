// Channel starter for the Plugin Host.
//
// Loads all configured channel plugins from the registry and starts them.
// Each channel's gateway.startAccount() hook runs as a long-lived async task
// with exponential backoff auto-restart on failure.
//
// This module is called by the "plugin-host.channels.start-all" RPC method
// after the Go gateway signals that the bridge is ready.

import { listChannelPlugins } from "../channels/plugins/index.js";
import type { ChannelAccountSnapshot } from "../channels/plugins/types.js";
import { loadConfig } from "../config/config.js";
import { computeBackoff, sleepWithAbort, type BackoffPolicy } from "../infra/backoff.js";
import { createSubsystemLogger } from "../logging/subsystem.js";
import type { RuntimeEnv } from "../runtime.js";

const log = createSubsystemLogger("plugin-host");

const RESTART_POLICY: BackoffPolicy = {
  initialMs: 5_000,
  maxMs: 5 * 60_000,
  factor: 2,
  jitter: 0.1,
};
const MAX_RESTART_ATTEMPTS = 10;

// Track running channel tasks so we can stop them on shutdown.
const runningTasks = new Map<string, { abort: AbortController; promise: Promise<void> }>();
const accountRuntimes = new Map<string, ChannelAccountSnapshot>();

function runtimeKey(channelId: string, accountId: string): string {
  return `${channelId}:${accountId}`;
}

function getRuntime(channelId: string, accountId: string): ChannelAccountSnapshot {
  return accountRuntimes.get(runtimeKey(channelId, accountId)) ?? { accountId };
}

function setRuntime(
  channelId: string,
  accountId: string,
  patch: Partial<ChannelAccountSnapshot>,
): void {
  const key = runtimeKey(channelId, accountId);
  const current = accountRuntimes.get(key) ?? { accountId };
  accountRuntimes.set(key, { ...current, ...patch, accountId });
}

function createChannelRuntimeEnv(channelId: string): RuntimeEnv {
  return {
    log: (...args: unknown[]) => console.log(`[${channelId}]`, ...args),
    error: (...args: unknown[]) => console.error(`[${channelId}]`, ...args),
    exit: () => {
      // Plugin host should not exit on channel errors.
    },
  };
}

/**
 * Start all configured channel plugins. Called once after bridge setup.
 * Returns a summary of which channels were started.
 */
export async function startAllChannels(): Promise<{
  started: string[];
  skipped: string[];
  errors: string[];
}> {
  const started: string[] = [];
  const skipped: string[] = [];
  const errors: string[] = [];

  const plugins = listChannelPlugins();
  if (plugins.length === 0) {
    log.info("no channel plugins registered");
    return { started, skipped, errors };
  }

  const cfg = loadConfig();

  for (const plugin of plugins) {
    const channelId = plugin.id;
    const startAccount = plugin.gateway?.startAccount;
    if (!startAccount) {
      skipped.push(channelId);
      continue;
    }

    const accountIds = plugin.config.listAccountIds(cfg);
    if (accountIds.length === 0) {
      skipped.push(channelId);
      continue;
    }

    for (const accountId of accountIds) {
      const key = runtimeKey(channelId, accountId);
      if (runningTasks.has(key)) {
        continue;
      }

      try {
        const account = plugin.config.resolveAccount(cfg, accountId);

        // Check if account is enabled.
        const enabled = plugin.config.isEnabled
          ? plugin.config.isEnabled(account, cfg)
          : (account as { enabled?: boolean }).enabled !== false;
        if (!enabled) {
          setRuntime(channelId, accountId, { enabled: false, configured: true, running: false });
          skipped.push(`${channelId}:${accountId}`);
          continue;
        }

        // Check if account is configured.
        let configured = true;
        if (plugin.config.isConfigured) {
          configured = await plugin.config.isConfigured(account, cfg);
        }
        if (!configured) {
          setRuntime(channelId, accountId, {
            enabled: true,
            configured: false,
            running: false,
            lastError: "not configured",
          });
          skipped.push(`${channelId}:${accountId}`);
          continue;
        }

        const abort = new AbortController();
        const channelLog = createSubsystemLogger(channelId);
        const runtimeEnv = createChannelRuntimeEnv(channelId);

        setRuntime(channelId, accountId, {
          enabled: true,
          configured: true,
          running: true,
          lastStartAt: Date.now(),
          lastError: null,
        });

        // Start the channel as a long-lived task with auto-restart.
        const promise = runChannelWithRestart({
          channelId,
          accountId,
          startAccount,
          account,
          cfg,
          runtimeEnv,
          abort,
          log: channelLog,
        });

        runningTasks.set(key, { abort, promise });
        started.push(`${channelId}:${accountId}`);
        log.info(`channel started: ${channelId}:${accountId}`);
      } catch (err) {
        const msg = `${channelId}:${accountId}: ${String(err)}`;
        errors.push(msg);
        log.error(`channel start failed: ${msg}`);
      }
    }
  }

  return { started, skipped, errors };
}

type RunChannelParams = {
  channelId: string;
  accountId: string;
  startAccount: (ctx: Record<string, unknown>) => Promise<void>;
  account: unknown;
  cfg: unknown;
  runtimeEnv: RuntimeEnv;
  abort: AbortController;
  log: ReturnType<typeof createSubsystemLogger>;
};

async function runChannelWithRestart(params: RunChannelParams): Promise<void> {
  const {
    channelId,
    accountId,
    startAccount,
    account,
    cfg,
    runtimeEnv,
    abort,
    log: channelLog,
  } = params;
  let attempt = 0;

  while (!abort.signal.aborted) {
    try {
      await startAccount({
        cfg,
        accountId,
        account,
        runtime: runtimeEnv,
        abortSignal: abort.signal,
        log: channelLog,
        getStatus: () => getRuntime(channelId, accountId),
        setStatus: (next: Partial<ChannelAccountSnapshot>) =>
          setRuntime(channelId, accountId, next),
      });
      // startAccount resolved normally (clean exit).
      break;
    } catch (err) {
      if (abort.signal.aborted) {
        break;
      }
      const message = err instanceof Error ? err.message : String(err);
      setRuntime(channelId, accountId, { running: false, lastError: message });
      channelLog.error?.(`[${accountId}] exited: ${message}`);

      attempt++;
      if (attempt > MAX_RESTART_ATTEMPTS) {
        channelLog.error?.(
          `[${accountId}] giving up after ${MAX_RESTART_ATTEMPTS} restart attempts`,
        );
        break;
      }

      const delayMs = computeBackoff(RESTART_POLICY, attempt);
      channelLog.info?.(
        `[${accountId}] auto-restart ${attempt}/${MAX_RESTART_ATTEMPTS} in ${Math.round(delayMs / 1000)}s`,
      );
      setRuntime(channelId, accountId, { restartPending: true, reconnectAttempts: attempt });

      try {
        await sleepWithAbort(delayMs, abort.signal);
      } catch {
        // Aborted during sleep — exit cleanly.
        break;
      }

      setRuntime(channelId, accountId, {
        running: true,
        restartPending: false,
        lastStartAt: Date.now(),
        lastError: null,
      });
    }
  }

  setRuntime(channelId, accountId, { running: false, lastStopAt: Date.now() });
  const key = runtimeKey(channelId, accountId);
  runningTasks.delete(key);
}
