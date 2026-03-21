export const DENEB_CLI_ENV_VAR = "DENEB_CLI";
export const DENEB_CLI_ENV_VALUE = "1";

export function markDenebExecEnv<T extends Record<string, string | undefined>>(env: T): T {
  return {
    ...env,
    [DENEB_CLI_ENV_VAR]: DENEB_CLI_ENV_VALUE,
  };
}

export function ensureDenebExecMarkerOnProcess(
  env: NodeJS.ProcessEnv = process.env,
): NodeJS.ProcessEnv {
  env[DENEB_CLI_ENV_VAR] = DENEB_CLI_ENV_VALUE;
  return env;
}
