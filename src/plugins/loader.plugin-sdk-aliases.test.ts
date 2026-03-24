/**
 * Plugin-SDK alias resolution and Jiti boundary tests.
 *
 * Extracted from loader.test.ts to enable parallel test execution.
 */
import { execFileSync } from "node:child_process";
import fs from "node:fs";
import path from "node:path";
import { pathToFileURL } from "node:url";
import { afterAll, afterEach, describe, expect, it } from "vitest";
import { withEnv } from "../test-utils/env.js";
import {
  createPluginFixtureContext,
  createPluginRuntimeAliasFixture,
  createPluginSdkAliasFixture,
  importFreshPluginTestModules,
  withCwd,
} from "./loader-test-helpers.js";

const { __testing, clearPluginLoaderCache } = await importFreshPluginTestModules();

const ctx = createPluginFixtureContext();
const { makeTempDir, writePlugin, useNoBundledPlugins } = ctx;

afterEach(() => {
  ctx.cleanupAfterEach(clearPluginLoaderCache);
});

afterAll(() => {
  ctx.cleanupAfterAll();
});

function resolvePluginSdkAlias(params: {
  root: string;
  srcFile: string;
  distFile: string;
  modulePath: string;
  argv1?: string;
  env?: NodeJS.ProcessEnv;
}) {
  const run = () =>
    __testing.resolvePluginSdkAliasFile({
      srcFile: params.srcFile,
      distFile: params.distFile,
      modulePath: params.modulePath,
      argv1: params.argv1,
    });
  return params.env ? withEnv(params.env, run) : run();
}

function listPluginSdkAliasCandidates(params: {
  root: string;
  srcFile: string;
  distFile: string;
  modulePath: string;
  env?: NodeJS.ProcessEnv;
}) {
  const run = () =>
    __testing.listPluginSdkAliasCandidates({
      srcFile: params.srcFile,
      distFile: params.distFile,
      modulePath: params.modulePath,
    });
  return params.env ? withEnv(params.env, run) : run();
}

function resolvePluginRuntimeModule(params: {
  modulePath: string;
  argv1?: string;
  env?: NodeJS.ProcessEnv;
}) {
  const run = () =>
    __testing.resolvePluginRuntimeModulePath({
      modulePath: params.modulePath,
      argv1: params.argv1,
    });
  return params.env ? withEnv(params.env, run) : run();
}

