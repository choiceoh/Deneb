/**
 * Exported `run*` command handlers for config subcommands:
 * runConfigSet, runConfigGet, runConfigUnset, runConfigFile, runConfigValidate.
 */

import type { DenebConfig } from "../config/config.js";
import { readConfigFileSnapshot, writeConfigFile } from "../config/config.js";
import { formatConfigIssueLines, normalizeConfigIssues } from "../config/issue-format.js";
import { CONFIG_PATH } from "../config/paths.js";
import { redactConfigObject } from "../config/redact-snapshot.js";
import { danger, info, success } from "../globals.js";
import type { RuntimeEnv } from "../runtime.js";
import { defaultRuntime } from "../runtime.js";
import { shortenHomePath } from "../utils.js";
import { formatCliCommand } from "./command-format.js";
import {
  collectDryRunRefs,
  collectDryRunResolvabilityErrors,
  collectDryRunSchemaErrors,
  collectDryRunStaticErrorsForSkippedExecRefs,
  formatDryRunFailureMessage,
  selectDryRunRefsForResolution,
} from "./config-cli-dryrun.js";
import {
  buildSingleSetOperations,
  ensureValidOllamaProviderForApiKeySet,
  parseBatchOperations,
  type ConfigSetOperation,
} from "./config-cli-ops.js";
import { getAtPath, parseRequiredPath, setAtPath, unsetAtPath } from "./config-cli-path.js";
import type { ConfigSetDryRunResult } from "./config-set-dryrun.js";
import {
  hasBatchMode,
  hasProviderBuilderOptions,
  hasRefBuilderOptions,
  parseBatchSource,
  type ConfigSetOptions,
} from "./config-set-input.js";
import { resolveConfigSetMode } from "./config-set-parser.js";

function formatDoctorHint(message: string): string {
  return `Run \`${formatCliCommand("deneb doctor")}\` ${message}`;
}

async function loadValidConfig(runtime: RuntimeEnv = defaultRuntime) {
  const snapshot = await readConfigFileSnapshot();
  if (snapshot.valid) {
    return snapshot;
  }
  runtime.error(`Config invalid at ${shortenHomePath(snapshot.path)}.`);
  for (const line of formatConfigIssueLines(snapshot.issues, "-", { normalizeRoot: true })) {
    runtime.error(line);
  }
  runtime.error(formatDoctorHint("to repair, then retry."));
  runtime.exit(1);
  return snapshot;
}

class ConfigSetDryRunValidationError extends Error {
  constructor(readonly result: ConfigSetDryRunResult) {
    super("config set dry-run validation failed");
    this.name = "ConfigSetDryRunValidationError";
  }
}

function modeError(message: string): Error {
  return new Error(`config set mode error: ${message}`);
}

