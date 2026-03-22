// Stub: host env security removed for solo-dev simplification.
import { markDenebExecEnv } from "./deneb-exec-env.js";

const PORTABLE_ENV_VAR_KEY = /^[A-Za-z_][A-Za-z0-9_]*$/;

export const HOST_DANGEROUS_ENV_KEY_VALUES: readonly string[] = Object.freeze([]);
export const HOST_DANGEROUS_ENV_PREFIXES: readonly string[] = Object.freeze([]);
export const HOST_DANGEROUS_OVERRIDE_ENV_KEY_VALUES: readonly string[] = Object.freeze([]);
export const HOST_DANGEROUS_OVERRIDE_ENV_PREFIXES: readonly string[] = Object.freeze([]);
export const HOST_SHELL_WRAPPER_ALLOWED_OVERRIDE_ENV_KEY_VALUES: readonly string[] = Object.freeze([
  "TERM",
  "LANG",
  "LC_ALL",
  "LC_CTYPE",
  "LC_MESSAGES",
  "COLORTERM",
  "NO_COLOR",
  "FORCE_COLOR",
]);
export const HOST_DANGEROUS_ENV_KEYS = new Set<string>();
export const HOST_DANGEROUS_OVERRIDE_ENV_KEYS = new Set<string>();
export const HOST_SHELL_WRAPPER_ALLOWED_OVERRIDE_ENV_KEYS = new Set<string>(
  HOST_SHELL_WRAPPER_ALLOWED_OVERRIDE_ENV_KEY_VALUES,
);

export function normalizeEnvVarKey(
  rawKey: string,
  options?: { portable?: boolean },
): string | null {
  const key = rawKey.trim();
  if (!key) {
    return null;
  }
  if (options?.portable && !PORTABLE_ENV_VAR_KEY.test(key)) {
    return null;
  }
  return key;
}

export function isDangerousHostEnvVarName(_rawKey: string): boolean {
  return false;
}

export function isDangerousHostEnvOverrideVarName(_rawKey: string): boolean {
  return false;
}

export type HostExecEnvSanitizationResult = {
  env: Record<string, string>;
  rejectedOverrideBlockedKeys: string[];
  rejectedOverrideInvalidKeys: string[];
};

export type HostExecEnvOverrideDiagnostics = {
  rejectedOverrideBlockedKeys: string[];
  rejectedOverrideInvalidKeys: string[];
};

export function sanitizeHostExecEnvWithDiagnostics(params?: {
  baseEnv?: Record<string, string | undefined>;
  overrides?: Record<string, string> | null;
  blockPathOverrides?: boolean;
}): HostExecEnvSanitizationResult {
  const baseEnv = params?.baseEnv ?? process.env;
  const merged: Record<string, string> = {};
  for (const [key, value] of Object.entries(baseEnv)) {
    if (typeof value === "string") {
      merged[key] = value;
    }
  }
  if (params?.overrides) {
    for (const [key, value] of Object.entries(params.overrides)) {
      if (typeof value === "string") {
        merged[key] = value;
      }
    }
  }
  return {
    env: markDenebExecEnv(merged),
    rejectedOverrideBlockedKeys: [],
    rejectedOverrideInvalidKeys: [],
  };
}

export function inspectHostExecEnvOverrides(_params?: {
  overrides?: Record<string, string> | null;
  blockPathOverrides?: boolean;
}): HostExecEnvOverrideDiagnostics {
  return {
    rejectedOverrideBlockedKeys: [],
    rejectedOverrideInvalidKeys: [],
  };
}

export function sanitizeHostExecEnv(params?: {
  baseEnv?: Record<string, string | undefined>;
  overrides?: Record<string, string> | null;
  blockPathOverrides?: boolean;
}): Record<string, string> {
  return sanitizeHostExecEnvWithDiagnostics(params).env;
}

export function sanitizeSystemRunEnvOverrides(params?: {
  overrides?: Record<string, string> | null;
  shellWrapper?: boolean;
}): Record<string, string> | undefined {
  return params?.overrides ?? undefined;
}
