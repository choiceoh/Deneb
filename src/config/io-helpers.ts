import crypto from "node:crypto";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import JSON5 from "json5";
import { loadDotEnv } from "../infra/dotenv.js";
import { resolveRequiredHomeDir } from "../infra/home-dir.js";
import {
  loadShellEnvFallback,
  resolveShellEnvFallbackTimeoutMs,
  shouldDeferShellEnvFallback,
  shouldEnableShellEnvFallback,
} from "../infra/shell-env.js";
import { VERSION } from "../version.js";
import { type EnvSubstitutionWarning, resolveConfigEnvVars } from "./env-substitution.js";
import { applyConfigEnvVars } from "./env-vars.js";
import {
  ConfigIncludeError,
  readConfigIncludeFileWithGuards,
  resolveConfigIncludes,
} from "./includes.js";
import { resolveConfigPath, resolveStateDir } from "./paths.js";
import type { DenebConfig, ConfigFileSnapshot } from "./types.js";
import { compareDenebVersions } from "./version.js";

export const SHELL_ENV_EXPECTED_KEYS = [
  "OPENAI_API_KEY",
  "ANTHROPIC_API_KEY",
  "ANTHROPIC_OAUTH_TOKEN",
  "GEMINI_API_KEY",
  "ZAI_API_KEY",
  "OPENROUTER_API_KEY",
  "AI_GATEWAY_API_KEY",
  "MINIMAX_API_KEY",
  "MODELSTUDIO_API_KEY",
  "SYNTHETIC_API_KEY",
  "KILOCODE_API_KEY",
  "ELEVENLABS_API_KEY",
  "TELEGRAM_BOT_TOKEN",
  "DISCORD_BOT_TOKEN",
  "SLACK_BOT_TOKEN",
  "SLACK_APP_TOKEN",
  "DENEB_GATEWAY_TOKEN",
  "DENEB_GATEWAY_PASSWORD",
];

export const CONFIG_AUDIT_LOG_FILENAME = "config-audit.jsonl";

export type ConfigWriteAuditResult = "rename" | "copy-fallback" | "failed";

export type ConfigWriteAuditRecord = {
  ts: string;
  source: "config-io";
  event: "config.write";
  result: ConfigWriteAuditResult;
  configPath: string;
  pid: number;
  ppid: number;
  cwd: string;
  argv: string[];
  execArgv: string[];
  watchMode: boolean;
  watchSession: string | null;
  watchCommand: string | null;
  existsBefore: boolean;
  previousHash: string | null;
  nextHash: string | null;
  previousBytes: number | null;
  nextBytes: number | null;
  changedPathCount: number | null;
  hasMetaBefore: boolean;
  hasMetaAfter: boolean;
  gatewayModeBefore: string | null;
  gatewayModeAfter: string | null;
  suspicious: string[];
  errorCode?: string;
  errorMessage?: string;
};

export type ParseConfigJson5Result = { ok: true; parsed: unknown } | { ok: false; error: string };

export type ConfigIoDeps = {
  fs?: typeof fs;
  json5?: typeof JSON5;
  env?: NodeJS.ProcessEnv;
  homedir?: () => string;
  configPath?: string;
  logger?: Pick<typeof console, "error" | "warn">;
};

export type ConfigReadResolution = {
  resolvedConfigRaw: unknown;
  envSnapshotForRestore: Record<string, string | undefined>;
  envWarnings: EnvSubstitutionWarning[];
};

export type ReadConfigFileSnapshotInternalResult = {
  snapshot: ConfigFileSnapshot;
  envSnapshotForRestore?: Record<string, string | undefined>;
};

export function hashConfigRaw(raw: string | null): string {
  return crypto
    .createHash("sha256")
    .update(raw ?? "")
    .digest("hex");
}

