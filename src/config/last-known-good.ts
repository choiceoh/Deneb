/**
 * Last-known-good config recovery.
 *
 * On every successful config load, a snapshot of the resolved config (post-include,
 * post-env-substitution, pre-defaults) is saved. When config validation fails on a
 * subsequent load, the last-known-good snapshot is used as a fallback so the gateway
 * can start in degraded mode instead of crashing.
 *
 * The LKG file is stored alongside the config file as `config-last-known-good.json`
 * with owner-only permissions (0o600).
 */
import fs from "node:fs";
import path from "node:path";

const LKG_FILENAME = "config-last-known-good.json";

/** Whether the current process is running from a last-known-good fallback config. */
let fallbackActive = false;

function resolveLkgPath(configPath: string): string {
  return path.join(path.dirname(configPath), LKG_FILENAME);
}

/**
 * Save a last-known-good config snapshot after successful validation.
 * Stores the resolved config (post-include, post-env-substitution) so the
 * backup is self-contained and does not depend on include files or env vars.
 *
 * Best-effort; failures are silently ignored.
 */
export function saveLastKnownGoodConfig(configPath: string, resolvedConfig: unknown): void {
  try {
    const lkgPath = resolveLkgPath(configPath);
    const json = JSON.stringify(resolvedConfig, null, 2);
    fs.writeFileSync(lkgPath, json, { encoding: "utf-8", mode: 0o600 });
  } catch {
    // Best-effort: don't disrupt normal operation if save fails.
  }
}

/**
 * Try to load the last-known-good config.
 * Returns the parsed config object, or null if no LKG exists or parsing fails.
 */
export function loadLastKnownGoodConfig(configPath: string): unknown {
  try {
    const lkgPath = resolveLkgPath(configPath);
    if (!fs.existsSync(lkgPath)) {
      return null;
    }
    const raw = fs.readFileSync(lkgPath, "utf-8");
    const parsed = JSON.parse(raw);
    if (typeof parsed !== "object" || parsed === null) {
      return null;
    }
    return parsed;
  } catch {
    return null;
  }
}

/**
 * Check if a last-known-good config backup exists for the given config path.
 */
export function hasLastKnownGoodConfig(configPath: string): boolean {
  try {
    return fs.existsSync(resolveLkgPath(configPath));
  } catch {
    return false;
  }
}

/**
 * Returns true if the current process fell back to a last-known-good config
 * because the main config file was invalid.
 */
export function isLastKnownGoodFallbackActive(): boolean {
  return fallbackActive;
}

export function setLastKnownGoodFallbackActive(active: boolean): void {
  fallbackActive = active;
}

/** Visible for testing. */
export function resolveLkgPathForTest(configPath: string): string {
  return resolveLkgPath(configPath);
}
