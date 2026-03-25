/**
 * Standalone implementation of readConfigFileSnapshotInternal.
 * Extracted from io-create.ts to keep file sizes manageable.
 */
import {
  applyAgentDefaults,
  applyContextPruningDefaults,
  applyLoggingDefaults,
  applyMessageDefaults,
  applyModelDefaults,
  applySessionDefaults,
  applyTalkApiKey,
  applyTalkConfigNormalization,
} from "./defaults.js";
import { ConfigIncludeError } from "./includes.js";
import {
  hashConfigRaw,
  maybeLoadDotEnvForConfig,
  parseConfigJson5,
  resolveConfigForRead,
  resolveConfigIncludesForRead,
  warnIfConfigFromFuture,
  type ConfigIoDeps,
} from "./io-helpers.js";
import { coerceConfig } from "./io-path-ops.js";
import { findLegacyConfigIssues } from "./legacy.js";
import { normalizeExecSafeBinProfilesInConfig } from "./normalize-exec-safe-bin.js";
import { normalizeConfigPaths } from "./normalize-paths.js";
import type { ConfigFileSnapshot, LegacyConfigIssue } from "./types.js";
import { validateConfigObjectWithPlugins } from "./validation.js";

export type ReadConfigFileSnapshotInternalResult = {
  snapshot: ConfigFileSnapshot;
  envSnapshotForRestore?: Record<string, string | undefined>;
};

export async function readConfigFileSnapshotInternal(
  configPath: string,
  deps: Required<ConfigIoDeps>,
): Promise<ReadConfigFileSnapshotInternalResult> {
  maybeLoadDotEnvForConfig(deps.env);
  const exists = deps.fs.existsSync(configPath);
  if (!exists) {
    const hash = hashConfigRaw(null);
    const config = applyTalkApiKey(
      applyTalkConfigNormalization(
        applyModelDefaults(
          applyContextPruningDefaults(
            applyAgentDefaults(applySessionDefaults(applyMessageDefaults({}))),
          ),
        ),
      ),
    );
    const legacyIssues: LegacyConfigIssue[] = [];
    return {
      snapshot: {
        path: configPath,
        exists: false,
        raw: null,
        parsed: {},
        resolved: {},
        valid: true,
        config,
        hash,
        issues: [],
        warnings: [],
        legacyIssues,
      },
    };
  }

  try {
    const raw = deps.fs.readFileSync(configPath, "utf-8");
    const hash = hashConfigRaw(raw);
    const parsedRes = parseConfigJson5(raw, deps.json5);
    if (!parsedRes.ok) {
      return {
        snapshot: {
          path: configPath,
          exists: true,
          raw,
          parsed: {},
          resolved: {},
          valid: false,
          config: {},
          hash,
          issues: [{ path: "", message: `JSON5 parse failed: ${parsedRes.error}` }],
          warnings: [],
          legacyIssues: [],
        },
      };
    }

    // Resolve $include directives
    let resolved: unknown;
    try {
      resolved = resolveConfigIncludesForRead(parsedRes.parsed, configPath, deps);
    } catch (err) {
      const message =
        err instanceof ConfigIncludeError
          ? err.message
          : `Include resolution failed: ${String(err)}`;
      return {
        snapshot: {
          path: configPath,
          exists: true,
          raw,
          parsed: parsedRes.parsed,
          resolved: coerceConfig(parsedRes.parsed),
          valid: false,
          config: coerceConfig(parsedRes.parsed),
          hash,
          issues: [{ path: "", message }],
          warnings: [],
          legacyIssues: [],
        },
      };
    }

    const readResolution = resolveConfigForRead(resolved, deps.env);

    // Convert missing env var references to config warnings instead of fatal errors.
    // This allows the gateway to start in degraded mode when non-critical config
    // sections reference unset env vars (e.g. optional provider API keys).
    const envVarWarnings = readResolution.envWarnings.map((w) => ({
      path: w.configPath,
      message: `Missing env var "${w.varName}" — feature using this value will be unavailable`,
    }));

    const resolvedConfigRaw = readResolution.resolvedConfigRaw;

    if (typeof resolvedConfigRaw !== "object" || resolvedConfigRaw === null) {
      const typeLabel = resolvedConfigRaw === null ? "null" : typeof resolvedConfigRaw;
      return {
        snapshot: {
          path: configPath,
          exists: true,
          raw,
          parsed: parsedRes.parsed,
          resolved: {},
          valid: false,
          config: {},
          hash,
          issues: [
            {
              path: "",
              message: `Expected an object at the root, got ${typeLabel}`,
            },
          ],
          warnings: [...envVarWarnings],
          legacyIssues: [],
        },
      };
    }

    // Detect legacy keys on resolved config, but only mark source-literal legacy
    // entries (for auto-migration) when they are present in the parsed source.
    const legacyIssues = findLegacyConfigIssues(resolvedConfigRaw, parsedRes.parsed);

    const validated = validateConfigObjectWithPlugins(resolvedConfigRaw);
    if (!validated.ok) {
      return {
        snapshot: {
          path: configPath,
          exists: true,
          raw,
          parsed: parsedRes.parsed,
          resolved: coerceConfig(resolvedConfigRaw),
          valid: false,
          config: coerceConfig(resolvedConfigRaw),
          hash,
          issues: validated.issues,
          warnings: [...validated.warnings, ...envVarWarnings],
          legacyIssues,
        },
      };
    }

    warnIfConfigFromFuture(validated.config, deps.logger);
    const snapshotConfig = normalizeConfigPaths(
      applyTalkApiKey(
        applyTalkConfigNormalization(
          applyModelDefaults(
            applyAgentDefaults(
              applySessionDefaults(applyLoggingDefaults(applyMessageDefaults(validated.config))),
            ),
          ),
        ),
      ),
    );
    normalizeExecSafeBinProfilesInConfig(snapshotConfig);
    return {
      snapshot: {
        path: configPath,
        exists: true,
        raw,
        parsed: parsedRes.parsed,
        // Use resolvedConfigRaw (after $include and ${ENV} substitution but BEFORE runtime defaults)
        // for config set/unset operations (issue #6070)
        resolved: coerceConfig(resolvedConfigRaw),
        valid: true,
        config: snapshotConfig,
        hash,
        issues: [],
        warnings: [...validated.warnings, ...envVarWarnings],
        legacyIssues,
      },
      envSnapshotForRestore: readResolution.envSnapshotForRestore,
    };
  } catch (err) {
    const nodeErr = err as NodeJS.ErrnoException;
    let message: string;
    if (nodeErr?.code === "EACCES") {
      // Permission denied — common in Docker/container deployments where the
      // config file is owned by root but the gateway runs as a non-root user.
      const uid = process.getuid?.();
      const uidHint = typeof uid === "number" ? String(uid) : "$(id -u)";
      message = [
        `read failed: ${String(err)}`,
        ``,
        `Config file is not readable by the current process. If running in a container`,
        `or 1-click deployment, fix ownership with:`,
        `  chown ${uidHint} "${configPath}"`,
        `Then restart the gateway.`,
      ].join("\n");
      deps.logger.error(message);
    } else {
      message = `read failed: ${String(err)}`;
    }
    return {
      snapshot: {
        path: configPath,
        exists: true,
        raw: null,
        parsed: {},
        resolved: {},
        valid: false,
        config: {},
        hash: hashConfigRaw(null),
        issues: [{ path: "", message }],
        warnings: [],
        legacyIssues: [],
      },
    };
  }
}