export async function tightenStateDirPermissionsIfNeeded(params: {
  configPath: string;
  env: NodeJS.ProcessEnv;
  homedir: () => string;
  fsModule: typeof fs;
}): Promise<void> {
  if (process.platform === "win32") {
    return;
  }
  const stateDir = resolveStateDir(params.env, params.homedir);
  const configDir = path.dirname(params.configPath);
  if (path.resolve(configDir) !== path.resolve(stateDir)) {
    return;
  }
  try {
    const stat = await params.fsModule.promises.stat(configDir);
    const mode = stat.mode & 0o777;
    if ((mode & 0o077) === 0) {
      return;
    }
    await params.fsModule.promises.chmod(configDir, 0o700);
  } catch {
    // Best-effort hardening only; callers still need the config write to proceed.
  }
}

export function hasConfigMeta(value: unknown): boolean {
  if (!isPlainObjectForHelpers(value)) {
    return false;
  }
  const meta = value.meta;
  return isPlainObjectForHelpers(meta);
}

export function resolveGatewayMode(value: unknown): string | null {
  if (!isPlainObjectForHelpers(value)) {
    return null;
  }
  const gateway = value.gateway;
  if (!isPlainObjectForHelpers(gateway) || typeof gateway.mode !== "string") {
    return null;
  }
  const trimmed = gateway.mode.trim();
  return trimmed.length > 0 ? trimmed : null;
}

