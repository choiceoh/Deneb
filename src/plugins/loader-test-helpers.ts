/**
 * Shared test helpers for plugin loader tests.
 *
 * Extracted from loader.test.ts to enable splitting the 3500+ LOC file
 * into focused, faster test suites.
 */
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { vi } from "vitest";

export async function importFreshPluginTestModules() {
  vi.resetModules();
  vi.doUnmock("node:fs");
  vi.doUnmock("node:fs/promises");
  vi.doUnmock("node:module");
  vi.doUnmock("./hook-runner-global.js");
  vi.doUnmock("./hooks.js");
  vi.doUnmock("./loader.js");
  vi.doUnmock("jiti");
  const [loader, hookRunnerGlobal, hooks, runtime, registry] = await Promise.all([
    import("./loader.js"),
    import("./hook-runner-global.js"),
    import("./hooks.js"),
    import("./runtime.js"),
    import("./registry.js"),
  ]);
  return {
    ...loader,
    ...hookRunnerGlobal,
    ...hooks,
    ...runtime,
    ...registry,
  };
}

export type TempPlugin = { dir: string; file: string; id: string };
export type PluginTestModules = Awaited<ReturnType<typeof importFreshPluginTestModules>>;
export type PluginLoadConfig = NonNullable<
  Parameters<PluginTestModules["loadDenebPlugins"]>[0]
>["config"];

function chmodSafeDir(dir: string) {
  if (process.platform === "win32") {
    return;
  }
  fs.chmodSync(dir, 0o755);
}

function mkdtempSafe(prefix: string) {
  const dir = fs.mkdtempSync(prefix);
  chmodSafeDir(dir);
  return dir;
}

export function mkdirSafe(dir: string) {
  fs.mkdirSync(dir, { recursive: true });
  chmodSafeDir(dir);
}

export const EMPTY_PLUGIN_SCHEMA = { type: "object", additionalProperties: false, properties: {} };

export const BUNDLED_TELEGRAM_PLUGIN_BODY = `module.exports = {
  id: "telegram",
  register(api) {
    api.registerChannel({
      plugin: {
        id: "telegram",
        meta: {
          id: "telegram",
          label: "Telegram",
          selectionLabel: "Telegram",
          docsPath: "/channels/telegram",
          blurb: "telegram channel",
        },
        capabilities: { chatTypes: ["direct"] },
        config: {
          listAccountIds: () => [],
          resolveAccount: () => ({ accountId: "default" }),
        },
        outbound: { deliveryMode: "direct" },
      },
    });
  },
};`;

/**
 * Creates an isolated fixture context for plugin loader tests.
 * Manages temp directories, env state, and cleanup automatically.
 */
