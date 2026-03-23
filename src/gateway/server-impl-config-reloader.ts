/**
 * Gateway startup: config-reloader wiring.
 *
 * Connects `startGatewayConfigReloader` and `createGatewayReloadHandlers`
 * with the mutable gateway state they need to observe and mutate during
 * hot-reload and restart cycles.
 *
 * The `getState`/`setState` callbacks allow the reloader to read and write
 * mutable `let` variables that live in the `startGatewayServer` closure.
 *
 * Extracted from server.impl.ts.
 */

import type { CliDeps } from "../cli/deps.js";
import type { DenebConfig } from "../config/config.js";
import { readConfigFileSnapshot } from "../config/config.js";
import type { HeartbeatRunner } from "../infra/heartbeat-runner.js";
import {
  activateSecretsRuntimeSnapshot,
  clearSecretsRuntimeSnapshot,
  getActiveSecretsRuntimeSnapshot,
} from "../secrets/runtime.js";
import { startGatewayConfigReloader } from "./config-reload.js";
import type { GatewayConfigReloader } from "./config-reload.js";
import type { ChannelKind } from "./config-reload.js";
import type { resolveHooksConfig } from "./hooks.js";
import type { HookClientIpConfig } from "./http/server-http.js";
import { startChannelHealthMonitor } from "./monitoring/channel-health-monitor.js";
import type { ChannelHealthMonitor } from "./monitoring/channel-health-monitor.js";
import type { startBrowserControlServerIfEnabled } from "./server-browser.js";
import type { ChannelManager } from "./server-channels.js";
import type { GatewayCronState } from "./server-cron.js";
import type { PreparedSecrets, SecretsActivationParams } from "./server-impl-secrets.js";
import { createGatewayReloadHandlers } from "./server-reload-handlers.js";

export type GatewayHotReloadState = {
  hooksConfig: ReturnType<typeof resolveHooksConfig>;
  hookClientIpConfig: HookClientIpConfig;
  heartbeatRunner: HeartbeatRunner;
  cronState: GatewayCronState;
  browserControl: Awaited<ReturnType<typeof startBrowserControlServerIfEnabled>> | null;
  channelHealthMonitor: ChannelHealthMonitor | null;
};

export type BuildConfigReloaderOptions = {
  deps: CliDeps;
  broadcast: (event: string, payload: unknown, opts?: { dropIfSlow?: boolean }) => void;
  getState: () => GatewayHotReloadState;
  setState: (state: GatewayHotReloadState) => void;
  startChannel: (name: ChannelKind) => Promise<void>;
  stopChannel: (name: ChannelKind) => Promise<void>;
  channelManager: ChannelManager;
  activateRuntimeSecrets: (
    config: DenebConfig,
    params: SecretsActivationParams,
  ) => Promise<PreparedSecrets>;
  initialConfig: DenebConfig;
  configSnapshotPath: string;
  logHooks: {
    info: (msg: string) => void;
    warn: (msg: string) => void;
    error: (msg: string) => void;
  };
  logBrowser: { error: (msg: string) => void };
  logChannels: { info: (msg: string) => void; error: (msg: string) => void };
  logCron: { error: (msg: string) => void };
  logReload: {
    info: (msg: string) => void;
    warn: (msg: string) => void;
    error: (msg: string) => void;
  };
};

/**
 * Creates and starts the gateway config-reloader service.
 *
 * On hot-reload: re-activates secrets, then delegates to `applyHotReload`.
 * On restart-required changes: validates secrets and calls `requestGatewayRestart`.
 */
export function buildGatewayConfigReloader(
  opts: BuildConfigReloaderOptions,
): GatewayConfigReloader {
  const {
    deps,
    broadcast,
    getState,
    setState,
    startChannel,
    stopChannel,
    channelManager,
    activateRuntimeSecrets,
    initialConfig,
    configSnapshotPath,
    logHooks,
    logBrowser,
    logChannels,
    logCron,
    logReload,
  } = opts;

  const { applyHotReload, requestGatewayRestart } = createGatewayReloadHandlers({
    deps,
    broadcast,
    getState,
    setState,
    startChannel,
    stopChannel,
    logHooks,
    logBrowser,
    logChannels,
    logCron,
    logReload,
    createHealthMonitor: (monitorOpts) =>
      startChannelHealthMonitor({
        channelManager,
        checkIntervalMs: monitorOpts.checkIntervalMs,
        ...(monitorOpts.staleEventThresholdMs != null && {
          staleEventThresholdMs: monitorOpts.staleEventThresholdMs,
        }),
        ...(monitorOpts.maxRestartsPerHour != null && {
          maxRestartsPerHour: monitorOpts.maxRestartsPerHour,
        }),
      }),
  });

  return startGatewayConfigReloader({
    initialConfig,
    readSnapshot: readConfigFileSnapshot,
    onHotReload: async (plan, nextConfig) => {
      const previousSnapshot = getActiveSecretsRuntimeSnapshot();
      const prepared = await activateRuntimeSecrets(nextConfig, {
        reason: "reload",
        activate: true,
      });
      try {
        await applyHotReload(plan, prepared.config);
      } catch (err) {
        if (previousSnapshot) {
          activateSecretsRuntimeSnapshot(previousSnapshot);
        } else {
          clearSecretsRuntimeSnapshot();
        }
        throw err;
      }
    },
    onRestart: async (plan, nextConfig) => {
      await activateRuntimeSecrets(nextConfig, { reason: "restart-check", activate: false });
      requestGatewayRestart(plan, nextConfig);
    },
    log: {
      info: (msg) => logReload.info(msg),
      warn: (msg) => logReload.warn(msg),
      error: (msg) => logReload.error(msg),
    },
    watchPath: configSnapshotPath,
  });
}
