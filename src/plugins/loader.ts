import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { createJiti } from "jiti";
import type { DenebConfig } from "../config/config.js";
import type { PluginInstallRecord } from "../config/types.plugins.js";
import type { GatewayRequestHandler } from "../gateway/server-methods/types.js";
import { openBoundaryFileSync } from "../infra/boundary-file-read.js";
import { createSubsystemLogger } from "../logging/subsystem.js";
import { resolveUserPath } from "../utils.js";
import { clearPluginCommands } from "./commands.js";
import {
  applyTestPluginDefaults,
  normalizePluginsConfig,
  resolveEffectiveEnableState,
  resolveMemorySlotDecision,
  type NormalizedPluginsConfig,
} from "./config-state.js";
import { discoverDenebPlugins } from "./discovery.js";
import { initializeGlobalHookRunner } from "./hook-runner-global.js";
import { clearPluginInteractiveHandlers } from "./interactive.js";
import { validateBundlePlugin } from "./loader-bundle-validation.js";
import {
  compareDuplicateCandidateOrder,
  createPluginRecord,
  normalizeScopedPluginIds,
  pushDiagnostics,
  recordPluginError,
  resolvePluginModuleExport,
  resolveSetupChannelRegistration,
  shouldLoadChannelPluginInSetupRuntime,
  validatePluginConfig,
  warnWhenAllowlistIsOpen,
} from "./loader-helpers.js";
import { buildProvenanceIndex, warnAboutUntrackedLoadedPlugins } from "./loader-provenance.js";
import { createLazyRuntimeProxy } from "./loader-runtime-proxy.js";
import { loadPluginManifestRegistry } from "./manifest-registry.js";
import { createPluginRegistry, type PluginRegistry } from "./registry.js";
import { resolvePluginCacheInputs } from "./roots.js";
import { setActivePluginRegistry } from "./runtime.js";
import type { CreatePluginRuntimeOptions } from "./runtime/index.js";
import type { PluginRuntime } from "./runtime/types.js";
import {
  buildPluginLoaderJitiOptions,
  listPluginSdkAliasCandidates,
  listPluginSdkExportedSubpaths,
  resolveLoaderPackageRoot,
  resolvePluginSdkAliasCandidateOrder,
  resolvePluginSdkAliasFile,
  resolvePluginSdkScopedAliasMap,
  shouldPreferNativeJiti,
  type LoaderModuleResolveParams,
} from "./sdk-alias.js";
import type { DenebPluginModule, PluginLogger } from "./types.js";

export type PluginLoadResult = PluginRegistry;

export type PluginLoadOptions = {
  config?: DenebConfig;
  workspaceDir?: string;
  // Allows callers to resolve plugin roots and load paths against an explicit env
  // instead of the process-global environment.
  env?: NodeJS.ProcessEnv;
  logger?: PluginLogger;
  coreGatewayHandlers?: Record<string, GatewayRequestHandler>;
  runtimeOptions?: CreatePluginRuntimeOptions;
  cache?: boolean;
  mode?: "full" | "validate";
  onlyPluginIds?: string[];
  includeSetupOnlyChannelPlugins?: boolean;
  /**
   * Prefer `setupEntry` for configured channel plugins that explicitly opt in
   * via package metadata because their setup entry covers the pre-listen startup surface.
   */
  preferSetupRuntimeForChannelPlugins?: boolean;
  activate?: boolean;
};

const MAX_PLUGIN_REGISTRY_CACHE_ENTRIES = 128;
const registryCache = new Map<string, PluginRegistry>();
const openAllowlistWarningCache = new Set<string>();

export function clearPluginLoaderCache(): void {
  registryCache.clear();
  openAllowlistWarningCache.clear();
}

const defaultLogger = () => createSubsystemLogger("plugins");

function resolveLoaderModulePath(params: LoaderModuleResolveParams = {}): string {
  return params.modulePath ?? fileURLToPath(params.moduleUrl ?? import.meta.url);
}

const resolvePluginSdkAlias = (): string | null =>
  resolvePluginSdkAliasFile({ srcFile: "root-alias.cjs", distFile: "root-alias.cjs" });

