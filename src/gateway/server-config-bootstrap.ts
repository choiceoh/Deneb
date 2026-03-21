import { formatCliCommand } from "../cli/command-format.js";
/**
 * Gateway startup helpers: config validation, auth bootstrapping, and rate limiting.
 *
 * Extracted from server.impl.ts to reduce god-function size.
 * These are pure helper functions with no side-effect initialization.
 */
import {
  type ConfigFileSnapshot,
  type GatewayAuthConfig,
  type GatewayTailscaleConfig,
  type DenebConfig,
} from "../config/config.js";
import { formatConfigIssueLines } from "../config/issue-format.js";
import {
  GATEWAY_AUTH_SURFACE_PATHS,
  evaluateGatewayAuthSurfaceStates,
} from "../secrets/runtime-gateway-auth-surfaces.js";
import { createAuthRateLimiter, type AuthRateLimiter } from "./auth-rate-limit.js";
import {
  ensureGatewayStartupAuth,
  mergeGatewayAuthConfig,
  mergeGatewayTailscaleConfig,
} from "./startup-auth.js";

// ── Media cleanup ───────────────────────────────────────────────────

const MAX_MEDIA_TTL_HOURS = 24 * 7;

export function resolveMediaCleanupTtlMs(ttlHoursRaw: number): number {
  const ttlHours = Math.min(Math.max(ttlHoursRaw, 1), MAX_MEDIA_TTL_HOURS);
  const ttlMs = ttlHours * 60 * 60_000;
  if (!Number.isFinite(ttlMs) || !Number.isSafeInteger(ttlMs)) {
    throw new Error(`Invalid media.ttlHours: ${String(ttlHoursRaw)}`);
  }
  return ttlMs;
}

// ── Auth rate limiters ──────────────────────────────────────────────

type AuthRateLimitConfig = Parameters<typeof createAuthRateLimiter>[0];

export function createGatewayAuthRateLimiters(rateLimitConfig: AuthRateLimitConfig | undefined): {
  rateLimiter?: AuthRateLimiter;
  browserRateLimiter: AuthRateLimiter;
} {
  const rateLimiter = rateLimitConfig ? createAuthRateLimiter(rateLimitConfig) : undefined;
  // Browser-origin WS auth attempts always use loopback-non-exempt throttling.
  const browserRateLimiter = createAuthRateLimiter({
    ...rateLimitConfig,
    exemptLoopback: false,
  });
  return { rateLimiter, browserRateLimiter };
}

// ── Auth surface diagnostics ────────────────────────────────────────

export function logGatewayAuthSurfaceDiagnostics(
  prepared: {
    sourceConfig: DenebConfig;
    warnings: Array<{ code: string; path: string; message: string }>;
  },
  logSecrets: { info: (msg: string) => void },
): void {
  const states = evaluateGatewayAuthSurfaceStates({
    config: prepared.sourceConfig,
    defaults: prepared.sourceConfig.secrets?.defaults,
    env: process.env,
  });
  const inactiveWarnings = new Map<string, string>();
  for (const warning of prepared.warnings) {
    if (warning.code !== "SECRETS_REF_IGNORED_INACTIVE_SURFACE") {
      continue;
    }
    inactiveWarnings.set(warning.path, warning.message);
  }
  for (const path of GATEWAY_AUTH_SURFACE_PATHS) {
    const state = states[path];
    if (!state.hasSecretRef) {
      continue;
    }
    const stateLabel = state.active ? "active" : "inactive";
    const inactiveDetails =
      !state.active && inactiveWarnings.get(path) ? inactiveWarnings.get(path) : undefined;
    const details = inactiveDetails ?? state.reason;
    logSecrets.info(`[SECRETS_GATEWAY_AUTH_SURFACE] ${path} is ${stateLabel}. ${details}`);
  }
}

// ── Auth override helper ────────────────────────────────────────────

export function applyGatewayAuthOverridesForStartupPreflight(
  config: DenebConfig,
  overrides: { auth?: GatewayAuthConfig; tailscale?: GatewayTailscaleConfig },
): DenebConfig {
  if (!overrides.auth && !overrides.tailscale) {
    return config;
  }
  return {
    ...config,
    gateway: {
      ...config.gateway,
      auth: mergeGatewayAuthConfig(config.gateway?.auth, overrides.auth),
      tailscale: mergeGatewayTailscaleConfig(config.gateway?.tailscale, overrides.tailscale),
    },
  };
}

// ── Config snapshot validation ──────────────────────────────────────

export function assertValidGatewayStartupConfigSnapshot(
  snapshot: ConfigFileSnapshot,
  options: { includeDoctorHint?: boolean } = {},
): void {
  if (snapshot.valid) {
    return;
  }
  const issues =
    snapshot.issues.length > 0
      ? formatConfigIssueLines(snapshot.issues, "", { normalizeRoot: true }).join("\n")
      : "Unknown validation issue.";
  const doctorHint = options.includeDoctorHint
    ? `\nRun "${formatCliCommand("deneb doctor")}" to repair, then retry.`
    : "";
  throw new Error(`Invalid config at ${snapshot.path}.\n${issues}${doctorHint}`);
}

// ── Startup config preparation ──────────────────────────────────────

export async function prepareGatewayStartupConfig(params: {
  configSnapshot: ConfigFileSnapshot;
  // Keep startup auth/runtime behavior aligned with loadConfig(), which applies
  // runtime overrides beyond the raw on-disk snapshot.
  runtimeConfig: DenebConfig;
  authOverride?: GatewayAuthConfig;
  tailscaleOverride?: GatewayTailscaleConfig;
  activateRuntimeSecrets: (
    config: DenebConfig,
    options: { reason: "startup"; activate: boolean },
  ) => Promise<{ config: DenebConfig }>;
}): Promise<Awaited<ReturnType<typeof ensureGatewayStartupAuth>>> {
  assertValidGatewayStartupConfigSnapshot(params.configSnapshot);

  // Fail fast before startup auth persists anything if required refs are unresolved.
  const startupPreflightConfig = applyGatewayAuthOverridesForStartupPreflight(
    params.runtimeConfig,
    {
      auth: params.authOverride,
      tailscale: params.tailscaleOverride,
    },
  );
  await params.activateRuntimeSecrets(startupPreflightConfig, {
    reason: "startup",
    activate: false,
  });

  const authBootstrap = await ensureGatewayStartupAuth({
    cfg: params.runtimeConfig,
    env: process.env,
    authOverride: params.authOverride,
    tailscaleOverride: params.tailscaleOverride,
    persist: true,
  });
  const runtimeStartupConfig = applyGatewayAuthOverridesForStartupPreflight(authBootstrap.cfg, {
    auth: params.authOverride,
    tailscale: params.tailscaleOverride,
  });
  const activatedConfig = (
    await params.activateRuntimeSecrets(runtimeStartupConfig, {
      reason: "startup",
      activate: true,
    })
  ).config;
  return {
    ...authBootstrap,
    cfg: activatedConfig,
  };
}
