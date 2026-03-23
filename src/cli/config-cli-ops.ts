/**
 * Operation building logic for `config set`: constructs ConfigSetOperation
 * values from CLI options (value mode, ref builder, provider builder, batch).
 */

import {
  coerceSecretRef,
  isValidEnvSecretRefId,
  type SecretProviderConfig,
  type SecretRef,
  type SecretRefSource,
} from "../config/types.secrets.js";
import { SecretProviderSchema } from "../config/zod-schema.core.js";
import {
  formatExecSecretRefIdValidationMessage,
  isValidFileSecretRefId,
  isValidSecretProviderAlias,
  validateExecSecretRefId,
} from "../secrets/ref-contract.js";
import { resolveConfigSecretTargetByPath } from "../secrets/target-registry.js";
import {
  getAtPath,
  parseRequiredPath,
  parseValue,
  pathEquals,
  setAtPath,
  toDotPath,
  type PathSegment,
} from "./config-cli-path.js";
import type { ConfigSetDryRunInputMode } from "./config-set-dryrun.js";
import {
  hasBatchMode,
  hasProviderBuilderOptions,
  hasRefBuilderOptions,
  parseBatchSource,
  type ConfigSetBatchEntry,
  type ConfigSetOptions,
} from "./config-set-input.js";
import { resolveConfigSetMode } from "./config-set-parser.js";

const OLLAMA_DEFAULT_BASE_URL = "http://localhost:11434";
const OLLAMA_API_KEY_PATH: PathSegment[] = ["models", "providers", "ollama", "apiKey"];
const OLLAMA_PROVIDER_PATH: PathSegment[] = ["models", "providers", "ollama"];
const SECRET_PROVIDER_PATH_PREFIX: PathSegment[] = ["secrets", "providers"];

export type ConfigSetInputMode = ConfigSetDryRunInputMode;
export type ConfigSetOperation = {
  inputMode: ConfigSetInputMode;
  requestedPath: PathSegment[];
  setPath: PathSegment[];
  value: unknown;
  touchedSecretTargetPath?: string;
  touchedProviderAlias?: string;
  assignedRef?: SecretRef;
};

export function ensureValidOllamaProviderForApiKeySet(
  root: Record<string, unknown>,
  path: PathSegment[],
): void {
  if (!pathEquals(path, OLLAMA_API_KEY_PATH)) {
    return;
  }
  const existing = getAtPath(root, OLLAMA_PROVIDER_PATH);
  if (existing.found) {
    return;
  }
  setAtPath(root, OLLAMA_PROVIDER_PATH, {
    baseUrl: OLLAMA_DEFAULT_BASE_URL,
    api: "ollama",
    models: [],
  });
}

function parseSecretRefSource(raw: string, label: string): SecretRefSource {
  const source = raw.trim();
  if (source === "env" || source === "file" || source === "exec") {
    return source;
  }
  throw new Error(`${label} must be one of: env, file, exec.`);
}

export function parseSecretRefBuilder(params: {
  provider: string;
  source: string;
  id: string;
  fieldPrefix: string;
}): SecretRef {
  const provider = params.provider.trim();
  if (!provider) {
    throw new Error(`${params.fieldPrefix}.provider is required.`);
  }
  if (!isValidSecretProviderAlias(provider)) {
    throw new Error(
      `${params.fieldPrefix}.provider must match /^[a-z][a-z0-9_-]{0,63}$/ (example: "default").`,
    );
  }

  const source = parseSecretRefSource(params.source, `${params.fieldPrefix}.source`);
  const id = params.id.trim();
  if (!id) {
    throw new Error(`${params.fieldPrefix}.id is required.`);
  }
  if (source === "env" && !isValidEnvSecretRefId(id)) {
    throw new Error(`${params.fieldPrefix}.id must match /^[A-Z][A-Z0-9_]{0,127}$/ for env refs.`);
  }
  if (source === "file" && !isValidFileSecretRefId(id)) {
    throw new Error(
      `${params.fieldPrefix}.id must be an absolute JSON pointer (or "value" for singleValue mode).`,
    );
  }
  if (source === "exec") {
    const validated = validateExecSecretRefId(id);
    if (!validated.ok) {
      throw new Error(formatExecSecretRefIdValidationMessage());
    }
  }
  return { source, provider, id };
}