export function createPluginFixtureContext() {
  const fixtureRoot = mkdtempSafe(path.join(os.tmpdir(), "deneb-plugin-"));
  let tempDirIndex = 0;
  const prevBundledDir = process.env.DENEB_BUNDLED_PLUGINS_DIR;
  let cachedBundledTelegramDir = "";
  let cachedBundledMemoryDir = "";

  function makeTempDir() {
    const dir = path.join(fixtureRoot, `case-${tempDirIndex++}`);
    mkdirSafe(dir);
    return dir;
  }

  function writePlugin(params: {
    id: string;
    body: string;
    dir?: string;
    filename?: string;
  }): TempPlugin {
    const dir = params.dir ?? makeTempDir();
    const filename = params.filename ?? `${params.id}.cjs`;
    mkdirSafe(dir);
    const file = path.join(dir, filename);
    fs.writeFileSync(file, params.body, "utf-8");
    fs.writeFileSync(
      path.join(dir, "deneb.plugin.json"),
      JSON.stringify(
        {
          id: params.id,
          configSchema: EMPTY_PLUGIN_SCHEMA,
        },
        null,
        2,
      ),
      "utf-8",
    );
    return { dir, file, id: params.id };
  }

  function useNoBundledPlugins() {
    process.env.DENEB_BUNDLED_PLUGINS_DIR = "/nonexistent/bundled/plugins";
  }

  function setupBundledTelegramPlugin() {
    if (!cachedBundledTelegramDir) {
      cachedBundledTelegramDir = makeTempDir();
      writePlugin({
        id: "telegram",
        body: BUNDLED_TELEGRAM_PLUGIN_BODY,
        dir: cachedBundledTelegramDir,
        filename: "telegram.cjs",
      });
    }
    process.env.DENEB_BUNDLED_PLUGINS_DIR = cachedBundledTelegramDir;
  }

  function getCachedBundledTelegramDir() {
    return cachedBundledTelegramDir;
  }

  function loadBundledMemoryPluginRegistry(
    loadDenebPlugins: PluginTestModules["loadDenebPlugins"],
    options?: {
      packageMeta?: { name: string; version: string; description?: string };
      pluginBody?: string;
      pluginFilename?: string;
    },
  ) {
    if (!options && cachedBundledMemoryDir) {
      process.env.DENEB_BUNDLED_PLUGINS_DIR = cachedBundledMemoryDir;
      return loadDenebPlugins({
        cache: false,
        workspaceDir: cachedBundledMemoryDir,
        config: {
          plugins: {
            slots: {
              memory: "memory-core",
            },
          },
        },
      });
    }

    const bundledDir = makeTempDir();
    let pluginDir = bundledDir;
    let pluginFilename = options?.pluginFilename ?? "memory-core.cjs";

    if (options?.packageMeta) {
      pluginDir = path.join(bundledDir, "memory-core");
      pluginFilename = options.pluginFilename ?? "index.js";
      mkdirSafe(pluginDir);
      fs.writeFileSync(
        path.join(pluginDir, "package.json"),
        JSON.stringify(
          {
            name: options.packageMeta.name,
            version: options.packageMeta.version,
            description: options.packageMeta.description,
            deneb: { extensions: [`./${pluginFilename}`] },
          },
          null,
          2,
        ),
        "utf-8",
      );
    }

    writePlugin({
      id: "memory-core",
      body:
        options?.pluginBody ??
        `module.exports = { id: "memory-core", kind: "memory", register() {} };`,
      dir: pluginDir,
      filename: pluginFilename,
    });
    if (!options) {
      cachedBundledMemoryDir = bundledDir;
    }
    process.env.DENEB_BUNDLED_PLUGINS_DIR = bundledDir;

    return loadDenebPlugins({
      cache: false,
      workspaceDir: bundledDir,
      config: {
        plugins: {
          slots: {
            memory: "memory-core",
          },
        },
      },
    });
  }

  function cleanupAfterEach(clearPluginLoaderCache: PluginTestModules["clearPluginLoaderCache"]) {
    clearPluginLoaderCache();
    if (prevBundledDir === undefined) {
      delete process.env.DENEB_BUNDLED_PLUGINS_DIR;
    } else {
      process.env.DENEB_BUNDLED_PLUGINS_DIR = prevBundledDir;
    }
  }

  function cleanupAfterAll() {
    try {
      fs.rmSync(fixtureRoot, { recursive: true, force: true });
    } catch {
      // ignore cleanup failures
    } finally {
      cachedBundledTelegramDir = "";
      cachedBundledMemoryDir = "";
    }
  }

  return {
    fixtureRoot,
    makeTempDir,
    writePlugin,
    useNoBundledPlugins,
    setupBundledTelegramPlugin,
    getCachedBundledTelegramDir,
    loadBundledMemoryPluginRegistry,
    cleanupAfterEach,
    cleanupAfterAll,
  };
}

export function withCwd<T>(cwd: string, run: () => T): T {
  const previousCwd = process.cwd();
  process.chdir(cwd);
  try {
    return run();
  } finally {
    process.chdir(previousCwd);
  }
}

export function createWarningLogger(warnings: string[]) {
  return {
    info: () => {},
    warn: (msg: string) => warnings.push(msg),
    error: () => {},
  };
}

export function createErrorLogger(errors: string[]) {
  return {
    info: () => {},
    warn: () => {},
    error: (msg: string) => errors.push(msg),
    debug: () => {},
  };
}

export function loadRegistryFromSinglePlugin(
  loadDenebPlugins: PluginTestModules["loadDenebPlugins"],
  params: {
    plugin: TempPlugin;
    pluginConfig?: Record<string, unknown>;
    includeWorkspaceDir?: boolean;
    options?: Omit<
      Parameters<PluginTestModules["loadDenebPlugins"]>[0],
      "cache" | "workspaceDir" | "config"
    >;
  },
) {
  const pluginConfig = params.pluginConfig ?? {};
  return loadDenebPlugins({
    cache: false,
    ...(params.includeWorkspaceDir === false ? {} : { workspaceDir: params.plugin.dir }),
    ...params.options,
    config: {
      plugins: {
        load: { paths: [params.plugin.file] },
        ...pluginConfig,
      },
    },
  });
}

