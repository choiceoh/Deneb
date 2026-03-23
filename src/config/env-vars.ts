import { containsEnvVarReference } from "./env-substitution.js";
import type { DenebConfig } from "./types.js";

const PORTABLE_ENV_VAR_KEY = /^[A-Za-z_][A-Za-z0-9_]*$/;

// Env vars that must never be set from user config — they control shell startup
// and could be abused for arbitrary code execution.
const BLOCKED_ENV_KEYS = new Set([
  "BASH_ENV",
  "ENV",
  "SHELL",
  "HOME",
  "ZDOTDIR",
  "LD_PRELOAD",
  "LD_LIBRARY_PATH",
  "DYLD_INSERT_LIBRARIES",
  "DYLD_LIBRARY_PATH",
]);

function normalizeEnvVarKey(rawKey: string, options?: { portable?: boolean }): string | null {
  const key = rawKey.trim();
  if (!key) {
    return null;
  }
  if (options?.portable && !PORTABLE_ENV_VAR_KEY.test(key)) {
    return null;
  }
  return key;
}

function collectConfigEnvVarsByTarget(cfg?: DenebConfig): Record<string, string> {
  const envConfig = cfg?.env;
  if (!envConfig) {
    return {};
  }

  const entries: Record<string, string> = {};

  if (envConfig.vars) {
    for (const [rawKey, value] of Object.entries(envConfig.vars)) {
      if (!value) {
        continue;
      }
      const key = normalizeEnvVarKey(rawKey, { portable: true });
      if (!key) {
        continue;
      }
      entries[key] = value;
    }
  }

  for (const [rawKey, value] of Object.entries(envConfig)) {
    if (rawKey === "shellEnv" || rawKey === "vars") {
      continue;
    }
    if (typeof value !== "string" || !value.trim()) {
      continue;
    }
    const key = normalizeEnvVarKey(rawKey, { portable: true });
    if (!key) {
      continue;
    }
    entries[key] = value;
  }

  return entries;
}

export function collectConfigRuntimeEnvVars(cfg?: DenebConfig): Record<string, string> {
  const entries = collectConfigEnvVarsByTarget(cfg);
  for (const key of Object.keys(entries)) {
    if (BLOCKED_ENV_KEYS.has(key)) {
      delete entries[key];
    }
  }
  return entries;
}

export function collectConfigServiceEnvVars(cfg?: DenebConfig): Record<string, string> {
  return collectConfigEnvVarsByTarget(cfg);
}

export function createConfigRuntimeEnv(
  cfg: DenebConfig,
  baseEnv: NodeJS.ProcessEnv = process.env,
): NodeJS.ProcessEnv {
  const env = { ...baseEnv };
  applyConfigEnvVars(cfg, env);
  return env;
}

export function applyConfigEnvVars(cfg: DenebConfig, env: NodeJS.ProcessEnv = process.env): void {
  const entries = collectConfigRuntimeEnvVars(cfg);
  for (const [key, value] of Object.entries(entries)) {
    if (env[key]?.trim()) {
      continue;
    }
    // Skip values containing unresolved ${VAR} references — applyConfigEnvVars runs
    // before env substitution, so these would pollute process.env with literal placeholders
    // (e.g. process.env.DENEB_GATEWAY_TOKEN = "${VAULT_TOKEN}") which downstream auth
    // resolution would accept as valid credentials.
    if (containsEnvVarReference(value)) {
      continue;
    }
    env[key] = value;
  }
}
