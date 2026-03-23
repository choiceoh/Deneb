import type { DenebConfig } from "./types.js";

const DEFAULT_CONFIG_CACHE_MS = 200;

let configCache: {
  configPath: string;
  expiresAt: number;
  config: DenebConfig;
} | null = null;

export function clearConfigCache(): void {
  configCache = null;
}

export function resolveConfigCacheMs(env: NodeJS.ProcessEnv): number {
  const raw = env.DENEB_CONFIG_CACHE_MS?.trim();
  if (raw === "" || raw === "0") {
    return 0;
  }
  if (!raw) {
    return DEFAULT_CONFIG_CACHE_MS;
  }
  const parsed = Number.parseInt(raw, 10);
  if (!Number.isFinite(parsed)) {
    return DEFAULT_CONFIG_CACHE_MS;
  }
  return Math.max(0, parsed);
}

export function shouldUseConfigCache(env: NodeJS.ProcessEnv): boolean {
  if (env.DENEB_DISABLE_CONFIG_CACHE?.trim()) {
    return false;
  }
  return resolveConfigCacheMs(env) > 0;
}

export function getCachedConfig(configPath: string, now: number): DenebConfig | null {
  if (!configCache) {
    return null;
  }
  if (configCache.configPath === configPath && configCache.expiresAt > now) {
    return configCache.config;
  }
  return null;
}

export function setCachedConfig(
  configPath: string,
  config: DenebConfig,
  cacheMs: number,
  now: number,
): void {
  configCache = {
    configPath,
    expiresAt: now + cacheMs,
    config,
  };
}