export function loadRegistryFromAllowedPlugins(
  loadDenebPlugins: PluginTestModules["loadDenebPlugins"],
  plugins: TempPlugin[],
  options?: Omit<Parameters<PluginTestModules["loadDenebPlugins"]>[0], "cache" | "config">,
) {
  return loadDenebPlugins({
    cache: false,
    ...options,
    config: {
      plugins: {
        load: { paths: plugins.map((plugin) => plugin.file) },
        allow: plugins.map((plugin) => plugin.id),
      },
    },
  });
}

export function createPluginSdkAliasFixture(
  makeTempDir: () => string,
  params?: {
    srcFile?: string;
    distFile?: string;
    srcBody?: string;
    distBody?: string;
    packageName?: string;
    packageExports?: Record<string, unknown>;
    trustedRootIndicators?: boolean;
    trustedRootIndicatorMode?: "bin+marker" | "cli-entry-only" | "none";
  },
) {
  const root = makeTempDir();
  const srcFile = path.join(root, "src", "plugin-sdk", params?.srcFile ?? "index.ts");
  const distFile = path.join(root, "dist", "plugin-sdk", params?.distFile ?? "index.js");
  mkdirSafe(path.dirname(srcFile));
  mkdirSafe(path.dirname(distFile));
  const trustedRootIndicatorMode =
    params?.trustedRootIndicatorMode ??
    (params?.trustedRootIndicators === false ? "none" : "bin+marker");
  const packageJson: Record<string, unknown> = {
    name: params?.packageName ?? "deneb",
    type: "module",
  };
  if (trustedRootIndicatorMode === "bin+marker") {
    packageJson.bin = {
      deneb: "deneb.mjs",
    };
  }
  if (params?.packageExports || trustedRootIndicatorMode === "cli-entry-only") {
    const trustedExports: Record<string, unknown> =
      trustedRootIndicatorMode === "cli-entry-only"
        ? { "./cli-entry": { default: "./dist/cli-entry.js" } }
        : {};
    packageJson.exports = {
      "./plugin-sdk": { default: "./dist/plugin-sdk/index.js" },
      ...trustedExports,
      ...params?.packageExports,
    };
  }
  fs.writeFileSync(path.join(root, "package.json"), JSON.stringify(packageJson, null, 2), "utf-8");
  if (trustedRootIndicatorMode === "bin+marker") {
    fs.writeFileSync(path.join(root, "deneb.mjs"), "export {};\n", "utf-8");
  }
  fs.writeFileSync(srcFile, params?.srcBody ?? "export {};\n", "utf-8");
  fs.writeFileSync(distFile, params?.distBody ?? "export {};\n", "utf-8");
  return { root, srcFile, distFile };
}

export function createPluginRuntimeAliasFixture(
  makeTempDir: () => string,
  params?: { srcBody?: string; distBody?: string },
) {
  const root = makeTempDir();
  const srcFile = path.join(root, "src", "plugins", "runtime", "index.ts");
  const distFile = path.join(root, "dist", "plugins", "runtime", "index.js");
  mkdirSafe(path.dirname(srcFile));
  mkdirSafe(path.dirname(distFile));
  fs.writeFileSync(
    path.join(root, "package.json"),
    JSON.stringify({ name: "deneb", type: "module" }, null, 2),
    "utf-8",
  );
  fs.writeFileSync(
    srcFile,
    params?.srcBody ?? "export const createPluginRuntime = () => ({});\n",
    "utf-8",
  );
  fs.writeFileSync(
    distFile,
    params?.distBody ?? "export const createPluginRuntime = () => ({});\n",
    "utf-8",
  );
  return { root, srcFile, distFile };
}

export function resolveLoadedPluginSource(
  registry: ReturnType<PluginTestModules["loadDenebPlugins"]>,
  pluginId: string,
) {
  return fs.realpathSync(
    registry.plugins.find((entry: { id: string }) => entry.id === pluginId)?.source ?? "",
  );
}