describe("plugin-sdk alias resolution", () => {
  it("supports legacy plugins importing monolithic plugin-sdk root", async () => {
    useNoBundledPlugins();
    const plugin = writePlugin({
      id: "legacy-root-import",
      filename: "legacy-root-import.cjs",
      body: `module.exports = {
  id: "legacy-root-import",
  configSchema: (require("deneb/plugin-sdk").emptyPluginConfigSchema)(),
  register() {},
};`,
    });

    const loaderModuleUrl = pathToFileURL(
      path.join(process.cwd(), "src", "plugins", "loader.ts"),
    ).href;
    const script = `
      import { loadDenebPlugins } from ${JSON.stringify(loaderModuleUrl)};
      const registry = loadDenebPlugins({
        cache: false,
        workspaceDir: ${JSON.stringify(plugin.dir)},
        config: {
          plugins: {
            load: { paths: [${JSON.stringify(plugin.file)}] },
            allow: ["legacy-root-import"],
          },
        },
      });
      const record = registry.plugins.find((entry) => entry.id === "legacy-root-import");
      if (!record || record.status !== "loaded") {
        console.error(record?.error ?? "legacy-root-import missing");
        process.exit(1);
      }
    `;

    execFileSync(process.execPath, ["--import", "tsx", "--input-type=module", "-e", script], {
      cwd: process.cwd(),
      env: {
        ...process.env,
        DENEB_HOME: undefined,
        DENEB_BUNDLED_PLUGINS_DIR: "/nonexistent/bundled/plugins",
      },
      encoding: "utf-8",
      stdio: "pipe",
    });
  });

  it.each([
    {
      name: "prefers dist plugin-sdk alias when loader runs from dist",
      buildFixture: () => createPluginSdkAliasFixture(makeTempDir),
      modulePath: (root: string) => path.join(root, "dist", "plugins", "loader.js"),
      srcFile: "index.ts",
      distFile: "index.js",
      expected: "dist" as const,
    },
    {
      name: "prefers src plugin-sdk alias when loader runs from src in non-production",
      buildFixture: () => createPluginSdkAliasFixture(makeTempDir),
      modulePath: (root: string) => path.join(root, "src", "plugins", "loader.ts"),
      srcFile: "index.ts",
      distFile: "index.js",
      env: { NODE_ENV: undefined },
      expected: "src" as const,
    },
    {
      name: "falls back to src plugin-sdk alias when dist is missing in production",
      buildFixture: () => {
        const fixture = createPluginSdkAliasFixture(makeTempDir);
        fs.rmSync(fixture.distFile);
        return fixture;
      },
      modulePath: (root: string) => path.join(root, "src", "plugins", "loader.ts"),
      srcFile: "index.ts",
      distFile: "index.js",
      env: { NODE_ENV: "production", VITEST: undefined },
      expected: "src" as const,
    },
    {
      name: "prefers dist root-alias shim when loader runs from dist",
      buildFixture: () =>
        createPluginSdkAliasFixture(makeTempDir, {
          srcFile: "root-alias.cjs",
          distFile: "root-alias.cjs",
          srcBody: "module.exports = {};\n",
          distBody: "module.exports = {};\n",
        }),
      modulePath: (root: string) => path.join(root, "dist", "plugins", "loader.js"),
      srcFile: "root-alias.cjs",
      distFile: "root-alias.cjs",
      expected: "dist" as const,
    },
    {
      name: "prefers src root-alias shim when loader runs from src in non-production",
      buildFixture: () =>
        createPluginSdkAliasFixture(makeTempDir, {
          srcFile: "root-alias.cjs",
          distFile: "root-alias.cjs",
          srcBody: "module.exports = {};\n",
          distBody: "module.exports = {};\n",
        }),
      modulePath: (root: string) => path.join(root, "src", "plugins", "loader.ts"),
      srcFile: "root-alias.cjs",
      distFile: "root-alias.cjs",
      env: { NODE_ENV: undefined },
      expected: "src" as const,
    },
    {
      name: "resolves plugin-sdk alias from package root when loader runs from transpiler cache path",
      buildFixture: () => createPluginSdkAliasFixture(makeTempDir),
      modulePath: () => "/tmp/tsx-cache/deneb-loader.js",
      argv1: (root: string) => path.join(root, "deneb.mjs"),
      srcFile: "index.ts",
      distFile: "index.js",
      env: { NODE_ENV: undefined },
      expected: "src" as const,
    },
  ])("$name", ({ buildFixture, modulePath, argv1, srcFile, distFile, env, expected }) => {
    const fixture = buildFixture();
    const resolved = resolvePluginSdkAlias({
      root: fixture.root,
      srcFile,
      distFile,
      modulePath: modulePath(fixture.root),
      argv1: argv1?.(fixture.root),
      env,
    });
    expect(resolved).toBe(expected === "dist" ? fixture.distFile : fixture.srcFile);
  });

  it.each([
    {
      name: "prefers dist candidates first for production src runtime",
      env: { NODE_ENV: "production", VITEST: undefined },
      expectedFirst: "dist" as const,
    },
    {
      name: "prefers src candidates first for non-production src runtime",
      env: { NODE_ENV: undefined },
      expectedFirst: "src" as const,
    },
  ])("$name", ({ env, expectedFirst }) => {
    const fixture = createPluginSdkAliasFixture(makeTempDir);
    const candidates = listPluginSdkAliasCandidates({
      root: fixture.root,
      srcFile: "index.ts",
      distFile: "index.js",
      modulePath: path.join(fixture.root, "src", "plugins", "loader.ts"),
      env,
    });
    const first = expectedFirst === "dist" ? fixture.distFile : fixture.srcFile;
    const second = expectedFirst === "dist" ? fixture.srcFile : fixture.distFile;
    expect(candidates.indexOf(first)).toBeLessThan(candidates.indexOf(second));
  });

  it("derives plugin-sdk subpaths from package exports", () => {
    const fixture = createPluginSdkAliasFixture(makeTempDir, {
      packageExports: {
        "./plugin-sdk/compat": { default: "./dist/plugin-sdk/compat.js" },
        "./plugin-sdk/telegram": { default: "./dist/plugin-sdk/telegram.js" },
        "./plugin-sdk/nested/value": { default: "./dist/plugin-sdk/nested/value.js" },
      },
    });
    const subpaths = __testing.listPluginSdkExportedSubpaths({
      modulePath: path.join(fixture.root, "src", "plugins", "loader.ts"),
    });
    expect(subpaths).toEqual(["compat", "telegram"]);
  });

  it("derives plugin-sdk subpaths from nearest package exports even when package name is renamed", () => {
    const fixture = createPluginSdkAliasFixture(makeTempDir, {
      packageName: "moltbot",
      packageExports: {
        "./plugin-sdk/core": { default: "./dist/plugin-sdk/core.js" },
        "./plugin-sdk/channel-runtime": { default: "./dist/plugin-sdk/channel-runtime.js" },
        "./plugin-sdk/compat": { default: "./dist/plugin-sdk/compat.js" },
      },
    });
    const subpaths = __testing.listPluginSdkExportedSubpaths({
      modulePath: path.join(fixture.root, "src", "plugins", "loader.ts"),
    });
    expect(subpaths).toEqual(["channel-runtime", "compat", "core"]);
  });

  it("derives plugin-sdk subpaths via cwd fallback when module path is a transpiler cache and package is renamed", () => {
    const fixture = createPluginSdkAliasFixture(makeTempDir, {
      packageName: "moltbot",
      packageExports: {
        "./plugin-sdk/core": { default: "./dist/plugin-sdk/core.js" },
        "./plugin-sdk/channel-runtime": { default: "./dist/plugin-sdk/channel-runtime.js" },
      },
    });
    const subpaths = withCwd(fixture.root, () =>
      __testing.listPluginSdkExportedSubpaths({
        modulePath: "/tmp/tsx-cache/deneb-loader.js",
      }),
    );
    expect(subpaths).toEqual(["channel-runtime", "core"]);
  });

  it("resolves plugin-sdk alias files via cwd fallback when module path is a transpiler cache and package is renamed", () => {
    const fixture = createPluginSdkAliasFixture(makeTempDir, {
      srcFile: "channel-runtime.ts",
      distFile: "channel-runtime.js",
      packageName: "moltbot",
      packageExports: {
        "./plugin-sdk/channel-runtime": { default: "./dist/plugin-sdk/channel-runtime.js" },
      },
    });
    const resolved = withCwd(fixture.root, () =>
      resolvePluginSdkAlias({
        root: fixture.root,
        srcFile: "channel-runtime.ts",
        distFile: "channel-runtime.js",
        modulePath: "/tmp/tsx-cache/deneb-loader.js",
        env: { NODE_ENV: undefined },
      }),
    );
    expect(resolved).not.toBeNull();
    expect(fs.realpathSync(resolved ?? "")).toBe(fs.realpathSync(fixture.srcFile));
  });

  it("does not derive plugin-sdk subpaths from cwd fallback when package root is not an Deneb root", () => {
    const fixture = createPluginSdkAliasFixture(makeTempDir, {
      packageName: "moltbot",
      trustedRootIndicators: false,
      packageExports: {
        "./plugin-sdk/core": { default: "./dist/plugin-sdk/core.js" },
        "./plugin-sdk/channel-runtime": { default: "./dist/plugin-sdk/channel-runtime.js" },
      },
    });
    const subpaths = withCwd(fixture.root, () =>
      __testing.listPluginSdkExportedSubpaths({
        modulePath: "/tmp/tsx-cache/deneb-loader.js",
      }),
    );
    expect(subpaths).toEqual([]);
  });

  it("derives plugin-sdk subpaths via cwd fallback when trusted root indicator is cli-entry export", () => {
    const fixture = createPluginSdkAliasFixture(makeTempDir, {
      packageName: "moltbot",
      trustedRootIndicatorMode: "cli-entry-only",
      packageExports: {
        "./plugin-sdk/core": { default: "./dist/plugin-sdk/core.js" },
        "./plugin-sdk/channel-runtime": { default: "./dist/plugin-sdk/channel-runtime.js" },
      },
    });
    const subpaths = withCwd(fixture.root, () =>
      __testing.listPluginSdkExportedSubpaths({
        modulePath: "/tmp/tsx-cache/deneb-loader.js",
      }),
    );
    expect(subpaths).toEqual(["channel-runtime", "core"]);
  });

  it("does not resolve plugin-sdk alias files from cwd fallback when package root is not an Deneb root", () => {
    const fixture = createPluginSdkAliasFixture(makeTempDir, {
      srcFile: "channel-runtime.ts",
      distFile: "channel-runtime.js",
      packageName: "moltbot",
      trustedRootIndicators: false,
      packageExports: {
        "./plugin-sdk/channel-runtime": { default: "./dist/plugin-sdk/channel-runtime.js" },
      },
    });
    const resolved = withCwd(fixture.root, () =>
      resolvePluginSdkAlias({
        root: fixture.root,
        srcFile: "channel-runtime.ts",
        distFile: "channel-runtime.js",
        modulePath: "/tmp/tsx-cache/deneb-loader.js",
        env: { NODE_ENV: undefined },
      }),
    );
    expect(resolved).toBeNull();
  });

  it("configures the plugin loader jiti boundary to prefer native dist modules", () => {
    const options = __testing.buildPluginLoaderJitiOptions({});

    expect(options.tryNative).toBe(true);
    expect(options.interopDefault).toBe(true);
    expect(options.extensions).toContain(".js");
    expect(options.extensions).toContain(".ts");
    expect("alias" in options).toBe(false);
  });

  it("uses transpiled Jiti loads for source TypeScript plugin entries", () => {
    expect(__testing.shouldPreferNativeJiti("/repo/dist/plugins/runtime/index.js")).toBe(true);
    expect(
      __testing.shouldPreferNativeJiti("/repo/extensions/discord/src/channel.runtime.ts"),
    ).toBe(false);
  });

  it.each([
    {
      name: "prefers dist plugin runtime module when loader runs from dist",
      modulePath: (root: string) => path.join(root, "dist", "plugins", "loader.js"),
      expected: "dist" as const,
    },
    {
      name: "resolves plugin runtime module from package root when loader runs from transpiler cache path",
      modulePath: () => "/tmp/tsx-cache/deneb-loader.js",
      argv1: (root: string) => path.join(root, "deneb.mjs"),
      env: { NODE_ENV: undefined },
      expected: "src" as const,
    },
  ])("$name", ({ modulePath, argv1, env, expected }) => {
    const fixture = createPluginRuntimeAliasFixture(makeTempDir);
    const resolved = resolvePluginRuntimeModule({
      modulePath: modulePath(fixture.root),
      argv1: argv1?.(fixture.root),
      env,
    });
    expect(resolved).toBe(expected === "dist" ? fixture.distFile : fixture.srcFile);
  });
});