function parseOptionalPositiveInteger(raw: string | undefined, flag: string): number | undefined {
  if (raw === undefined) {
    return undefined;
  }
  const trimmed = raw.trim();
  if (!trimmed) {
    throw new Error(`${flag} must not be empty.`);
  }
  const parsed = Number(trimmed);
  if (!Number.isInteger(parsed) || parsed <= 0) {
    throw new Error(`${flag} must be a positive integer.`);
  }
  return parsed;
}

function parseProviderEnvEntries(
  entries: string[] | undefined,
): Record<string, string> | undefined {
  if (!entries || entries.length === 0) {
    return undefined;
  }
  const env: Record<string, string> = {};
  for (const entry of entries) {
    const separator = entry.indexOf("=");
    if (separator <= 0) {
      throw new Error(`--provider-env expects KEY=VALUE entries (received: "${entry}").`);
    }
    const key = entry.slice(0, separator).trim();
    if (!key) {
      throw new Error(`--provider-env key must not be empty (received: "${entry}").`);
    }
    env[key] = entry.slice(separator + 1);
  }
  return Object.keys(env).length > 0 ? env : undefined;
}

export function parseProviderAliasPath(path: PathSegment[]): string {
  const expectedPrefixMatches =
    path.length === 3 &&
    path[0] === SECRET_PROVIDER_PATH_PREFIX[0] &&
    path[1] === SECRET_PROVIDER_PATH_PREFIX[1];
  if (!expectedPrefixMatches) {
    throw new Error(
      'Provider builder mode requires path "secrets.providers.<alias>" (example: secrets.providers.vault).',
    );
  }
  const alias = path[2] ?? "";
  if (!isValidSecretProviderAlias(alias)) {
    throw new Error(
      `Provider alias "${alias}" must match /^[a-z][a-z0-9_-]{0,63}$/ (example: "default").`,
    );
  }
  return alias;
}

export function buildProviderFromBuilder(opts: ConfigSetOptions): SecretProviderConfig {
  const sourceRaw = opts.providerSource?.trim();
  if (!sourceRaw) {
    throw new Error("--provider-source is required in provider builder mode.");
  }
  const source = parseSecretRefSource(sourceRaw, "--provider-source");
  const timeoutMs = parseOptionalPositiveInteger(opts.providerTimeoutMs, "--provider-timeout-ms");
  const maxBytes = parseOptionalPositiveInteger(opts.providerMaxBytes, "--provider-max-bytes");
  const noOutputTimeoutMs = parseOptionalPositiveInteger(
    opts.providerNoOutputTimeoutMs,
    "--provider-no-output-timeout-ms",
  );
  const maxOutputBytes = parseOptionalPositiveInteger(
    opts.providerMaxOutputBytes,
    "--provider-max-output-bytes",
  );
  const providerEnv = parseProviderEnvEntries(opts.providerEnv);

  let provider: SecretProviderConfig;
  if (source === "env") {
    const allowlist = (opts.providerAllowlist ?? []).map((entry) => entry.trim()).filter(Boolean);
    for (const envName of allowlist) {
      if (!isValidEnvSecretRefId(envName)) {
        throw new Error(
          `--provider-allowlist entry "${envName}" must match /^[A-Z][A-Z0-9_]{0,127}$/.`,
        );
      }
    }
    provider = {
      source: "env",
      ...(allowlist.length > 0 ? { allowlist } : {}),
    };
  } else if (source === "file") {
    const filePath = opts.providerPath?.trim();
    if (!filePath) {
      throw new Error("--provider-path is required when --provider-source file is used.");
    }
    const modeRaw = opts.providerMode?.trim();
    if (modeRaw && modeRaw !== "singleValue" && modeRaw !== "json") {
      throw new Error("--provider-mode must be one of: singleValue, json.");
    }
    const mode = modeRaw === "singleValue" || modeRaw === "json" ? modeRaw : undefined;
    provider = {
      source: "file",
      path: filePath,
      ...(mode ? { mode } : {}),
      ...(timeoutMs !== undefined ? { timeoutMs } : {}),
      ...(maxBytes !== undefined ? { maxBytes } : {}),
    };
  } else {
    const command = opts.providerCommand?.trim();
    if (!command) {
      throw new Error("--provider-command is required when --provider-source exec is used.");
    }
    provider = {
      source: "exec",
      command,
      ...(opts.providerArg && opts.providerArg.length > 0
        ? { args: opts.providerArg.map((entry) => entry.trim()) }
        : {}),
      ...(timeoutMs !== undefined ? { timeoutMs } : {}),
      ...(noOutputTimeoutMs !== undefined ? { noOutputTimeoutMs } : {}),
      ...(maxOutputBytes !== undefined ? { maxOutputBytes } : {}),
      ...(opts.providerJsonOnly ? { jsonOnly: true } : {}),
      ...(providerEnv ? { env: providerEnv } : {}),
      ...(opts.providerPassEnv && opts.providerPassEnv.length > 0
        ? { passEnv: opts.providerPassEnv.map((entry) => entry.trim()).filter(Boolean) }
        : {}),
      ...(opts.providerTrustedDir && opts.providerTrustedDir.length > 0
        ? { trustedDirs: opts.providerTrustedDir.map((entry) => entry.trim()).filter(Boolean) }
        : {}),
      ...(opts.providerAllowInsecurePath ? { allowInsecurePath: true } : {}),
      ...(opts.providerAllowSymlinkCommand ? { allowSymlinkCommand: true } : {}),
    };
  }

  const validated = SecretProviderSchema.safeParse(provider);
  if (!validated.success) {
    const issue = validated.error.issues[0];
    const issuePath = issue?.path?.join(".") ?? "<provider>";
    const issueMessage = issue?.message ?? "Invalid provider config.";
    throw new Error(`Provider builder config invalid at ${issuePath}: ${issueMessage}`);
  }
  return validated.data;
}