export async function runConfigSet(opts: {
  path?: string;
  value?: string;
  cliOptions: ConfigSetOptions;
  runtime?: RuntimeEnv;
}) {
  const runtime = opts.runtime ?? defaultRuntime;
  try {
    const isBatchMode = hasBatchMode(opts.cliOptions);
    const modeResolution = resolveConfigSetMode({
      hasBatchMode: isBatchMode,
      hasRefBuilderOptions: hasRefBuilderOptions(opts.cliOptions),
      hasProviderBuilderOptions: hasProviderBuilderOptions(opts.cliOptions),
      strictJson: Boolean(opts.cliOptions.strictJson || opts.cliOptions.json),
    });
    if (!modeResolution.ok) {
      throw modeError(modeResolution.error);
    }
    if (opts.cliOptions.allowExec && !opts.cliOptions.dryRun) {
      throw modeError("--allow-exec requires --dry-run.");
    }

    const batchEntries = parseBatchSource(opts.cliOptions);
    if (batchEntries) {
      if (opts.path !== undefined || opts.value !== undefined) {
        throw modeError("batch mode does not accept <path> or <value> arguments.");
      }
    }
    const operations: ConfigSetOperation[] = batchEntries
      ? parseBatchOperations(batchEntries)
      : buildSingleSetOperations({
          path: opts.path,
          value: opts.value,
          opts: opts.cliOptions,
        });
    const snapshot = await loadValidConfig(runtime);
    // Use snapshot.resolved (config after $include and ${ENV} resolution, but BEFORE runtime defaults)
    // instead of snapshot.config (runtime-merged with defaults).
    // This prevents runtime defaults from leaking into the written config file (issue #6070)
    const next = structuredClone(snapshot.resolved) as Record<string, unknown>;
    for (const operation of operations) {
      ensureValidOllamaProviderForApiKeySet(next, operation.setPath);
      setAtPath(next, operation.setPath, operation.value);
    }
    const nextConfig = next as DenebConfig;

    if (opts.cliOptions.dryRun) {
      const hasJsonMode = operations.some((operation) => operation.inputMode === "json");
      const hasBuilderMode = operations.some((operation) => operation.inputMode === "builder");
      const refs =
        hasJsonMode || hasBuilderMode
          ? collectDryRunRefs({
              config: nextConfig,
              operations,
            })
          : [];
      const selectedDryRunRefs = selectDryRunRefsForResolution({
        refs,
        allowExecInDryRun: Boolean(opts.cliOptions.allowExec),
      });
      const errors = [];
      if (hasJsonMode) {
        errors.push(...collectDryRunSchemaErrors(nextConfig));
      }
      if (hasJsonMode || hasBuilderMode) {
        errors.push(
          ...collectDryRunStaticErrorsForSkippedExecRefs({
            refs: selectedDryRunRefs.skippedExecRefs,
            config: nextConfig,
          }),
        );
        errors.push(
          ...(await collectDryRunResolvabilityErrors({
            refs: selectedDryRunRefs.refsToResolve,
            config: nextConfig,
          })),
        );
      }
      const dryRunResult: ConfigSetDryRunResult = {
        ok: errors.length === 0,
        operations: operations.length,
        configPath: shortenHomePath(snapshot.path),
        inputModes: [...new Set(operations.map((operation) => operation.inputMode))],
        checks: {
          schema: hasJsonMode,
          resolvability: hasJsonMode || hasBuilderMode,
          resolvabilityComplete:
            (hasJsonMode || hasBuilderMode) && selectedDryRunRefs.skippedExecRefs.length === 0,
        },
        refsChecked: selectedDryRunRefs.refsToResolve.length,
        skippedExecRefs: selectedDryRunRefs.skippedExecRefs.length,
        ...(errors.length > 0 ? { errors } : {}),
      };
      if (errors.length > 0) {
        if (opts.cliOptions.json) {
          throw new ConfigSetDryRunValidationError(dryRunResult);
        }
        throw new Error(
          formatDryRunFailureMessage({
            errors,
            skippedExecRefs: selectedDryRunRefs.skippedExecRefs.length,
          }),
        );
      }
      if (opts.cliOptions.json) {
        runtime.log(JSON.stringify(dryRunResult, null, 2));
      } else {
        if (!dryRunResult.checks.schema && !dryRunResult.checks.resolvability) {
          runtime.log(
            info(
              "Dry run note: value mode does not run schema/resolvability checks. Use --strict-json, builder flags, or batch mode to enable validation checks.",
            ),
          );
        }
        if (dryRunResult.skippedExecRefs > 0) {
          runtime.log(
            info(
              `Dry run note: skipped ${dryRunResult.skippedExecRefs} exec SecretRef resolvability check(s). Re-run with --allow-exec to execute exec providers during dry-run.`,
            ),
          );
        }
        runtime.log(
          info(
            `Dry run successful: ${operations.length} update(s) validated against ${shortenHomePath(snapshot.path)}.`,
          ),
        );
      }
      return;
    }

    await writeConfigFile(next);
    if (operations.length === 1) {
      runtime.log(
        info(
          `Updated ${operations[0]?.requestedPath.join(".") ?? ""}. Restart the gateway to apply.`,
        ),
      );
      return;
    }
    runtime.log(info(`Updated ${operations.length} config paths. Restart the gateway to apply.`));
  } catch (err) {
    if (
      opts.cliOptions.dryRun &&
      opts.cliOptions.json &&
      err instanceof ConfigSetDryRunValidationError
    ) {
      runtime.log(JSON.stringify(err.result, null, 2));
      runtime.exit(1);
      return;
    }
    runtime.error(danger(String(err)));
    runtime.exit(1);
  }
}

