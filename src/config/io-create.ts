import crypto from "node:crypto";
import path from "node:path";
import { ensureOwnerDisplaySecret } from "../agents/owner-display.js";
import { sanitizeTerminalText } from "../terminal/safe-text.js";
import { DuplicateAgentDirError, findDuplicateAgentDirs } from "./agent-dirs.js";
import { maintainConfigBackups } from "./backup-rotation.js";
import {
  applyContextPruningDefaults,
  applyAgentDefaults,
  applyLoggingDefaults,
  applyMessageDefaults,
  applyModelDefaults,
  applySessionDefaults,
  applyTalkConfigNormalization,
} from "./defaults.js";
import { restoreEnvVarRefs } from "./env-preserve.js";
import { applyConfigEnvVars } from "./env-vars.js";
import { readConfigIncludeFileWithGuards, resolveConfigIncludes } from "./includes.js";
import { checkConfigIntegrity, ConfigIntegrityError } from "./integrity-guard.js";
import {
  appendConfigWriteAuditRecord,
  hashConfigRaw,
  hasConfigMeta,
  maybeLoadDotEnvForConfig,
  maybeLoadShellEnvFallbackForConfig,
  maybeLoadShellEnvFallbackForNoConfig,
  normalizeDeps,
  parseConfigJson5,
  resolveConfigForRead,
  resolveConfigIncludesForRead,
  resolveConfigPathForDeps,
  resolveConfigWriteSuspiciousReasons,
  resolveGatewayMode,
  stampConfigVersion,
  tightenStateDirPermissionsIfNeeded,
  warnIfConfigFromFuture,
  type ConfigIoDeps,
  type ConfigWriteAuditRecord,
  type ConfigWriteAuditResult,
} from "./io-helpers.js";
import {
  collectChangedPaths,
  collectEnvRefPaths,
  createMergePatch,
  formatConfigValidationFailure,
  resolveConfigSnapshotHash,
  restoreEnvRefsFromMap,
  unsetPathForWrite,
  warnOnConfigMiskeys,
} from "./io-path-ops.js";
import { readConfigFileSnapshotInternal } from "./io-read.js";
import {
  loadLastKnownGoodConfig,
  saveLastKnownGoodConfig,
  setLastKnownGoodFallbackActive,
} from "./last-known-good.js";
import { applyMergePatch } from "./merge-patch.js";
import { normalizeExecSafeBinProfilesInConfig } from "./normalize-exec-safe-bin.js";
import { normalizeConfigPaths } from "./normalize-paths.js";
import { resolveDefaultConfigCandidates } from "./paths.js";
import { applyConfigOverrides } from "./runtime-overrides.js";
import type { DenebConfig, ConfigFileSnapshot } from "./types.js";
import {
  validateConfigObjectRawWithPlugins,
  validateConfigObjectWithPlugins,
} from "./validation.js";

export type ConfigWriteOptions = {
  /**
   * Read-time env snapshot used to validate `${VAR}` restoration decisions.
   * If omitted, write falls back to current process env.
   */
  envSnapshotForRestore?: Record<string, string | undefined>;
  /**
   * Optional safety check: only use envSnapshotForRestore when writing the
   * same config file path that produced the snapshot.
   */
  expectedConfigPath?: string;
  /**
   * Paths that must be explicitly removed from the persisted file payload,
   * even if schema/default normalization reintroduces them.
   */
  unsetPaths?: string[][];
  /**
   * Bypass config integrity guards (critical-key removal, bulk-key removal,
   * size-drop detection). Use only when the destructive write is intentional.
   * Can also be set via DENEB_CONFIG_FORCE_WRITE=1 environment variable.
   */
  force?: boolean;
};

export type ReadConfigFileSnapshotForWriteResult = {
  snapshot: ConfigFileSnapshot;
  writeOptions: ConfigWriteOptions;
};

const loggedInvalidConfigs = new Set<string>();

