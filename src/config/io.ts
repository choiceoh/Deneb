// Re-export for backwards compatibility
export { CircularIncludeError, ConfigIncludeError } from "./includes.js";
export { ConfigIntegrityError } from "./integrity-guard.js";
export { MissingEnvVarError } from "./env-substitution.js";
export { resolveConfigSnapshotHash } from "./io-path-ops.js";
export { parseConfigJson5, type ParseConfigJson5Result, type ConfigIoDeps } from "./io-helpers.js";
export {
  createConfigIO,
  type ConfigWriteOptions,
  type ReadConfigFileSnapshotForWriteResult,
} from "./io-create.js";
export {
  getRuntimeConfigSnapshot,
  getRuntimeConfigSourceSnapshot,
  setRuntimeConfigSnapshotRefreshHandler,
  getRuntimeConfigSnapshotRefreshHandler,
  projectConfigOntoRuntimeSourceSnapshot,
  ConfigRuntimeRefreshError,
  type RuntimeConfigSnapshotRefreshParams,
  type RuntimeConfigSnapshotRefreshHandler,
} from "./io-snapshot.js";
export { clearConfigCache } from "./io-cache.js";

import {
  clearConfigCache,
  getCachedConfig,
  setCachedConfig,
  shouldUseConfigCache,
  resolveConfigCacheMs,
} from "./io-cache.js";
import { createConfigIO } from "./io-create.js";
import type { ConfigWriteOptions, ReadConfigFileSnapshotForWriteResult } from "./io-create.js";
import { coerceConfig, createMergePatch } from "./io-path-ops.js";
import {
  getRuntimeConfigSnapshot,
  getRuntimeConfigSourceSnapshot,
  getRuntimeConfigSnapshotRefreshHandler,
  setRuntimeConfigSnapshot as setRuntimeConfigSnapshotInner,
  clearRuntimeConfigSnapshot as clearRuntimeConfigSnapshotInner,
  ConfigRuntimeRefreshError,
} from "./io-snapshot.js";
import { applyMergePatch } from "./merge-patch.js";
import type { DenebConfig } from "./types.js";
import type { ConfigFileSnapshot } from "./types.js";

// Wrap snapshot setters to also clear the config cache (which they did in the
// original single-module implementation).
export function setRuntimeConfigSnapshot(config: DenebConfig, sourceConfig?: DenebConfig): void {
  setRuntimeConfigSnapshotInner(config, sourceConfig);
  clearConfigCache();
}

export function clearRuntimeConfigSnapshot(): void {
  clearRuntimeConfigSnapshotInner();
  clearConfigCache();
}

export function loadConfig(): DenebConfig {
  const runtimeConfigSnapshot = getRuntimeConfigSnapshot();
  if (runtimeConfigSnapshot) {
    return runtimeConfigSnapshot;
  }
  const io = createConfigIO(undefined, clearConfigCache);
  const configPath = io.configPath;
  const now = Date.now();
  if (shouldUseConfigCache(process.env)) {
    const cached = getCachedConfig(configPath, now);
    if (cached) {
      return cached;
    }
  }
  const config = io.loadConfig();
  if (shouldUseConfigCache(process.env)) {
    const cacheMs = resolveConfigCacheMs(process.env);
    if (cacheMs > 0) {
      setCachedConfig(configPath, config, cacheMs, now);
    }
  }
  return config;
}

export async function readBestEffortConfig(): Promise<DenebConfig> {
  const snapshot = await readConfigFileSnapshot();
  return snapshot.valid ? loadConfig() : snapshot.config;
}

export async function readConfigFileSnapshot(): Promise<ConfigFileSnapshot> {
  return await createConfigIO().readConfigFileSnapshot();
}

export async function readConfigFileSnapshotForWrite(): Promise<ReadConfigFileSnapshotForWriteResult> {
  return await createConfigIO().readConfigFileSnapshotForWrite();
}

export async function writeConfigFile(
  cfg: DenebConfig,
  options: ConfigWriteOptions = {},
): Promise<void> {
  const io = createConfigIO(undefined, clearConfigCache);
  let nextCfg = cfg;
  const runtimeConfigSnapshot = getRuntimeConfigSnapshot();
  const runtimeConfigSourceSnapshot = getRuntimeConfigSourceSnapshot();
  const hadRuntimeSnapshot = Boolean(runtimeConfigSnapshot);
  const hadBothSnapshots = Boolean(runtimeConfigSnapshot && runtimeConfigSourceSnapshot);
  if (hadBothSnapshots) {
    const runtimePatch = createMergePatch(runtimeConfigSnapshot!, cfg);
    nextCfg = coerceConfig(applyMergePatch(runtimeConfigSourceSnapshot!, runtimePatch));
  }
  const sameConfigPath =
    options.expectedConfigPath === undefined || options.expectedConfigPath === io.configPath;
  await io.writeConfigFile(nextCfg, {
    envSnapshotForRestore: sameConfigPath ? options.envSnapshotForRestore : undefined,
    unsetPaths: options.unsetPaths,
  });
  // Keep the last-known-good runtime snapshot active until the specialized refresh path
  // succeeds, so concurrent readers do not observe unresolved SecretRefs mid-refresh.
  const refreshHandler = getRuntimeConfigSnapshotRefreshHandler();
  if (refreshHandler) {
    try {
      const refreshed = await refreshHandler.refresh({ sourceConfig: nextCfg });
      if (refreshed) {
        return;
      }
    } catch (error) {
      try {
        refreshHandler.clearOnRefreshFailure?.();
      } catch {
        // Keep the original refresh failure as the surfaced error.
      }
      const detail = error instanceof Error ? error.message : String(error);
      throw new ConfigRuntimeRefreshError(
        `Config was written to ${io.configPath}, but runtime snapshot refresh failed: ${detail}`,
        { cause: error },
      );
    }
  }
  if (hadBothSnapshots) {
    // Refresh both snapshots from disk atomically so follow-up reads get normalized config and
    // subsequent writes still get secret-preservation merge-patch (hadBothSnapshots stays true).
    const fresh = io.loadConfig();
    setRuntimeConfigSnapshot(fresh, nextCfg);
    return;
  }
  if (hadRuntimeSnapshot) {
    clearRuntimeConfigSnapshot();
  }
  // When we had no runtime snapshot, keep callers reading from disk/cache so external/manual
  // edits to deneb.json remain visible (no stale snapshot).
}