/** Minimal plain-object check used only within io-helpers (avoids circular dep on io-path-ops). */
function isPlainObjectForHelpers(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

export function resolveConfigAuditLogPath(env: NodeJS.ProcessEnv, homedir: () => string): string {
  return path.join(resolveStateDir(env, homedir), "logs", CONFIG_AUDIT_LOG_FILENAME);
}

export function resolveConfigWriteSuspiciousReasons(params: {
  existsBefore: boolean;
  previousBytes: number | null;
  nextBytes: number | null;
  hasMetaBefore: boolean;
  gatewayModeBefore: string | null;
  gatewayModeAfter: string | null;
}): string[] {
  const reasons: string[] = [];
  if (!params.existsBefore) {
    return reasons;
  }
  if (
    typeof params.previousBytes === "number" &&
    typeof params.nextBytes === "number" &&
    params.previousBytes >= 512 &&
    params.nextBytes < Math.floor(params.previousBytes * 0.5)
  ) {
    reasons.push(`size-drop:${params.previousBytes}->${params.nextBytes}`);
  }
  if (!params.hasMetaBefore) {
    reasons.push("missing-meta-before-write");
  }
  if (params.gatewayModeBefore && !params.gatewayModeAfter) {
    reasons.push("gateway-mode-removed");
  }
  return reasons;
}

export async function appendConfigWriteAuditRecord(
  deps: Required<ConfigIoDeps>,
  record: ConfigWriteAuditRecord,
): Promise<void> {
  try {
    const auditPath = resolveConfigAuditLogPath(deps.env, deps.homedir);
    await deps.fs.promises.mkdir(path.dirname(auditPath), { recursive: true, mode: 0o700 });
    await deps.fs.promises.appendFile(auditPath, `${JSON.stringify(record)}\n`, {
      encoding: "utf-8",
      mode: 0o600,
    });
  } catch {
    // best-effort
  }
}

export function stampConfigVersion(cfg: DenebConfig): DenebConfig {
  const now = new Date().toISOString();
  return {
    ...cfg,
    meta: {
      ...cfg.meta,
      lastTouchedVersion: VERSION,
      lastTouchedAt: now,
    },
  };
}

export function warnIfConfigFromFuture(
  cfg: DenebConfig,
  logger: Pick<typeof console, "warn">,
): void {
  const touched = cfg.meta?.lastTouchedVersion;
  if (!touched) {
    return;
  }
  const cmp = compareDenebVersions(VERSION, touched);
  if (cmp === null) {
    return;
  }
  if (cmp < 0) {
    logger.warn(
      `Config was last written by a newer Deneb (${touched}); current version is ${VERSION}.`,
    );
  }
}

export function resolveConfigPathForDeps(deps: Required<ConfigIoDeps>): string {
  if (deps.configPath) {
    return deps.configPath;
  }
  return resolveConfigPath(deps.env, resolveStateDir(deps.env, deps.homedir));
}

export function normalizeDeps(overrides: ConfigIoDeps = {}): Required<ConfigIoDeps> {
  return {
    fs: overrides.fs ?? fs,
    json5: overrides.json5 ?? JSON5,
    env: overrides.env ?? process.env,
    homedir:
      overrides.homedir ?? (() => resolveRequiredHomeDir(overrides.env ?? process.env, os.homedir)),
    configPath: overrides.configPath ?? "",
    logger: overrides.logger ?? console,
  };
}

export function maybeLoadDotEnvForConfig(env: NodeJS.ProcessEnv): void {
  // Only hydrate dotenv for the real process env. Callers using injected env
  // objects (tests/diagnostics) should stay isolated.
  if (env !== process.env) {
    return;
  }
  loadDotEnv({ quiet: true });
}

export function parseConfigJson5(
  raw: string,
  json5: { parse: (value: string) => unknown } = JSON5,
): ParseConfigJson5Result {
  try {
    return { ok: true, parsed: json5.parse(raw) };
  } catch (err) {
    return { ok: false, error: String(err) };
  }
}

export function resolveConfigIncludesForRead(
  parsed: unknown,
  configPath: string,
  deps: Required<ConfigIoDeps>,
): unknown {
  return resolveConfigIncludes(parsed, configPath, {
    readFile: (candidate) => deps.fs.readFileSync(candidate, "utf-8"),
    readFileWithGuards: ({ includePath, resolvedPath, rootRealDir }) =>
      readConfigIncludeFileWithGuards({
        includePath,
        resolvedPath,
        rootRealDir,
        ioFs: deps.fs,
      }),
    parseJson: (raw) => deps.json5.parse(raw),
  });
}

export function resolveConfigForRead(
  resolvedIncludes: unknown,
  env: NodeJS.ProcessEnv,
): ConfigReadResolution {
  // Apply config.env to process.env BEFORE substitution so ${VAR} can reference config-defined vars.
  if (resolvedIncludes && typeof resolvedIncludes === "object" && "env" in resolvedIncludes) {
    applyConfigEnvVars(resolvedIncludes as DenebConfig, env);
  }

  // Collect missing env var references as warnings instead of throwing,
  // so non-critical config sections with unset vars don't crash the gateway.
  const envWarnings: EnvSubstitutionWarning[] = [];
  return {
    resolvedConfigRaw: resolveConfigEnvVars(resolvedIncludes, env, {
      onMissing: (w) => envWarnings.push(w),
    }),
    // Capture env snapshot after substitution for write-time ${VAR} restoration.
    envSnapshotForRestore: { ...env } as Record<string, string | undefined>,
    envWarnings,
  };
}

export { ConfigIncludeError };

export function maybeLoadShellEnvFallbackForNoConfig(deps: Required<ConfigIoDeps>): void {
  if (shouldEnableShellEnvFallback(deps.env) && !shouldDeferShellEnvFallback(deps.env)) {
    loadShellEnvFallback({
      enabled: true,
      env: deps.env,
      expectedKeys: SHELL_ENV_EXPECTED_KEYS,
      logger: deps.logger,
      timeoutMs: resolveShellEnvFallbackTimeoutMs(deps.env),
    });
  }
}

export function maybeLoadShellEnvFallbackForConfig(
  cfg: DenebConfig,
  deps: Required<ConfigIoDeps>,
): void {
  const enabled = shouldEnableShellEnvFallback(deps.env) || cfg.env?.shellEnv?.enabled === true;
  if (enabled && !shouldDeferShellEnvFallback(deps.env)) {
    loadShellEnvFallback({
      enabled: true,
      env: deps.env,
      expectedKeys: SHELL_ENV_EXPECTED_KEYS,
      logger: deps.logger,
      timeoutMs: cfg.env?.shellEnv?.timeoutMs ?? resolveShellEnvFallbackTimeoutMs(deps.env),
    });
  }
}