function parseSecretRefFromUnknown(value: unknown, label: string): SecretRef {
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    throw new Error(`${label} must be an object with source/provider/id.`);
  }
  const candidate = value as Record<string, unknown>;
  if (
    typeof candidate.provider !== "string" ||
    typeof candidate.source !== "string" ||
    typeof candidate.id !== "string"
  ) {
    throw new Error(`${label} must include string fields: source, provider, id.`);
  }
  return parseSecretRefBuilder({
    provider: candidate.provider,
    source: candidate.source,
    id: candidate.id,
    fieldPrefix: label,
  });
}

function parseProviderAliasFromTargetPath(path: PathSegment[]): string | null {
  if (
    path.length >= 3 &&
    path[0] === SECRET_PROVIDER_PATH_PREFIX[0] &&
    path[1] === SECRET_PROVIDER_PATH_PREFIX[1]
  ) {
    return path[2] ?? null;
  }
  return null;
}

export function buildRefAssignmentOperation(params: {
  requestedPath: PathSegment[];
  ref: SecretRef;
  inputMode: ConfigSetInputMode;
}): ConfigSetOperation {
  const resolved = resolveConfigSecretTargetByPath(params.requestedPath);
  if (resolved?.entry.secretShape === "sibling_ref" && resolved.refPathSegments) {
    return {
      inputMode: params.inputMode,
      requestedPath: params.requestedPath,
      setPath: resolved.refPathSegments,
      value: params.ref,
      touchedSecretTargetPath: toDotPath(resolved.pathSegments),
      assignedRef: params.ref,
      ...(resolved.providerId ? { touchedProviderAlias: resolved.providerId } : {}),
    };
  }
  return {
    inputMode: params.inputMode,
    requestedPath: params.requestedPath,
    setPath: params.requestedPath,
    value: params.ref,
    touchedSecretTargetPath: resolved
      ? toDotPath(resolved.pathSegments)
      : toDotPath(params.requestedPath),
    assignedRef: params.ref,
    ...(resolved?.providerId ? { touchedProviderAlias: resolved.providerId } : {}),
  };
}

export function buildValueAssignmentOperation(params: {
  requestedPath: PathSegment[];
  value: unknown;
  inputMode: ConfigSetInputMode;
}): ConfigSetOperation {
  const resolved = resolveConfigSecretTargetByPath(params.requestedPath);
  const providerAlias = parseProviderAliasFromTargetPath(params.requestedPath);
  const coercedRef = coerceSecretRef(params.value);
  return {
    inputMode: params.inputMode,
    requestedPath: params.requestedPath,
    setPath: params.requestedPath,
    value: params.value,
    ...(resolved ? { touchedSecretTargetPath: toDotPath(resolved.pathSegments) } : {}),
    ...(providerAlias ? { touchedProviderAlias: providerAlias } : {}),
    ...(coercedRef ? { assignedRef: coercedRef } : {}),
  };
}

