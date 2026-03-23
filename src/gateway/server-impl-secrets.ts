/**
 * Gateway startup: secrets management helpers.
 *
 * Encapsulates the secrets-activation closure used during startup and
 * config-reload cycles. Extracted from server.impl.ts to keep that file
 * focused on the top-level orchestration flow.
 */

import type { DenebConfig } from "../config/config.js";
import { resolveMainSessionKey } from "../config/sessions.js";
import { enqueueSystemEvent } from "../infra/system-events.js";
import {
  activateSecretsRuntimeSnapshot,
  clearSecretsRuntimeSnapshot,
  prepareSecretsRuntimeSnapshot,
} from "../secrets/runtime.js";
import { logGatewayAuthSurfaceDiagnostics } from "./server-config-bootstrap.js";

export type PreparedSecrets = Awaited<ReturnType<typeof prepareSecretsRuntimeSnapshot>>;

export type SecretsActivationParams = {
  reason: "startup" | "reload" | "restart-check";
  activate: boolean;
};

export type SecretsManagerHandle = {
  activateRuntimeSecrets: (
    config: DenebConfig,
    params: SecretsActivationParams,
  ) => Promise<PreparedSecrets>;
};

function emitSecretsStateEvent(
  code: "SECRETS_RELOADER_DEGRADED" | "SECRETS_RELOADER_RECOVERED",
  message: string,
  cfg: DenebConfig,
) {
  enqueueSystemEvent(`[${code}] ${message}`, {
    sessionKey: resolveMainSessionKey(cfg),
    contextKey: code,
  });
}

/**
 * Creates the secrets manager used throughout gateway startup and hot-reload.
 *
 * The returned `activateRuntimeSecrets` function serialises concurrent calls
 * (via an internal promise chain) so that two simultaneous reload events
 * cannot interleave snapshot activation.
 */
export function createGatewaySecretsManager(logSecrets: {
  info: (msg: string) => void;
  warn: (msg: string) => void;
  error: (msg: string) => void;
}): SecretsManagerHandle {
  let secretsDegraded = false;
  // Chain all activation calls so they run serially even if invoked concurrently.
  let secretsActivationTail: Promise<void> = Promise.resolve();

  const runWithSecretsActivationLock = async <T>(operation: () => Promise<T>): Promise<T> => {
    const run = secretsActivationTail.then(operation, operation);
    secretsActivationTail = run.then(
      () => undefined,
      () => undefined,
    );
    return await run;
  };

  const activateRuntimeSecrets = async (
    config: DenebConfig,
    params: SecretsActivationParams,
  ): Promise<PreparedSecrets> =>
    await runWithSecretsActivationLock(async () => {
      try {
        const prepared = await prepareSecretsRuntimeSnapshot({ config });
        if (params.activate) {
          activateSecretsRuntimeSnapshot(prepared);
          logGatewayAuthSurfaceDiagnostics(prepared, logSecrets);
        }
        for (const warning of prepared.warnings) {
          logSecrets.warn(`[${warning.code}] ${warning.message}`);
        }
        if (secretsDegraded) {
          const recoveredMessage =
            "Secret resolution recovered; runtime remained on last-known-good during the outage.";
          logSecrets.info(`[SECRETS_RELOADER_RECOVERED] ${recoveredMessage}`);
          emitSecretsStateEvent("SECRETS_RELOADER_RECOVERED", recoveredMessage, prepared.config);
        }
        secretsDegraded = false;
        return prepared;
      } catch (err) {
        const details = String(err);
        if (!secretsDegraded) {
          logSecrets.error(`[SECRETS_RELOADER_DEGRADED] ${details}`);
          if (params.reason !== "startup") {
            emitSecretsStateEvent(
              "SECRETS_RELOADER_DEGRADED",
              `Secret resolution failed; runtime remains on last-known-good snapshot. ${details}`,
              config,
            );
          }
        } else {
          logSecrets.warn(`[SECRETS_RELOADER_DEGRADED] ${details}`);
        }
        secretsDegraded = true;
        if (params.reason === "startup") {
          throw new Error(`Startup failed: required secrets are unavailable. ${details}`, {
            cause: err,
          });
        }
        throw err;
      }
    });

  return { activateRuntimeSecrets };
}

export { activateSecretsRuntimeSnapshot, clearSecretsRuntimeSnapshot };
