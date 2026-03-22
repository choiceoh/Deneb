const SUPERVISOR_HINTS = {
  systemd: ["DENEB_SYSTEMD_UNIT", "INVOCATION_ID", "SYSTEMD_EXEC_PID", "JOURNAL_STREAM"],
} as const;

export const SUPERVISOR_HINT_ENV_VARS = [
  ...SUPERVISOR_HINTS.systemd,
  "DENEB_SERVICE_MARKER",
  "DENEB_SERVICE_KIND",
] as const;

export type RespawnSupervisor = "systemd";

function hasAnyHint(env: NodeJS.ProcessEnv, keys: readonly string[]): boolean {
  return keys.some((key) => {
    const value = env[key];
    return typeof value === "string" && value.trim().length > 0;
  });
}

export function detectRespawnSupervisor(
  env: NodeJS.ProcessEnv = process.env,
): RespawnSupervisor | null {
  return hasAnyHint(env, SUPERVISOR_HINTS.systemd) ? "systemd" : null;
}