function resolvePluginRuntimeModulePath(params: LoaderModuleResolveParams = {}): string | null {
  try {
    const modulePath = resolveLoaderModulePath(params);
    const orderedKinds = resolvePluginSdkAliasCandidateOrder({
      modulePath,
      isProduction: process.env.NODE_ENV === "production",
    });
    const packageRoot = resolveLoaderPackageRoot({ ...params, modulePath });
    const candidates = packageRoot
      ? orderedKinds.map((kind) =>
          kind === "src"
            ? path.join(packageRoot, "src", "plugins", "runtime", "index.ts")
            : path.join(packageRoot, "dist", "plugins", "runtime", "index.js"),
        )
      : [
          path.join(path.dirname(modulePath), "runtime", "index.ts"),
          path.join(path.dirname(modulePath), "runtime", "index.js"),
        ];
    for (const candidate of candidates) {
      if (fs.existsSync(candidate)) {
        return candidate;
      }
    }
  } catch {
    // ignore
  }
  return null;
}

export const __testing = {
  buildPluginLoaderJitiOptions,
  listPluginSdkAliasCandidates,
  listPluginSdkExportedSubpaths,
  resolvePluginSdkScopedAliasMap,
  resolvePluginSdkAliasCandidateOrder,
  resolvePluginSdkAliasFile,
  resolvePluginRuntimeModulePath,
  shouldPreferNativeJiti,
  maxPluginRegistryCacheEntries: MAX_PLUGIN_REGISTRY_CACHE_ENTRIES,
};

function getCachedPluginRegistry(cacheKey: string): PluginRegistry | undefined {
  const cached = registryCache.get(cacheKey);
  if (!cached) {
    return undefined;
  }
  // Refresh insertion order so frequently reused registries survive eviction.
  registryCache.delete(cacheKey);
  registryCache.set(cacheKey, cached);
  return cached;
}

function setCachedPluginRegistry(cacheKey: string, registry: PluginRegistry): void {
  if (registryCache.has(cacheKey)) {
    registryCache.delete(cacheKey);
  }
  registryCache.set(cacheKey, registry);
  while (registryCache.size > MAX_PLUGIN_REGISTRY_CACHE_ENTRIES) {
    const oldestKey = registryCache.keys().next().value;
    if (!oldestKey) {
      break;
    }
    registryCache.delete(oldestKey);
  }
}

function buildCacheKey(params: {
  workspaceDir?: string;
  plugins: NormalizedPluginsConfig;
  installs?: Record<string, PluginInstallRecord>;
  env: NodeJS.ProcessEnv;
  onlyPluginIds?: string[];
  includeSetupOnlyChannelPlugins?: boolean;
  preferSetupRuntimeForChannelPlugins?: boolean;
  runtimeSubagentMode?: "default" | "explicit" | "gateway-bindable";
}): string {
  const { roots, loadPaths } = resolvePluginCacheInputs({
    workspaceDir: params.workspaceDir,
    loadPaths: params.plugins.loadPaths,
    env: params.env,
  });
  const installs = Object.fromEntries(
    Object.entries(params.installs ?? {}).map(([pluginId, install]) => [
      pluginId,
      {
        ...install,
        installPath:
          typeof install.installPath === "string"
            ? resolveUserPath(install.installPath, params.env)
            : install.installPath,
        sourcePath:
          typeof install.sourcePath === "string"
            ? resolveUserPath(install.sourcePath, params.env)
            : install.sourcePath,
      },
    ]),
  );
  const scopeKey = JSON.stringify(params.onlyPluginIds ?? []);
  const setupOnlyKey = params.includeSetupOnlyChannelPlugins === true ? "setup-only" : "runtime";
  const startupChannelMode =
    params.preferSetupRuntimeForChannelPlugins === true ? "prefer-setup" : "full";
  return `${roots.workspace ?? ""}::${roots.global ?? ""}::${roots.stock ?? ""}::${JSON.stringify({
    ...params.plugins,
    installs,
    loadPaths,
  })}::${scopeKey}::${setupOnlyKey}::${startupChannelMode}::${params.runtimeSubagentMode ?? "default"}`;
}

function activatePluginRegistry(registry: PluginRegistry, cacheKey: string): void {
  setActivePluginRegistry(registry, cacheKey);
  initializeGlobalHookRunner(registry);
}