// Tracks pending auto-generated ownerDisplaySecret values that have not yet
// been persisted to disk, keyed by config path.
const AUTO_OWNER_DISPLAY_SECRET_BY_PATH = new Map<string, string>();
const AUTO_OWNER_DISPLAY_SECRET_PERSIST_IN_FLIGHT = new Set<string>();
const AUTO_OWNER_DISPLAY_SECRET_PERSIST_WARNED = new Set<string>();

/**
 * Attempt to recover from a config load failure using the last-known-good backup.
 * Returns a fully normalized DenebConfig on success, or null if recovery fails.
 */
function tryLastKnownGoodFallback(
  configPath: string,
  deps: Required<ConfigIoDeps>,
): DenebConfig | null {
  const lkg = loadLastKnownGoodConfig(configPath);
  if (!lkg) {
    return null;
  }
  // Re-validate the LKG config against the current schema.
  // If the schema changed across an upgrade, LKG may also be invalid.
  const lkgValidated = validateConfigObjectWithPlugins(lkg);
  if (!lkgValidated.ok) {
    deps.logger.warn(
      `Last-known-good config backup also failed validation — cannot recover automatically.`,
    );
    return null;
  }
  deps.logger.warn(
    `Config at ${configPath} is invalid. Using last-known-good backup. Fix your config and restart.`,
  );
  setLastKnownGoodFallbackActive(true);
  const cfg = applyTalkConfigNormalization(
    applyModelDefaults(
      applyContextPruningDefaults(
        applyAgentDefaults(
          applySessionDefaults(applyLoggingDefaults(applyMessageDefaults(lkgValidated.config))),
        ),
      ),
    ),
  );
  normalizeConfigPaths(cfg);
  normalizeExecSafeBinProfilesInConfig(cfg);
  applyConfigEnvVars(cfg, deps.env);
  return applyConfigOverrides(cfg);
}