export function parseBatchOperations(entries: ConfigSetBatchEntry[]): ConfigSetOperation[] {
  const operations: ConfigSetOperation[] = [];
  for (const [index, entry] of entries.entries()) {
    const path = parseRequiredPath(entry.path);
    if (entry.ref !== undefined) {
      const ref = parseSecretRefFromUnknown(entry.ref, `batch[${index}].ref`);
      operations.push(
        buildRefAssignmentOperation({
          requestedPath: path,
          ref,
          inputMode: "json",
        }),
      );
      continue;
    }
    if (entry.provider !== undefined) {
      const alias = parseProviderAliasPath(path);
      const validated = SecretProviderSchema.safeParse(entry.provider);
      if (!validated.success) {
        const issue = validated.error.issues[0];
        const issuePath = issue?.path?.join(".") ?? "<provider>";
        throw new Error(
          `batch[${index}].provider invalid at ${issuePath}: ${issue?.message ?? ""}`,
        );
      }
      operations.push({
        inputMode: "json",
        requestedPath: path,
        setPath: path,
        value: validated.data,
        touchedProviderAlias: alias,
      });
      continue;
    }
    operations.push(
      buildValueAssignmentOperation({
        requestedPath: path,
        value: entry.value,
        inputMode: "json",
      }),
    );
  }
  return operations;
}

function modeError(message: string): Error {
  return new Error(`config set mode error: ${message}`);
}

export function buildSingleSetOperations(params: {
  path?: string;
  value?: string;
  opts: ConfigSetOptions;
}): ConfigSetOperation[] {
  const pathProvided = typeof params.path === "string" && params.path.trim().length > 0;
  const parsedPath = pathProvided ? parseRequiredPath(params.path as string) : null;
  const strictJson = Boolean(params.opts.strictJson || params.opts.json);
  const modeResolution = resolveConfigSetMode({
    hasBatchMode: false,
    hasRefBuilderOptions: hasRefBuilderOptions(params.opts),
    hasProviderBuilderOptions: hasProviderBuilderOptions(params.opts),
    strictJson,
  });
  if (!modeResolution.ok) {
    throw modeError(modeResolution.error);
  }

  if (modeResolution.mode === "ref_builder") {
    if (!pathProvided || !parsedPath) {
      throw modeError("ref builder mode requires <path>.");
    }
    if (params.value !== undefined) {
      throw modeError("ref builder mode does not accept <value>.");
    }
    if (!params.opts.refProvider || !params.opts.refSource || !params.opts.refId) {
      throw modeError(
        "ref builder mode requires --ref-provider <alias>, --ref-source <env|file|exec>, and --ref-id <id>.",
      );
    }
    const ref = parseSecretRefBuilder({
      provider: params.opts.refProvider,
      source: params.opts.refSource,
      id: params.opts.refId,
      fieldPrefix: "ref",
    });
    return [
      buildRefAssignmentOperation({
        requestedPath: parsedPath,
        ref,
        inputMode: "builder",
      }),
    ];
  }

  if (modeResolution.mode === "provider_builder") {
    if (!pathProvided || !parsedPath) {
      throw modeError("provider builder mode requires <path>.");
    }
    if (params.value !== undefined) {
      throw modeError("provider builder mode does not accept <value>.");
    }
    const alias = parseProviderAliasPath(parsedPath);
    const provider = buildProviderFromBuilder(params.opts);
    return [
      {
        inputMode: "builder",
        requestedPath: parsedPath,
        setPath: parsedPath,
        value: provider,
        touchedProviderAlias: alias,
      },
    ];
  }

  if (!pathProvided || !parsedPath) {
    throw modeError("value/json mode requires <path> when batch mode is not used.");
  }
  if (params.value === undefined) {
    throw modeError("value/json mode requires <value>.");
  }
  const parsedValue = parseValue(params.value, { strictJson });
  return [
    buildValueAssignmentOperation({
      requestedPath: parsedPath,
      value: parsedValue,
      inputMode: modeResolution.mode === "json" ? "json" : "value",
    }),
  ];
}

export { hasBatchMode, parseBatchSource };