export async function runConfigGet(opts: { path: string; json?: boolean; runtime?: RuntimeEnv }) {
  const runtime = opts.runtime ?? defaultRuntime;
  try {
    const parsedPath = parseRequiredPath(opts.path);
    const snapshot = await loadValidConfig(runtime);
    const redacted = redactConfigObject(snapshot.config);
    const res = getAtPath(redacted, parsedPath);
    if (!res.found) {
      runtime.error(danger(`Config path not found: ${opts.path}`));
      runtime.exit(1);
      return;
    }
    if (opts.json) {
      runtime.log(JSON.stringify(res.value ?? null, null, 2));
      return;
    }
    if (
      typeof res.value === "string" ||
      typeof res.value === "number" ||
      typeof res.value === "boolean"
    ) {
      runtime.log(String(res.value));
      return;
    }
    runtime.log(JSON.stringify(res.value ?? null, null, 2));
  } catch (err) {
    runtime.error(danger(String(err)));
    runtime.exit(1);
  }
}

export async function runConfigUnset(opts: { path: string; runtime?: RuntimeEnv }) {
  const runtime = opts.runtime ?? defaultRuntime;
  try {
    const parsedPath = parseRequiredPath(opts.path);
    const snapshot = await loadValidConfig(runtime);
    // Use snapshot.resolved (config after $include and ${ENV} resolution, but BEFORE runtime defaults)
    // instead of snapshot.config (runtime-merged with defaults).
    // This prevents runtime defaults from leaking into the written config file (issue #6070)
    const next = structuredClone(snapshot.resolved) as Record<string, unknown>;
    const removed = unsetAtPath(next, parsedPath);
    if (!removed) {
      runtime.error(danger(`Config path not found: ${opts.path}`));
      runtime.exit(1);
      return;
    }
    await writeConfigFile(next, { unsetPaths: [parsedPath] });
    runtime.log(info(`Removed ${opts.path}. Restart the gateway to apply.`));
  } catch (err) {
    runtime.error(danger(String(err)));
    runtime.exit(1);
  }
}

export async function runConfigFile(opts: { runtime?: RuntimeEnv }) {
  const runtime = opts.runtime ?? defaultRuntime;
  try {
    const snapshot = await readConfigFileSnapshot();
    runtime.log(shortenHomePath(snapshot.path));
  } catch (err) {
    runtime.error(danger(String(err)));
    runtime.exit(1);
  }
}

export async function runConfigValidate(opts: { json?: boolean; runtime?: RuntimeEnv } = {}) {
  const runtime = opts.runtime ?? defaultRuntime;
  let outputPath = CONFIG_PATH ?? "deneb.json";

  try {
    const snapshot = await readConfigFileSnapshot();
    outputPath = snapshot.path;
    const shortPath = shortenHomePath(outputPath);

    if (!snapshot.exists) {
      if (opts.json) {
        runtime.log(JSON.stringify({ valid: false, path: outputPath, error: "file not found" }));
      } else {
        runtime.error(danger(`Config file not found: ${shortPath}`));
      }
      runtime.exit(1);
      return;
    }

    if (!snapshot.valid) {
      const issues = normalizeConfigIssues(snapshot.issues);

      if (opts.json) {
        runtime.log(JSON.stringify({ valid: false, path: outputPath, issues }, null, 2));
      } else {
        runtime.error(danger(`Config invalid at ${shortPath}:`));
        for (const line of formatConfigIssueLines(issues, danger("×"), { normalizeRoot: true })) {
          runtime.error(`  ${line}`);
        }
        runtime.error("");
        runtime.error(formatDoctorHint("to repair, or fix the keys above manually."));
      }
      runtime.exit(1);
      return;
    }

    if (opts.json) {
      runtime.log(JSON.stringify({ valid: true, path: outputPath }));
    } else {
      runtime.log(success(`Config valid: ${shortPath}`));
    }
  } catch (err) {
    if (opts.json) {
      runtime.log(JSON.stringify({ valid: false, path: outputPath, error: String(err) }));
    } else {
      runtime.error(danger(`Config validation error: ${String(err)}`));
    }
    runtime.exit(1);
  }
}