export function loadDenebPlugins(options: PluginLoadOptions = {}): PluginRegistry {
  // Snapshot (non-activating) loads must disable the cache to avoid storing a registry
  // whose commands were never globally registered.
  if (options.activate === false && options.cache !== false) {
    throw new Error(
      "loadDenebPlugins: activate:false requires cache:false to prevent command registry divergence",
    );
  }
  const env = options.env ?? process.env;
  // Test env: default-disable plugins unless explicitly configured.
  // This keeps unit/gateway suites fast and avoids loading heavyweight plugin deps by accident.
  const cfg = applyTestPluginDefaults(options.config ?? {}, env);
  const logger = options.logger ?? defaultLogger();
  const validateOnly = options.mode === "validate";
  const normalized = normalizePluginsConfig(cfg.plugins);
  const onlyPluginIds = normalizeScopedPluginIds(options.onlyPluginIds);
  const onlyPluginIdSet = onlyPluginIds ? new Set(onlyPluginIds) : null;
  const includeSetupOnlyChannelPlugins = options.includeSetupOnlyChannelPlugins === true;
  const preferSetupRuntimeForChannelPlugins = options.preferSetupRuntimeForChannelPlugins === true;
  const shouldActivate = options.activate !== false;
  // NOTE: `activate` is intentionally excluded from the cache key. All non-activating
  // (snapshot) callers pass `cache: false` via loadOnboardingPluginRegistry(), so they
  // never read from or write to the cache. Including `activate` here would be misleading
  // — it would imply mixed-activate caching is supported, when in practice it is not.
  const cacheKey = buildCacheKey({
    workspaceDir: options.workspaceDir,
    plugins: normalized,
    installs: cfg.plugins?.installs,
    env,
    onlyPluginIds,
    includeSetupOnlyChannelPlugins,
    preferSetupRuntimeForChannelPlugins,
    runtimeSubagentMode:
      options.runtimeOptions?.allowGatewaySubagentBinding === true
        ? "gateway-bindable"
        : options.runtimeOptions?.subagent
          ? "explicit"
          : "default",
  });
  const cacheEnabled = options.cache !== false;
  if (cacheEnabled) {
    const cached = getCachedPluginRegistry(cacheKey);
    if (cached) {
      if (shouldActivate) {
        activatePluginRegistry(cached, cacheKey);
      }
      return cached;
    }
  }

  // Clear previously registered plugin commands before reloading.
  // Skip for non-activating (snapshot) loads to avoid wiping commands from other plugins.
  if (shouldActivate) {
    clearPluginCommands();
    clearPluginInteractiveHandlers();
  }

  // Lazy: avoid creating the Jiti loader when all plugins are disabled (common in unit tests).
  const jitiLoaders = new Map<boolean, ReturnType<typeof createJiti>>();
  const getJiti = (modulePath: string) => {
    const tryNative = shouldPreferNativeJiti(modulePath);
    const cached = jitiLoaders.get(tryNative);
    if (cached) {
      return cached;
    }
    const pluginSdkAlias = resolvePluginSdkAlias();
    const aliasMap = {
      ...(pluginSdkAlias ? { "deneb/plugin-sdk": pluginSdkAlias } : {}),
      ...resolvePluginSdkScopedAliasMap(),
    };
    const loader = createJiti(import.meta.url, {
      ...buildPluginLoaderJitiOptions(aliasMap),
      // Source .ts runtime shims import sibling ".js" specifiers that only exist
      // after build. Disable native loading for source entries so Jiti rewrites
      // those imports against the source graph, while keeping native dist/*.js
      // loading for the canonical built module graph.
      tryNative,
    });
    jitiLoaders.set(tryNative, loader);
    return loader;
  };

  let createPluginRuntimeFactory: ((options?: CreatePluginRuntimeOptions) => PluginRuntime) | null =
    null;
  const resolveCreatePluginRuntime = (): ((
    options?: CreatePluginRuntimeOptions,
  ) => PluginRuntime) => {
    if (createPluginRuntimeFactory) {
      return createPluginRuntimeFactory;
    }
    const runtimeModulePath = resolvePluginRuntimeModulePath();
    if (!runtimeModulePath) {
      throw new Error("Unable to resolve plugin runtime module");
    }
    const runtimeModule = getJiti(runtimeModulePath)(runtimeModulePath) as {
      createPluginRuntime?: (options?: CreatePluginRuntimeOptions) => PluginRuntime;
    };
    if (typeof runtimeModule.createPluginRuntime !== "function") {
      throw new Error("Plugin runtime module missing createPluginRuntime export");
    }
    createPluginRuntimeFactory = runtimeModule.createPluginRuntime;
    return createPluginRuntimeFactory;
  };

  const runtime = createLazyRuntimeProxy(resolveCreatePluginRuntime, options.runtimeOptions);

  const { registry, createApi } = createPluginRegistry({
    logger,
    runtime,
    coreGatewayHandlers: options.coreGatewayHandlers as Record<string, GatewayRequestHandler>,
    suppressGlobalCommands: !shouldActivate,
  });

  const discovery = discoverDenebPlugins({
    workspaceDir: options.workspaceDir,
    extraPaths: normalized.loadPaths,
    cache: options.cache,
    env,
  });
  const manifestRegistry = loadPluginManifestRegistry({
    config: cfg,
    workspaceDir: options.workspaceDir,
    cache: options.cache,
    env,
    candidates: discovery.candidates,
    diagnostics: discovery.diagnostics,
  });
  pushDiagnostics(registry.diagnostics, manifestRegistry.diagnostics);
  warnWhenAllowlistIsOpen({
    logger,
    pluginsEnabled: normalized.enabled,
    allow: normalized.allow,
    warningCacheKey: cacheKey,
    warningCache: openAllowlistWarningCache,
    // Keep warning input scoped as well so partial snapshot loads only mention the
    // plugins that were intentionally requested for this registry.
    discoverablePlugins: manifestRegistry.plugins
      .filter((plugin) => !onlyPluginIdSet || onlyPluginIdSet.has(plugin.id))
      .map((plugin) => ({
        id: plugin.id,
        source: plugin.source,
        origin: plugin.origin,
      })),
  });
  const provenance = buildProvenanceIndex({
    config: cfg,
    normalizedLoadPaths: normalized.loadPaths,
    env,
  });

  const manifestByRoot = new Map(
    manifestRegistry.plugins.map((record) => [record.rootDir, record]),
  );
  const orderedCandidates = [...discovery.candidates].toSorted((left, right) => {
    return compareDuplicateCandidateOrder({
      left,
      right,
      manifestByRoot,
      provenance,
      env,
    });
  });

  const seenIds = new Map<
    string,
    ReturnType<typeof discoverDenebPlugins>["candidates"][number]["origin"]
  >();
  const memorySlot = normalized.slots.memory;
  let selectedMemoryPluginId: string | null = null;
  let memorySlotMatched = false;

  for (const candidate of orderedCandidates) {
    const manifestRecord = manifestByRoot.get(candidate.rootDir);
    if (!manifestRecord) {
      continue;
    }
    const pluginId = manifestRecord.id;
    // Filter again at import time as a final guard. The earlier manifest filter keeps
    // warnings scoped; this one prevents loading/registering anything outside the scope.
    if (onlyPluginIdSet && !onlyPluginIdSet.has(pluginId)) {
      continue;
    }
    const existingOrigin = seenIds.get(pluginId);
    if (existingOrigin) {
      const record = createPluginRecord({
        id: pluginId,
        name: manifestRecord.name ?? pluginId,
        description: manifestRecord.description,
        version: manifestRecord.version,
        format: manifestRecord.format,
        bundleFormat: manifestRecord.bundleFormat,
        bundleCapabilities: manifestRecord.bundleCapabilities,
        source: candidate.source,
        rootDir: candidate.rootDir,
        origin: candidate.origin,
        workspaceDir: candidate.workspaceDir,
        enabled: false,
        configSchema: Boolean(manifestRecord.configSchema),
      });
      record.status = "disabled";
      record.error = `overridden by ${existingOrigin} plugin`;
      registry.plugins.push(record);
      continue;
    }

    const enableState = resolveEffectiveEnableState({
      id: pluginId,
      origin: candidate.origin,
      config: normalized,
      rootConfig: cfg,
      enabledByDefault: manifestRecord.enabledByDefault,
    });
    const entry = normalized.entries[pluginId];
    const record = createPluginRecord({
      id: pluginId,
      name: manifestRecord.name ?? pluginId,
      description: manifestRecord.description,
      version: manifestRecord.version,
      format: manifestRecord.format,
      bundleFormat: manifestRecord.bundleFormat,
      bundleCapabilities: manifestRecord.bundleCapabilities,
      source: candidate.source,
      rootDir: candidate.rootDir,
      origin: candidate.origin,
      workspaceDir: candidate.workspaceDir,
      enabled: enableState.enabled,
      configSchema: Boolean(manifestRecord.configSchema),
    });
    record.kind = manifestRecord.kind;
    record.configUiHints = manifestRecord.configUiHints;
    record.configJsonSchema = manifestRecord.configSchema;
    const pushPluginLoadError = (message: string) => {
      record.status = "error";
      record.error = message;
      registry.plugins.push(record);
      seenIds.set(pluginId, candidate.origin);
      registry.diagnostics.push({
        level: "error",
        pluginId: record.id,
        source: record.source,
        message: record.error,
      });
    };

    const registrationMode = enableState.enabled
      ? !validateOnly &&
        shouldLoadChannelPluginInSetupRuntime({
          manifestChannels: manifestRecord.channels,
          setupSource: manifestRecord.setupSource,
          startupDeferConfiguredChannelFullLoadUntilAfterListen:
            manifestRecord.startupDeferConfiguredChannelFullLoadUntilAfterListen,
          cfg,
          env,
          preferSetupRuntimeForChannelPlugins,
        })
        ? "setup-runtime"
        : "full"
      : includeSetupOnlyChannelPlugins && !validateOnly && manifestRecord.channels.length > 0
        ? "setup-only"
        : null;

    if (!registrationMode) {
      record.status = "disabled";
      record.error = enableState.reason;
      registry.plugins.push(record);
      seenIds.set(pluginId, candidate.origin);
      continue;
    }
    if (!enableState.enabled) {
      record.status = "disabled";
      record.error = enableState.reason;
    }

    if (
      validateBundlePlugin({
        record,
        enabled: enableState.enabled,
        diagnostics: registry.diagnostics,
      })
    ) {
      registry.plugins.push(record);
      seenIds.set(pluginId, candidate.origin);
      continue;
    }
    // Fast-path bundled memory plugins that are guaranteed disabled by slot policy.
    // This avoids opening/importing heavy memory plugin modules that will never register.
    if (
      registrationMode === "full" &&
      candidate.origin === "bundled" &&
      manifestRecord.kind === "memory"
    ) {
      const earlyMemoryDecision = resolveMemorySlotDecision({
        id: record.id,
        kind: "memory",
        slot: memorySlot,
        selectedId: selectedMemoryPluginId,
      });
      if (!earlyMemoryDecision.enabled) {
        record.enabled = false;
        record.status = "disabled";
        record.error = earlyMemoryDecision.reason;
        registry.plugins.push(record);
        seenIds.set(pluginId, candidate.origin);
        continue;
      }
    }

    if (!manifestRecord.configSchema) {
      pushPluginLoadError("missing config schema");
      continue;
    }

    const pluginRoot = safeRealpathOrResolve(candidate.rootDir);
    const loadSource =
      (registrationMode === "setup-only" || registrationMode === "setup-runtime") &&
      manifestRecord.setupSource
        ? manifestRecord.setupSource
        : candidate.source;
    const opened = openBoundaryFileSync({
      absolutePath: loadSource,
      rootPath: pluginRoot,
      boundaryLabel: "plugin root",
      rejectHardlinks: candidate.origin !== "bundled",
      skipLexicalRootCheck: true,
    });
    if (!opened.ok) {
      pushPluginLoadError("plugin entry path escapes plugin root or fails alias checks");
      continue;
    }
    const safeSource = opened.path;
    fs.closeSync(opened.fd);

    let mod: DenebPluginModule | null = null;
    try {
      mod = getJiti(safeSource)(safeSource) as DenebPluginModule;
    } catch (err) {
      recordPluginError({
        logger,
        registry,
        record,
        seenIds,
        pluginId,
        origin: candidate.origin,
        error: err,
        logPrefix: `[plugins] ${record.id} failed to load from ${record.source}: `,
        diagnosticMessagePrefix: "failed to load plugin: ",
      });
      continue;
    }

    if (
      (registrationMode === "setup-only" || registrationMode === "setup-runtime") &&
      manifestRecord.setupSource
    ) {
      const setupRegistration = resolveSetupChannelRegistration(mod);
      if (setupRegistration.plugin) {
        if (setupRegistration.plugin.id && setupRegistration.plugin.id !== record.id) {
          pushPluginLoadError(
            `plugin id mismatch (config uses "${record.id}", setup export uses "${setupRegistration.plugin.id}")`,
          );
          continue;
        }
        const api = createApi(record, {
          config: cfg,
          pluginConfig: {},
          hookPolicy: entry?.hooks,
          registrationMode,
        });
        api.registerChannel(setupRegistration.plugin);
        registry.plugins.push(record);
        seenIds.set(pluginId, candidate.origin);
        continue;
      }
    }

    const resolved = resolvePluginModuleExport(mod);
    const definition = resolved.definition;
    const register = resolved.register;

    if (definition?.id && definition.id !== record.id) {
      pushPluginLoadError(
        `plugin id mismatch (config uses "${record.id}", export uses "${definition.id}")`,
      );
      continue;
    }

    record.name = definition?.name ?? record.name;
    record.description = definition?.description ?? record.description;
    record.version = definition?.version ?? record.version;
    const manifestKind = record.kind as string | undefined;
    const exportKind = definition?.kind as string | undefined;
    if (manifestKind && exportKind && exportKind !== manifestKind) {
      registry.diagnostics.push({
        level: "warn",
        pluginId: record.id,
        source: record.source,
        message: `plugin kind mismatch (manifest uses "${manifestKind}", export uses "${exportKind}")`,
      });
    }
    record.kind = definition?.kind ?? record.kind;

    if (record.kind === "memory" && memorySlot === record.id) {
      memorySlotMatched = true;
    }

    if (registrationMode === "full") {
      const memoryDecision = resolveMemorySlotDecision({
        id: record.id,
        kind: record.kind,
        slot: memorySlot,
        selectedId: selectedMemoryPluginId,
      });

      if (!memoryDecision.enabled) {
        record.enabled = false;
        record.status = "disabled";
        record.error = memoryDecision.reason;
        registry.plugins.push(record);
        seenIds.set(pluginId, candidate.origin);
        continue;
      }

      if (memoryDecision.selected && record.kind === "memory") {
        selectedMemoryPluginId = record.id;
      }
    }

    const validatedConfig = validatePluginConfig({
      schema: manifestRecord.configSchema,
      cacheKey: manifestRecord.schemaCacheKey,
      value: entry?.config,
    });

    if (!validatedConfig.ok) {
      logger.error(`[plugins] ${record.id} invalid config: ${validatedConfig.errors?.join(", ")}`);
      pushPluginLoadError(`invalid config: ${validatedConfig.errors?.join(", ")}`);
      continue;
    }

    if (validateOnly) {
      registry.plugins.push(record);
      seenIds.set(pluginId, candidate.origin);
      continue;
    }

    if (typeof register !== "function") {
      logger.error(`[plugins] ${record.id} missing register/activate export`);
      pushPluginLoadError("plugin export missing register/activate");
      continue;
    }

    const api = createApi(record, {
      config: cfg,
      pluginConfig: validatedConfig.value,
      hookPolicy: entry?.hooks,
      registrationMode,
    });

    try {
      const result = register(api);
      if (result && typeof result.then === "function") {
        registry.diagnostics.push({
          level: "warn",
          pluginId: record.id,
          source: record.source,
          message: "plugin register returned a promise; async registration is ignored",
        });
      }
      registry.plugins.push(record);
      seenIds.set(pluginId, candidate.origin);
    } catch (err) {
      recordPluginError({
        logger,
        registry,
        record,
        seenIds,
        pluginId,
        origin: candidate.origin,
        error: err,
        logPrefix: `[plugins] ${record.id} failed during register from ${record.source}: `,
        diagnosticMessagePrefix: "plugin failed during register: ",
      });
    }
  }

  // Scoped snapshot loads may intentionally omit the configured memory plugin, so only
  // emit the missing-memory diagnostic for full registry loads.
  if (!onlyPluginIdSet && typeof memorySlot === "string" && !memorySlotMatched) {
    registry.diagnostics.push({
      level: "warn",
      message: `memory slot plugin not found or not marked as memory: ${memorySlot}`,
    });
  }

  warnAboutUntrackedLoadedPlugins({
    registry,
    provenance,
    logger,
    env,
  });

  if (cacheEnabled) {
    setCachedPluginRegistry(cacheKey, registry);
  }
  if (shouldActivate) {
    activatePluginRegistry(registry, cacheKey);
  }
  return registry;
}

function safeRealpathOrResolve(value: string): string {
  try {
    return fs.realpathSync(value);
  } catch {
    return path.resolve(value);
  }
}
