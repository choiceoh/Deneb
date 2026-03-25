import type { ChannelPlugin } from "../channels/plugins/types.js";
import type { DenebConfig } from "../config/config.js";
import { isChannelConfigured } from "../config/plugin-auto-enable.js";
import type { discoverDenebPlugins } from "./discovery.js";
import { matchesExplicitInstallRule, type PluginProvenanceIndex } from "./loader-provenance.js";
import type { loadPluginManifestRegistry } from "./manifest-registry.js";
import type { PluginRecord, PluginRegistry } from "./registry.js";
import { validateJsonSchemaValue } from "./schema-validator.js";
import type {
  DenebPluginDefinition,
  PluginBundleFormat,
  PluginDiagnostic,
  PluginFormat,
  PluginLogger,
} from "./types.js";

export function createPluginRecord(params: {
  id: string;
  name?: string;
  description?: string;
  version?: string;
  format?: PluginFormat;
  bundleFormat?: PluginBundleFormat;
  bundleCapabilities?: string[];
  source: string;
  rootDir?: string;
  origin: PluginRecord["origin"];
  workspaceDir?: string;
  enabled: boolean;
  configSchema: boolean;
}): PluginRecord {
  return {
    id: params.id,
    name: params.name ?? params.id,
    description: params.description,
    version: params.version,
    format: params.format ?? "deneb",
    bundleFormat: params.bundleFormat,
    bundleCapabilities: params.bundleCapabilities,
    source: params.source,
    rootDir: params.rootDir,
    origin: params.origin,
    workspaceDir: params.workspaceDir,
    enabled: params.enabled,
    status: params.enabled ? "loaded" : "disabled",
    toolNames: [],
    hookNames: [],
    channelIds: [],
    providerIds: [],
    mediaUnderstandingProviderIds: [],
    imageGenerationProviderIds: [],
    webSearchProviderIds: [],
    gatewayMethods: [],
    cliCommands: [],
    services: [],
    commands: [],
    httpRoutes: 0,
    hookCount: 0,
    configSchema: params.configSchema,
    configUiHints: undefined,
    configJsonSchema: undefined,
  };
}

export function recordPluginError(params: {
  logger: PluginLogger;
  registry: PluginRegistry;
  record: PluginRecord;
  seenIds: Map<string, PluginRecord["origin"]>;
  pluginId: string;
  origin: PluginRecord["origin"];
  error: unknown;
  logPrefix: string;
  diagnosticMessagePrefix: string;
}) {
  const errorText =
    process.env.DENEB_PLUGIN_LOADER_DEBUG_STACKS === "1" &&
    params.error instanceof Error &&
    typeof params.error.stack === "string"
      ? params.error.stack
      : String(params.error);
  const deprecatedApiHint =
    errorText.includes("api.registerHttpHandler") && errorText.includes("is not a function")
      ? "deprecated api.registerHttpHandler(...) was removed; use api.registerHttpRoute(...) for plugin-owned routes or registerPluginHttpRoute(...) for dynamic lifecycle routes"
      : null;
  const displayError = deprecatedApiHint ? `${deprecatedApiHint} (${errorText})` : errorText;
  params.logger.error(`${params.logPrefix}${displayError}`);
  params.record.status = "error";
  params.record.error = displayError;
  params.registry.plugins.push(params.record);
  params.seenIds.set(params.pluginId, params.origin);
  params.registry.diagnostics.push({
    level: "error",
    pluginId: params.record.id,
    source: params.record.source,
    message: `${params.diagnosticMessagePrefix}${displayError}`,
  });
}

export function pushDiagnostics(diagnostics: PluginDiagnostic[], append: PluginDiagnostic[]) {
  diagnostics.push(...append);
}

export function validatePluginConfig(params: {
  schema?: Record<string, unknown>;
  cacheKey?: string;
  value?: unknown;
}): { ok: boolean; value?: Record<string, unknown>; errors?: string[] } {
  const schema = params.schema;
  if (!schema) {
    // No schema means no validation; pass through undefined or valid objects.
    if (params.value !== undefined && (typeof params.value !== "object" || params.value === null)) {
      return { ok: false, errors: ["plugin config must be an object or undefined"] };
    }
    return { ok: true, value: params.value as Record<string, unknown> | undefined };
  }
  const cacheKey = params.cacheKey ?? JSON.stringify(schema);
  const result = validateJsonSchemaValue({
    schema,
    cacheKey,
    value: params.value ?? {},
  });
  if (result.ok) {
    // Validated value may be coerced by the schema validator; ensure it is an object.
    if (params.value !== undefined && (typeof params.value !== "object" || params.value === null)) {
      return { ok: false, errors: ["plugin config must be an object"] };
    }
    return { ok: true, value: params.value as Record<string, unknown> | undefined };
  }
  return { ok: false, errors: result.errors.map((error) => error.text) };
}

export function resolvePluginModuleExport(moduleExport: unknown): {
  definition?: DenebPluginDefinition;
  register?: DenebPluginDefinition["register"];
} {
  const resolved =
    moduleExport &&
    typeof moduleExport === "object" &&
    "default" in (moduleExport as Record<string, unknown>)
      ? (moduleExport as { default: unknown }).default
      : moduleExport;
  if (typeof resolved === "function") {
    return {
      register: resolved as DenebPluginDefinition["register"],
    };
  }
  if (resolved && typeof resolved === "object") {
    const def = resolved as DenebPluginDefinition;
    const register = def.register ?? def.activate;
    return { definition: def, register };
  }
  return {};
}

