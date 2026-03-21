import fs from "node:fs";
import path from "node:path";
import { defineConfig, type UserConfig } from "tsdown";
import { shouldBuildBundledCluster } from "./scripts/lib/optional-bundled-clusters.mjs";
import { buildPluginSdkEntrySources } from "./scripts/lib/plugin-sdk-entries.mjs";

type InputOptionsFactory = Extract<NonNullable<UserConfig["inputOptions"]>, Function>;
type InputOptionsArg = InputOptionsFactory extends (
  options: infer Options,
  format: infer _Format,
  context: infer _Context,
) => infer _Return
  ? Options
  : never;
type InputOptionsReturn = InputOptionsFactory extends (
  options: infer _Options,
  format: infer _Format,
  context: infer _Context,
) => infer Return
  ? Return
  : never;
type OnLogFunction = InputOptionsArg extends { onLog?: infer OnLog } ? NonNullable<OnLog> : never;

const env = {
  NODE_ENV: "production",
};

function buildInputOptions(options: InputOptionsArg): InputOptionsReturn {
  if (process.env.DENEB_BUILD_VERBOSE === "1") {
    return undefined;
  }

  const previousOnLog = typeof options.onLog === "function" ? options.onLog : undefined;

  function isSuppressedLog(log: {
    code?: string;
    message?: string;
    id?: string;
    importer?: string;
  }) {
    if (log.code === "PLUGIN_TIMINGS") {
      return true;
    }
    if (log.code !== "EVAL") {
      return false;
    }
    const haystack = [log.message, log.id, log.importer].filter(Boolean).join("\n");
    return haystack.includes("@protobufjs/inquire/index.js");
  }

  return {
    ...options,
    onLog(...args: Parameters<OnLogFunction>) {
      const [level, log, defaultHandler] = args;
      if (isSuppressedLog(log)) {
        return;
      }
      if (typeof previousOnLog === "function") {
        previousOnLog(level, log, defaultHandler);
        return;
      }
      defaultHandler(level, log);
    },
  };
}

// Rolldown plugin: when resolving a .js import under extensions/, prefer the
// .ts source file if it exists.  This prevents stale compiled .js artifacts
// (left by a previous build or tsc) from shadowing the real TypeScript source.
function preferTsInExtensions(): import("rolldown").Plugin {
  return {
    name: "prefer-ts-in-extensions",
    resolveId: {
      filter: { id: /extensions\/.*\.js$/ },
      async handler(source, importer, options) {
        if (!importer) {
          return null;
        }
        const resolved = path.resolve(path.dirname(importer), source);
        if (!resolved.includes(`${path.sep}extensions${path.sep}`)) {
          return null;
        }
        const tsPath = resolved.replace(/\.js$/, ".ts");
        if (fs.existsSync(tsPath)) {
          // Delegate back to rolldown with the .ts path so it goes through
          // normal TypeScript handling.
          return this.resolve(tsPath, importer, { ...options, skipSelf: true });
        }
        return null;
      },
    },
  };
}

function nodeBuildConfig(config: UserConfig): UserConfig {
  return {
    ...config,
    env,
    fixedExtension: false,
    platform: "node",
    inputOptions: buildInputOptions,
    plugins: [...(Array.isArray(config.plugins) ? config.plugins : []), preferTsInExtensions()],
  };
}

function listBundledPluginBuildEntries(): Record<string, string> {
  const extensionsRoot = path.join(process.cwd(), "extensions");
  const entries: Record<string, string> = {};

  for (const dirent of fs.readdirSync(extensionsRoot, { withFileTypes: true })) {
    if (!dirent.isDirectory()) {
      continue;
    }
    if (!shouldBuildBundledCluster(dirent.name, process.env)) {
      continue;
    }

    const pluginDir = path.join(extensionsRoot, dirent.name);
    const manifestPath = path.join(pluginDir, "deneb.plugin.json");
    if (!fs.existsSync(manifestPath)) {
      continue;
    }

    const packageJsonPath = path.join(pluginDir, "package.json");
    let packageEntries: string[] = [];
    if (fs.existsSync(packageJsonPath)) {
      try {
        const packageJson = JSON.parse(fs.readFileSync(packageJsonPath, "utf8")) as {
          deneb?: { extensions?: unknown; setupEntry?: unknown };
        };
        packageEntries = Array.isArray(packageJson.deneb?.extensions)
          ? packageJson.deneb.extensions.filter(
              (entry): entry is string => typeof entry === "string" && entry.trim().length > 0,
            )
          : [];
        const setupEntry =
          typeof packageJson.deneb?.setupEntry === "string" &&
          packageJson.deneb.setupEntry.trim().length > 0
            ? packageJson.deneb.setupEntry
            : undefined;
        if (setupEntry) {
          packageEntries = Array.from(new Set([...packageEntries, setupEntry]));
        }
      } catch {
        packageEntries = [];
      }
    }

    const sourceEntries = packageEntries.length > 0 ? packageEntries : ["./index.ts"];
    for (const entry of sourceEntries) {
      const normalizedEntry = entry.replace(/^\.\//, "");
      const entryKey = `extensions/${dirent.name}/${normalizedEntry.replace(/\.[^.]+$/u, "")}`;
      entries[entryKey] = path.join("extensions", dirent.name, normalizedEntry);
    }
  }

  return entries;
}

const bundledPluginBuildEntries = listBundledPluginBuildEntries();

function buildBundledHookEntries(): Record<string, string> {
  const hooksRoot = path.join(process.cwd(), "src", "hooks", "bundled");
  const entries: Record<string, string> = {};

  if (!fs.existsSync(hooksRoot)) {
    return entries;
  }

  for (const dirent of fs.readdirSync(hooksRoot, { withFileTypes: true })) {
    if (!dirent.isDirectory()) {
      continue;
    }

    const hookName = dirent.name;
    const handlerPath = path.join(hooksRoot, hookName, "handler.ts");
    if (!fs.existsSync(handlerPath)) {
      continue;
    }

    entries[`bundled/${hookName}/handler`] = handlerPath;
  }

  return entries;
}

const bundledHookEntries = buildBundledHookEntries();

function buildCoreDistEntries(): Record<string, string> {
  return {
    index: "src/index.ts",
    entry: "src/entry.ts",
    // Ensure this module is bundled as an entry so legacy CLI shims can resolve its exports.
    "cli/daemon-cli": "src/cli/daemon-cli.ts",
    "infra/warning-filter": "src/infra/warning-filter.ts",
    "telegram/audit": "extensions/telegram/src/audit.ts",
    "telegram/token": "extensions/telegram/src/token.ts",

    "plugins/build-smoke-entry": "src/plugins/build-smoke-entry.ts",
    "plugins/runtime/index": "src/plugins/runtime/index.ts",
    "llm-slug-generator": "src/hooks/llm-slug-generator.ts",
  };
}

const coreDistEntries = buildCoreDistEntries();

function buildUnifiedDistEntries(): Record<string, string> {
  return {
    ...coreDistEntries,
    ...Object.fromEntries(
      Object.entries(buildPluginSdkEntrySources()).map(([entry, source]) => [
        `plugin-sdk/${entry}`,
        source,
      ]),
    ),
    ...bundledPluginBuildEntries,
    ...bundledHookEntries,
  };
}

export default defineConfig([
  nodeBuildConfig({
    // Build core entrypoints, plugin-sdk subpaths, bundled plugin entrypoints,
    // and bundled hooks in one graph so runtime singletons are emitted once.
    entry: buildUnifiedDistEntries(),
    deps: {
      neverBundle: ["@lancedb/lancedb"],
    },
  }),
]);
