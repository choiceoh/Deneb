/**
 * Dry-run validation helpers for `config set --dry-run`.
 * Collects schema errors, resolvability errors, and formats failure messages.
 */

import type { DenebConfig } from "../config/config.js";
import { formatConfigIssueLines } from "../config/issue-format.js";
import { resolveSecretInputRef, type SecretRef } from "../config/types.secrets.js";
import { validateConfigObjectRaw } from "../config/validation.js";
import {
  formatExecSecretRefIdValidationMessage,
  isValidExecSecretRefId,
  secretRefKey,
} from "../secrets/ref-contract.js";
import { resolveSecretRefValue } from "../secrets/resolve.js";
import { discoverConfigSecretTargets } from "../secrets/target-registry.js";
import type { ConfigSetOperation } from "./config-cli-ops.js";
import type { ConfigSetDryRunError, ConfigSetDryRunResult } from "./config-set-dryrun.js";

export function collectDryRunRefs(params: {
  config: DenebConfig;
  operations: ConfigSetOperation[];
}): SecretRef[] {
  const refsByKey = new Map<string, SecretRef>();
  const targetPaths = new Set<string>();
  const providerAliases = new Set<string>();

  for (const operation of params.operations) {
    if (operation.assignedRef) {
      refsByKey.set(secretRefKey(operation.assignedRef), operation.assignedRef);
    }
    if (operation.touchedSecretTargetPath) {
      targetPaths.add(operation.touchedSecretTargetPath);
    }
    if (operation.touchedProviderAlias) {
      providerAliases.add(operation.touchedProviderAlias);
    }
  }

  if (targetPaths.size === 0 && providerAliases.size === 0) {
    return [...refsByKey.values()];
  }

  const defaults = params.config.secrets?.defaults;
  for (const target of discoverConfigSecretTargets(params.config)) {
    const { ref } = resolveSecretInputRef({
      value: target.value,
      refValue: target.refValue,
      defaults,
    });
    if (!ref) {
      continue;
    }
    if (targetPaths.has(target.path) || providerAliases.has(ref.provider)) {
      refsByKey.set(secretRefKey(ref), ref);
    }
  }
  return [...refsByKey.values()];
}

export async function collectDryRunResolvabilityErrors(params: {
  refs: SecretRef[];
  config: DenebConfig;
}): Promise<ConfigSetDryRunError[]> {
  const failures: ConfigSetDryRunError[] = [];
  for (const ref of params.refs) {
    try {
      await resolveSecretRefValue(ref, {
        config: params.config,
        env: process.env,
      });
    } catch (err) {
      failures.push({
        kind: "resolvability",
        message: String(err),
        ref: `${ref.source}:${ref.provider}:${ref.id}`,
      });
    }
  }
  return failures;
}

export function collectDryRunStaticErrorsForSkippedExecRefs(params: {
  refs: SecretRef[];
  config: DenebConfig;
}): ConfigSetDryRunError[] {
  const failures: ConfigSetDryRunError[] = [];
  for (const ref of params.refs) {
    const id = ref.id.trim();
    const refLabel = `${ref.source}:${ref.provider}:${id}`;
    if (!id) {
      failures.push({
        kind: "resolvability",
        message: "Error: Secret reference id is empty.",
        ref: refLabel,
      });
      continue;
    }
    if (!isValidExecSecretRefId(id)) {
      failures.push({
        kind: "resolvability",
        message: `Error: ${formatExecSecretRefIdValidationMessage()} (ref: ${refLabel}).`,
        ref: refLabel,
      });
      continue;
    }
    const providerConfig = params.config.secrets?.providers?.[ref.provider];
    if (!providerConfig) {
      failures.push({
        kind: "resolvability",
        message: `Error: Secret provider "${ref.provider}" is not configured (ref: ${refLabel}).`,
        ref: refLabel,
      });
      continue;
    }
    if (providerConfig.source !== ref.source) {
      failures.push({
        kind: "resolvability",
        message: `Error: Secret provider "${ref.provider}" has source "${providerConfig.source}" but ref requests "${ref.source}".`,
        ref: refLabel,
      });
    }
  }
  return failures;
}

export function selectDryRunRefsForResolution(params: {
  refs: SecretRef[];
  allowExecInDryRun: boolean;
}): {
  refsToResolve: SecretRef[];
  skippedExecRefs: SecretRef[];
} {
  const refsToResolve: SecretRef[] = [];
  const skippedExecRefs: SecretRef[] = [];
  for (const ref of params.refs) {
    if (ref.source === "exec" && !params.allowExecInDryRun) {
      skippedExecRefs.push(ref);
      continue;
    }
    refsToResolve.push(ref);
  }
  return { refsToResolve, skippedExecRefs };
}

export function collectDryRunSchemaErrors(config: DenebConfig): ConfigSetDryRunError[] {
  const validated = validateConfigObjectRaw(config);
  if (validated.ok) {
    return [];
  }
  return formatConfigIssueLines(validated.issues, "-", { normalizeRoot: true }).map((message) => ({
    kind: "schema" as const,
    message,
  }));
}

export function formatDryRunFailureMessage(params: {
  errors: ConfigSetDryRunError[];
  skippedExecRefs: number;
}): string {
  const { errors, skippedExecRefs } = params;
  const schemaErrors = errors.filter((error) => error.kind === "schema");
  const resolveErrors = errors.filter((error) => error.kind === "resolvability");
  const lines: string[] = [];
  if (schemaErrors.length > 0) {
    lines.push("Dry run failed: config schema validation failed.");
    lines.push(...schemaErrors.map((error) => `- ${error.message}`));
  }
  if (resolveErrors.length > 0) {
    lines.push(
      `Dry run failed: ${resolveErrors.length} SecretRef assignment(s) could not be resolved.`,
    );
    lines.push(
      ...resolveErrors
        .slice(0, 5)
        .map((error) => `- ${error.ref ?? "<unknown-ref>"} -> ${error.message}`),
    );
    if (resolveErrors.length > 5) {
      lines.push(`- ... ${resolveErrors.length - 5} more`);
    }
  }
  if (skippedExecRefs > 0) {
    lines.push(
      `Dry run note: skipped ${skippedExecRefs} exec SecretRef resolvability check(s). Re-run with --allow-exec to execute exec providers during dry-run.`,
    );
  }
  return lines.join("\n");
}

export type { ConfigSetDryRunError, ConfigSetDryRunResult };