export function createConfigIO(overrides: ConfigIoDeps = {}, clearCacheFn?: () => void) {
  const deps = normalizeDeps(overrides);
  const requestedConfigPath = resolveConfigPathForDeps(deps);
  const candidatePaths = deps.configPath
    ? [requestedConfigPath]
    : resolveDefaultConfigCandidates(deps.env, deps.homedir);
  const configPath =
    candidatePaths.find((candidate) => deps.fs.existsSync(candidate)) ?? requestedConfigPath;

  function loadConfig(): DenebConfig {
    try {
      maybeLoadDotEnvForConfig(deps.env);
      if (!deps.fs.existsSync(configPath)) {
        maybeLoadShellEnvFallbackForNoConfig(deps);
        return {};
      }
      const raw = deps.fs.readFileSync(configPath, "utf-8");
      const parsed = deps.json5.parse(raw);
      const readResolution = resolveConfigForRead(
        resolveConfigIncludesForRead(parsed, configPath, deps),
        deps.env,
      );
      const resolvedConfig = readResolution.resolvedConfigRaw;
      for (const w of readResolution.envWarnings) {
        deps.logger.warn(
          `Config (${configPath}): missing env var "${w.varName}" at ${w.configPath} — feature using this value will be unavailable`,
        );
      }
      warnOnConfigMiskeys(resolvedConfig, deps.logger);
      if (typeof resolvedConfig !== "object" || resolvedConfig === null) {
        const typeLabel = resolvedConfig === null ? "null" : typeof resolvedConfig;
        const error = new Error(
          `Invalid config at ${configPath}: expected an object at the root, got ${typeLabel}`,
        );
        (error as { code?: string }).code = "INVALID_CONFIG";
        throw error;
      }
      const preValidationDuplicates = findDuplicateAgentDirs(resolvedConfig as DenebConfig, {
        env: deps.env,
        homedir: deps.homedir,
      });
      if (preValidationDuplicates.length > 0) {
        throw new DuplicateAgentDirError(preValidationDuplicates);
      }
      const validated = validateConfigObjectWithPlugins(resolvedConfig);
      if (!validated.ok) {
        const details = validated.issues
          .map(
            (iss) =>
              `- ${sanitizeTerminalText(iss.path || "<root>")}: ${sanitizeTerminalText(iss.message)}`,
          )
          .join("\n");
        if (!loggedInvalidConfigs.has(configPath)) {
          loggedInvalidConfigs.add(configPath);
          deps.logger.error(`Invalid config at ${configPath}:\\n${details}`);
        }
        const error = new Error(`Invalid config at ${configPath}:\n${details}`);
        (error as { code?: string; details?: string }).code = "INVALID_CONFIG";
        (error as { code?: string; details?: string }).details = details;
        throw error;
      }
      if (validated.warnings.length > 0) {
        const details = validated.warnings
          .map(
            (iss) =>
              `- ${sanitizeTerminalText(iss.path || "<root>")}: ${sanitizeTerminalText(iss.message)}`,
          )
          .join("\n");
        deps.logger.warn(`Config warnings:\\n${details}`);
      }
      // Save the resolved config as last-known-good for recovery on future failures.
      saveLastKnownGoodConfig(configPath, resolvedConfig);
      setLastKnownGoodFallbackActive(false);
      warnIfConfigFromFuture(validated.config, deps.logger);
      const cfg = applyTalkConfigNormalization(
        applyModelDefaults(
          applyContextPruningDefaults(
            applyAgentDefaults(
              applySessionDefaults(applyLoggingDefaults(applyMessageDefaults(validated.config))),
            ),
          ),
        ),
      );
      normalizeConfigPaths(cfg);
      normalizeExecSafeBinProfilesInConfig(cfg);

      const duplicates = findDuplicateAgentDirs(cfg, {
        env: deps.env,
        homedir: deps.homedir,
      });
      if (duplicates.length > 0) {
        throw new DuplicateAgentDirError(duplicates);
      }

      applyConfigEnvVars(cfg, deps.env);
      maybeLoadShellEnvFallbackForConfig(cfg, deps);

      const pendingSecret = AUTO_OWNER_DISPLAY_SECRET_BY_PATH.get(configPath);
      const ownerDisplaySecretResolution = ensureOwnerDisplaySecret(
        cfg,
        () => pendingSecret ?? crypto.randomBytes(32).toString("hex"),
      );
      const cfgWithOwnerDisplaySecret = ownerDisplaySecretResolution.config;
      if (ownerDisplaySecretResolution.generatedSecret) {
        AUTO_OWNER_DISPLAY_SECRET_BY_PATH.set(
          configPath,
          ownerDisplaySecretResolution.generatedSecret,
        );
        if (!AUTO_OWNER_DISPLAY_SECRET_PERSIST_IN_FLIGHT.has(configPath)) {
          AUTO_OWNER_DISPLAY_SECRET_PERSIST_IN_FLIGHT.add(configPath);
          void writeConfigFile(cfgWithOwnerDisplaySecret, { expectedConfigPath: configPath })
            .then(() => {
              AUTO_OWNER_DISPLAY_SECRET_BY_PATH.delete(configPath);
              AUTO_OWNER_DISPLAY_SECRET_PERSIST_WARNED.delete(configPath);
            })
            .catch((err) => {
              if (!AUTO_OWNER_DISPLAY_SECRET_PERSIST_WARNED.has(configPath)) {
                AUTO_OWNER_DISPLAY_SECRET_PERSIST_WARNED.add(configPath);
                deps.logger.warn(
                  `Failed to persist auto-generated commands.ownerDisplaySecret at ${configPath}: ${String(err)}`,
                );
              }
            })
            .finally(() => {
              AUTO_OWNER_DISPLAY_SECRET_PERSIST_IN_FLIGHT.delete(configPath);
            });
        }
      } else {
        AUTO_OWNER_DISPLAY_SECRET_BY_PATH.delete(configPath);
        AUTO_OWNER_DISPLAY_SECRET_PERSIST_WARNED.delete(configPath);
      }

      return applyConfigOverrides(cfgWithOwnerDisplaySecret);
    } catch (err) {
      if (err instanceof DuplicateAgentDirError) {
        deps.logger.error(err.message);
        throw err;
      }
      const error = err as { code?: string };
      if (error?.code === "INVALID_CONFIG") {
        // Try last-known-good fallback before crashing.
        const lkgResult = tryLastKnownGoodFallback(configPath, deps);
        if (lkgResult) {
          return lkgResult;
        }
        throw err;
      }
      deps.logger.error(`Failed to read config at ${configPath}`, err);
      // Try last-known-good fallback for parse errors and other read failures.
      const lkgResult = tryLastKnownGoodFallback(configPath, deps);
      if (lkgResult) {
        return lkgResult;
      }
      throw err;
    }
  }

  async function readConfigFileSnapshot(): Promise<ConfigFileSnapshot> {
    const result = await readConfigFileSnapshotInternal(configPath, deps);
    return result.snapshot;
  }

  async function readConfigFileSnapshotForWrite(): Promise<ReadConfigFileSnapshotForWriteResult> {
    const result = await readConfigFileSnapshotInternal(configPath, deps);
    return {
      snapshot: result.snapshot,
      writeOptions: {
        envSnapshotForRestore: result.envSnapshotForRestore,
        expectedConfigPath: configPath,
      },
    };
  }

  async function writeConfigFile(cfg: DenebConfig, options: ConfigWriteOptions = {}) {
    clearCacheFn?.();
    let persistCandidate: unknown = cfg;
    const { snapshot } = await readConfigFileSnapshotInternal(configPath, deps);
    let envRefMap: Map<string, string> | null = null;
    let changedPaths: Set<string> | null = null;
    if (snapshot.valid && snapshot.exists) {
      const patch = createMergePatch(snapshot.config, cfg);
      persistCandidate = applyMergePatch(snapshot.resolved, patch);
      try {
        const resolvedIncludes = resolveConfigIncludes(snapshot.parsed, configPath, {
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
        const collected = new Map<string, string>();
        collectEnvRefPaths(resolvedIncludes, "", collected);
        if (collected.size > 0) {
          envRefMap = collected;
          changedPaths = new Set<string>();
          collectChangedPaths(snapshot.config, cfg, "", changedPaths);
        }
      } catch {
        envRefMap = null;
      }
    }

    const validated = validateConfigObjectRawWithPlugins(persistCandidate);
    if (!validated.ok) {
      const issue = validated.issues[0];
      const pathLabel = issue?.path ? issue.path : "<root>";
      const issueMessage = issue?.message ?? "invalid";
      throw new Error(formatConfigValidationFailure(pathLabel, issueMessage));
    }
    if (validated.warnings.length > 0) {
      const details = validated.warnings
        .map((warning) => `- ${warning.path}: ${warning.message}`)
        .join("\n");
      deps.logger.warn(`Config warnings:\n${details}`);
    }

    // Restore ${VAR} env var references that were resolved during config loading.
    // Read the current file (pre-substitution) and restore any references whose
    // resolved values match the incoming config — so we don't overwrite
    // "${ANTHROPIC_API_KEY}" with "sk-ant-..." when the caller didn't change it.
    //
    // We use only the root file's parsed content (no $include resolution) to avoid
    // pulling values from included files into the root config on write-back.
    // Apply env restoration to validated.config (which has runtime defaults stripped
    // per issue #6070) rather than the raw caller input.
    let cfgToWrite = validated.config;
    try {
      if (deps.fs.existsSync(configPath)) {
        const currentRaw = await deps.fs.promises.readFile(configPath, "utf-8");
        const parsedRes = parseConfigJson5(currentRaw, deps.json5);
        if (parsedRes.ok) {
          // Use env snapshot from when config was loaded (if available) to avoid
          // TOCTOU issues where env changes between load and write. Falls back to
          // live env if no snapshot exists (e.g., first write before any load).
          const envForRestore = options.envSnapshotForRestore ?? deps.env;
          cfgToWrite = restoreEnvVarRefs(
            cfgToWrite,
            parsedRes.parsed,
            envForRestore,
          ) as DenebConfig;
        }
      }
    } catch {
      // If reading the current file fails, write cfg as-is (no env restoration)
    }

    const dir = path.dirname(configPath);
    await deps.fs.promises.mkdir(dir, { recursive: true, mode: 0o700 });
    await tightenStateDirPermissionsIfNeeded({
      configPath,
      env: deps.env,
      homedir: deps.homedir,
      fsModule: deps.fs,
    });
    const outputConfigBase =
      envRefMap && changedPaths
        ? (restoreEnvRefsFromMap(cfgToWrite, "", envRefMap, changedPaths) as DenebConfig)
        : cfgToWrite;
    let outputConfig = outputConfigBase;
    if (options.unsetPaths?.length) {
      for (const unsetPath of options.unsetPaths) {
        if (!Array.isArray(unsetPath) || unsetPath.length === 0) {
          continue;
        }
        const unsetResult = unsetPathForWrite(outputConfig, unsetPath);
        if (unsetResult.changed) {
          outputConfig = unsetResult.next;
        }
      }
    }
    // Do NOT apply runtime defaults when writing — user config should only contain
    // explicitly set values. Runtime defaults are applied when loading (issue #6070).
    const stampedOutputConfig = stampConfigVersion(outputConfig);
    const json = JSON.stringify(stampedOutputConfig, null, 2).trimEnd().concat("\n");
    const nextHash = hashConfigRaw(json);
    const previousHash = resolveConfigSnapshotHash(snapshot);
    const changedPathCount = changedPaths?.size;
    const previousBytes =
      typeof snapshot.raw === "string" ? Buffer.byteLength(snapshot.raw, "utf-8") : null;
    const nextBytes = Buffer.byteLength(json, "utf-8");
    const hasMetaBefore = hasConfigMeta(snapshot.parsed);
    const hasMetaAfter = hasConfigMeta(stampedOutputConfig);
    const gatewayModeBefore = resolveGatewayMode(snapshot.resolved);
    const gatewayModeAfter = resolveGatewayMode(stampedOutputConfig);
    const suspiciousReasons = resolveConfigWriteSuspiciousReasons({
      existsBefore: snapshot.exists,
      previousBytes,
      nextBytes,
      hasMetaBefore,
      gatewayModeBefore,
      gatewayModeAfter,
    });
    // --- Config integrity guard: reject destructive writes unless forced ---
    const forceWrite = options.force || deps.env.DENEB_CONFIG_FORCE_WRITE === "1";
    if (snapshot.exists && !forceWrite) {
      const violations = checkConfigIntegrity({
        previous: snapshot.resolved as Record<string, unknown>,
        next: stampedOutputConfig as Record<string, unknown>,
        previousBytes,
        nextBytes,
      });
      if (violations.length > 0) {
        deps.logger.warn(
          `Config integrity guard blocked write: ${violations.map((v) => v.code).join(", ")}`,
        );
        throw new ConfigIntegrityError(violations);
      }
    }

    const logConfigOverwrite = () => {
      if (!snapshot.exists) {
        return;
      }
      const isVitest = deps.env.VITEST === "true";
      const shouldLogInVitest = deps.env.DENEB_TEST_CONFIG_OVERWRITE_LOG === "1";
      if (isVitest && !shouldLogInVitest) {
        return;
      }
      const changeSummary =
        typeof changedPathCount === "number" ? `, changedPaths=${changedPathCount}` : "";
      deps.logger.warn(
        `Config overwrite: ${configPath} (sha256 ${previousHash ?? "unknown"} -> ${nextHash}, backup=${configPath}.bak${changeSummary})`,
      );
    };
    const logConfigWriteAnomalies = () => {
      if (suspiciousReasons.length === 0) {
        return;
      }
      // Tests often write minimal configs (missing meta, etc); keep output quiet unless requested.
      const isVitest = deps.env.VITEST === "true";
      const shouldLogInVitest = deps.env.DENEB_TEST_CONFIG_WRITE_ANOMALY_LOG === "1";
      if (isVitest && !shouldLogInVitest) {
        return;
      }
      deps.logger.warn(`Config write anomaly: ${configPath} (${suspiciousReasons.join(", ")})`);
    };
    const auditRecordBase = {
      ts: new Date().toISOString(),
      source: "config-io" as const,
      event: "config.write" as const,
      configPath,
      pid: process.pid,
      ppid: process.ppid,
      cwd: process.cwd(),
      argv: process.argv.slice(0, 8),
      execArgv: process.execArgv.slice(0, 8),
      watchMode: deps.env.DENEB_WATCH_MODE === "1",
      watchSession:
        typeof deps.env.DENEB_WATCH_SESSION === "string" &&
        deps.env.DENEB_WATCH_SESSION.trim().length > 0
          ? deps.env.DENEB_WATCH_SESSION.trim()
          : null,
      watchCommand:
        typeof deps.env.DENEB_WATCH_COMMAND === "string" &&
        deps.env.DENEB_WATCH_COMMAND.trim().length > 0
          ? deps.env.DENEB_WATCH_COMMAND.trim()
          : null,
      existsBefore: snapshot.exists,
      previousHash: previousHash ?? null,
      nextHash,
      previousBytes,
      nextBytes,
      changedPathCount: typeof changedPathCount === "number" ? changedPathCount : null,
      hasMetaBefore,
      hasMetaAfter,
      gatewayModeBefore,
      gatewayModeAfter,
      suspicious: suspiciousReasons,
    };
    const appendWriteAudit = async (result: ConfigWriteAuditResult, err?: unknown) => {
      const errorCode =
        err && typeof err === "object" && "code" in err && typeof err.code === "string"
          ? err.code
          : undefined;
      const errorMessage =
        err && typeof err === "object" && "message" in err && typeof err.message === "string"
          ? err.message
          : undefined;
      await appendConfigWriteAuditRecord(deps, {
        ...(auditRecordBase as ConfigWriteAuditRecord),
        result,
        nextHash: result === "failed" ? null : auditRecordBase.nextHash,
        nextBytes: result === "failed" ? null : auditRecordBase.nextBytes,
        errorCode,
        errorMessage,
      });
    };

    const tmp = path.join(
      dir,
      `${path.basename(configPath)}.${process.pid}.${crypto.randomUUID()}.tmp`,
    );

    try {
      await deps.fs.promises.writeFile(tmp, json, {
        encoding: "utf-8",
        mode: 0o600,
      });

      if (deps.fs.existsSync(configPath)) {
        await maintainConfigBackups(configPath, deps.fs.promises);
      }

      try {
        await deps.fs.promises.rename(tmp, configPath);
      } catch (err) {
        const code = (err as { code?: string }).code;
        // Windows doesn't reliably support atomic replace via rename when dest exists.
        if (code === "EPERM" || code === "EEXIST") {
          await deps.fs.promises.copyFile(tmp, configPath);
          await deps.fs.promises.chmod(configPath, 0o600).catch(() => {
            // best-effort
          });
          await deps.fs.promises.unlink(tmp).catch(() => {
            // best-effort
          });
          logConfigOverwrite();
          logConfigWriteAnomalies();
          await appendWriteAudit("copy-fallback");
          return;
        }
        await deps.fs.promises.unlink(tmp).catch(() => {
          // best-effort
        });
        throw err;
      }
      logConfigOverwrite();
      logConfigWriteAnomalies();
      await appendWriteAudit("rename");
    } catch (err) {
      await appendWriteAudit("failed", err);
      throw err;
    }
  }

  return {
    configPath,
    loadConfig,
    readConfigFileSnapshot,
    readConfigFileSnapshotForWrite,
    writeConfigFile,
  };
}