export function resolveSetupChannelRegistration(moduleExport: unknown): {
  plugin?: ChannelPlugin;
} {
  const resolved =
    moduleExport &&
    typeof moduleExport === "object" &&
    "default" in (moduleExport as Record<string, unknown>)
      ? (moduleExport as { default: unknown }).default
      : moduleExport;
  if (!resolved || typeof resolved !== "object") {
    return {};
  }
  const setup = resolved as {
    plugin?: unknown;
  };
  if (!setup.plugin || typeof setup.plugin !== "object") {
    return {};
  }
  return {
    plugin: setup.plugin as ChannelPlugin,
  };
}

export function shouldLoadChannelPluginInSetupRuntime(params: {
  manifestChannels: string[];
  setupSource?: string;
  startupDeferConfiguredChannelFullLoadUntilAfterListen?: boolean;
  cfg: DenebConfig;
  env: NodeJS.ProcessEnv;
  preferSetupRuntimeForChannelPlugins?: boolean;
}): boolean {
  if (!params.setupSource || params.manifestChannels.length === 0) {
    return false;
  }
  if (
    params.preferSetupRuntimeForChannelPlugins &&
    params.startupDeferConfiguredChannelFullLoadUntilAfterListen === true
  ) {
    return true;
  }
  return !params.manifestChannels.some((channelId) =>
    isChannelConfigured(params.cfg, channelId, params.env),
  );
}

export function resolveCandidateDuplicateRank(params: {
  candidate: ReturnType<typeof discoverDenebPlugins>["candidates"][number];
  manifestByRoot: Map<string, ReturnType<typeof loadPluginManifestRegistry>["plugins"][number]>;
  provenance: PluginProvenanceIndex;
  env: NodeJS.ProcessEnv;
}): number {
  const manifestRecord = params.manifestByRoot.get(params.candidate.rootDir);
  const pluginId = manifestRecord?.id;
  const isExplicitInstall =
    params.candidate.origin === "global" &&
    pluginId !== undefined &&
    matchesExplicitInstallRule({
      pluginId,
      source: params.candidate.source,
      index: params.provenance,
      env: params.env,
    });

  if (params.candidate.origin === "config") {
    return 0;
  }
  if (params.candidate.origin === "global" && isExplicitInstall) {
    return 1;
  }
  if (params.candidate.origin === "bundled") {
    // Bundled plugin ids stay reserved unless the operator configured an override.
    return 2;
  }
  if (params.candidate.origin === "workspace") {
    return 3;
  }
  return 4;
}

export function compareDuplicateCandidateOrder(params: {
  left: ReturnType<typeof discoverDenebPlugins>["candidates"][number];
  right: ReturnType<typeof discoverDenebPlugins>["candidates"][number];
  manifestByRoot: Map<string, ReturnType<typeof loadPluginManifestRegistry>["plugins"][number]>;
  provenance: PluginProvenanceIndex;
  env: NodeJS.ProcessEnv;
}): number {
  const leftPluginId = params.manifestByRoot.get(params.left.rootDir)?.id;
  const rightPluginId = params.manifestByRoot.get(params.right.rootDir)?.id;
  if (!leftPluginId || leftPluginId !== rightPluginId) {
    return 0;
  }
  return (
    resolveCandidateDuplicateRank({
      candidate: params.left,
      manifestByRoot: params.manifestByRoot,
      provenance: params.provenance,
      env: params.env,
    }) -
    resolveCandidateDuplicateRank({
      candidate: params.right,
      manifestByRoot: params.manifestByRoot,
      provenance: params.provenance,
      env: params.env,
    })
  );
}

export function warnWhenAllowlistIsOpen(params: {
  logger: PluginLogger;
  pluginsEnabled: boolean;
  allow: string[];
  warningCacheKey: string;
  warningCache: Set<string>;
  discoverablePlugins: Array<{ id: string; source: string; origin: PluginRecord["origin"] }>;
}) {
  if (!params.pluginsEnabled) {
    return;
  }
  if (params.allow.length > 0) {
    return;
  }
  const nonBundled = params.discoverablePlugins.filter((entry) => entry.origin !== "bundled");
  if (nonBundled.length === 0) {
    return;
  }
  if (params.warningCache.has(params.warningCacheKey)) {
    return;
  }
  const preview = nonBundled
    .slice(0, 6)
    .map((entry) => `${entry.id} (${entry.source})`)
    .join(", ");
  const extra = nonBundled.length > 6 ? ` (+${nonBundled.length - 6} more)` : "";
  params.warningCache.add(params.warningCacheKey);
  params.logger.warn(
    `[plugins] plugins.allow is empty; discovered non-bundled plugins may auto-load: ${preview}${extra}. Set plugins.allow to explicit trusted ids.`,
  );
}

export function normalizeScopedPluginIds(ids?: string[]): string[] | undefined {
  if (!ids) {
    return undefined;
  }
  const normalized = Array.from(new Set(ids.map((id) => id.trim()).filter(Boolean))).toSorted();
  return normalized.length > 0 ? normalized : undefined;
}
