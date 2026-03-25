import path from "node:path";
import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("node:fs", async (importOriginal) => {
  const actual = await importOriginal<typeof import("node:fs")>();
  const existsSync = vi.fn();
  return {
    ...actual,
    existsSync,
    default: {
      ...actual,
      existsSync,
    },
  };
});

const installPluginFromNpmSpec = vi.fn();
vi.mock("../../plugins/install.js", () => ({
  installPluginFromNpmSpec: (...args: unknown[]) => installPluginFromNpmSpec(...args),
}));

const resolveBundledPluginSources = vi.fn();
vi.mock("../../plugins/bundled-sources.js", () => ({
  findBundledPluginSourceInMap: ({
    bundled,
    lookup,
  }: {
    bundled: ReadonlyMap<string, { pluginId: string; localPath: string; npmSpec?: string }>;
    lookup: { kind: "pluginId" | "npmSpec"; value: string };
  }) => {
    const targetValue = lookup.value.trim();
    if (!targetValue) {
      return undefined;
    }
    if (lookup.kind === "pluginId") {
      return bundled.get(targetValue);
    }
    for (const source of bundled.values()) {
      if (source.npmSpec === targetValue) {
        return source;
      }
    }
    return undefined;
  },
  resolveBundledPluginSources: (...args: unknown[]) => resolveBundledPluginSources(...args),
}));

vi.mock("../../plugins/loader.js", () => ({
  loadDenebPlugins: vi.fn(),
}));

const clearPluginDiscoveryCache = vi.fn();
vi.mock("../../plugins/discovery.js", () => ({
  clearPluginDiscoveryCache: () => clearPluginDiscoveryCache(),
}));

import fs from "node:fs";
import type { ChannelPluginCatalogEntry } from "../../channels/plugins/catalog.js";
import type { DenebConfig } from "../../config/config.js";
import { loadDenebPlugins } from "../../plugins/loader.js";
import { createEmptyPluginRegistry } from "../../plugins/registry.js";
import { setActivePluginRegistry } from "../../plugins/runtime.js";
import type { WizardPrompter } from "../../wizard/prompts.js";
import { makePrompter, makeRuntime } from "../setup/__tests__/test-utils.js";
import {
  ensureChannelSetupPluginInstalled,
  loadChannelSetupPluginRegistrySnapshotForChannel,
  reloadChannelSetupPluginRegistry,
  reloadChannelSetupPluginRegistryForChannel,
} from "./plugin-install.js";

const baseEntry: ChannelPluginCatalogEntry = {
  id: "matrix",
  pluginId: "matrix",
  meta: {
    id: "matrix",
    label: "Matrix",
    selectionLabel: "Matrix (Protocol)",
    docsPath: "/channels/matrix",
    docsLabel: "matrix",
    blurb: "Test",
  },
  install: {
    npmSpec: "@deneb/matrix",
    localPath: "extensions/matrix",
  },
};

beforeEach(() => {
  vi.clearAllMocks();
  resolveBundledPluginSources.mockReturnValue(new Map());
  setActivePluginRegistry(createEmptyPluginRegistry());
});

function mockRepoLocalPathExists() {
  vi.mocked(fs.existsSync).mockImplementation((value) => {
    const raw = String(value);
    return (
      raw.endsWith(`${path.sep}.git`) || raw.endsWith(`${path.sep}extensions${path.sep}matrix`)
    );
  });
}

async function runInitialValueForChannel(channel: "dev" | "beta") {
  const runtime = makeRuntime();
  const select = vi.fn((async <T extends string>() => "skip" as T) as WizardPrompter["select"]);
  const prompter = makePrompter({ select: select as unknown as WizardPrompter["select"] });
  const cfg: DenebConfig = { update: { channel } };
  mockRepoLocalPathExists();

  await ensureChannelSetupPluginInstalled({
    cfg,
    entry: baseEntry,
    prompter,
    runtime,
  });

  const call = select.mock.calls[0];
  return call?.[0]?.initialValue;
}

function expectPluginLoadedFromLocalPath(
  result: Awaited<ReturnType<typeof ensureChannelSetupPluginInstalled>>,
) {
  const expectedPath = path.resolve(process.cwd(), "extensions/matrix");
  expect(result.installed).toBe(true);
  expect(result.cfg.plugins?.load?.paths).toContain(expectedPath);
}

describe("ensureChannelSetupPluginInstalled", () => {
  it("installs from npm and enables the plugin", async () => {
    const runtime = makeRuntime();
    const prompter = makePrompter({
      select: vi.fn(async () => "npm") as WizardPrompter["select"],
    });
    const cfg: DenebConfig = { plugins: { allow: ["other"] } };
    vi.mocked(fs.existsSync).mockReturnValue(false);
    installPluginFromNpmSpec.mockResolvedValue({
      ok: true,
      pluginId: "matrix",
      targetDir: "/tmp/matrix",
      extensions: [],
    });

    const result = await ensureChannelSetupPluginInstalled({
      cfg,
      entry: baseEntry,
      prompter,
      runtime,
    });

    expect(result.installed).toBe(true);
    expect(result.cfg.plugins?.entries?.matrix?.enabled).toBe(true);
    expect(result.cfg.plugins?.allow).toContain("matrix");
    expect(result.cfg.plugins?.installs?.matrix?.source).toBe("npm");
    expect(result.cfg.plugins?.installs?.matrix?.spec).toBe("@deneb/matrix");
    expect(result.cfg.plugins?.installs?.matrix?.installPath).toBe("/tmp/matrix");
    expect(installPluginFromNpmSpec).toHaveBeenCalledWith(
      expect.objectContaining({ spec: "@deneb/matrix" }),
    );
  });

  it("uses local path when selected", async () => {
    const runtime = makeRuntime();
    const prompter = makePrompter({
      select: vi.fn(async () => "local") as WizardPrompter["select"],
    });
    const cfg: DenebConfig = {};
    mockRepoLocalPathExists();

    const result = await ensureChannelSetupPluginInstalled({
      cfg,
      entry: baseEntry,
      prompter,
      runtime,
    });

    expectPluginLoadedFromLocalPath(result);
    expect(result.cfg.plugins?.entries?.matrix?.enabled).toBe(true);
  });

  it("uses the catalog plugin id for local-path installs", async () => {
    const runtime = makeRuntime();
    const prompter = makePrompter({
      select: vi.fn(async () => "local") as WizardPrompter["select"],
    });
    const cfg: DenebConfig = {};
    mockRepoLocalPathExists();

    const result = await ensureChannelSetupPluginInstalled({
      cfg,
      entry: {
        ...baseEntry,
        id: "teams",
        pluginId: "@deneb/nostr-plugin",
      },
      prompter,
      runtime,
    });

    expect(result.installed).toBe(true);
    expect(result.pluginId).toBe("@deneb/nostr-plugin");
    expect(result.cfg.plugins?.entries?.["@deneb/nostr-plugin"]?.enabled).toBe(true);
  });

  it("defaults to local on dev channel when local path exists", async () => {
    expect(await runInitialValueForChannel("dev")).toBe("local");
  });

  it("defaults to npm on beta channel even when local path exists", async () => {
    expect(await runInitialValueForChannel("beta")).toBe("npm");
  });

  it("defaults to bundled local path on beta channel when available", async () => {
    const runtime = makeRuntime();
    const select = vi.fn((async <T extends string>() => "skip" as T) as WizardPrompter["select"]);
    const prompter = makePrompter({ select: select as unknown as WizardPrompter["select"] });
    const cfg: DenebConfig = { update: { channel: "beta" } };
    vi.mocked(fs.existsSync).mockReturnValue(false);
    resolveBundledPluginSources.mockReturnValue(
      new Map([
        [
          "zalo",
          {
            pluginId: "matrix",
            localPath: "/opt/deneb/extensions/matrix",
            npmSpec: "@deneb/matrix",
          },
        ],
      ]),
    );

    await ensureChannelSetupPluginInstalled({
      cfg,
      entry: baseEntry,
      prompter,
      runtime,
    });

    expect(select).toHaveBeenCalledWith(
      expect.objectContaining({
        initialValue: "local",
        options: expect.arrayContaining([
          expect.objectContaining({
            value: "local",
            hint: "/opt/deneb/extensions/matrix",
          }),
        ]),
      }),
    );
  });

  it("falls back to local path after npm install failure", async () => {
    const runtime = makeRuntime();
    const note = vi.fn(async () => {});
    const confirm = vi.fn(async () => true);
    const prompter = makePrompter({
      select: vi.fn(async () => "npm") as WizardPrompter["select"],
      note,
      confirm,
    });
    const cfg: DenebConfig = {};
    mockRepoLocalPathExists();
    installPluginFromNpmSpec.mockResolvedValue({
      ok: false,
      error: "nope",
    });

    const result = await ensureChannelSetupPluginInstalled({
      cfg,
      entry: baseEntry,
      prompter,
      runtime,
    });

    expectPluginLoadedFromLocalPath(result);
    expect(note).toHaveBeenCalled();
    expect(runtime.error).not.toHaveBeenCalled();
  });

  it("clears discovery cache before reloading the setup plugin registry", () => {
    const runtime = makeRuntime();
    const cfg: DenebConfig = {};

    reloadChannelSetupPluginRegistry({
      cfg,
      runtime,
      workspaceDir: "/tmp/deneb-workspace",
    });

    expect(clearPluginDiscoveryCache).toHaveBeenCalledTimes(1);
    expect(loadDenebPlugins).toHaveBeenCalledWith(
      expect.objectContaining({
        config: cfg,
        workspaceDir: "/tmp/deneb-workspace",
        cache: false,
        includeSetupOnlyChannelPlugins: true,
      }),
    );
    expect(clearPluginDiscoveryCache.mock.invocationCallOrder[0]).toBeLessThan(
      vi.mocked(loadDenebPlugins).mock.invocationCallOrder[0] ?? Number.POSITIVE_INFINITY,
    );
  });

  it("scopes channel reloads when setup starts from an empty registry", () => {
    const runtime = makeRuntime();
    const cfg: DenebConfig = {};

    reloadChannelSetupPluginRegistryForChannel({
      cfg,
      runtime,
      channel: "telegram",
      workspaceDir: "/tmp/deneb-workspace",
    });

    expect(loadDenebPlugins).toHaveBeenCalledWith(
      expect.objectContaining({
        config: cfg,
        workspaceDir: "/tmp/deneb-workspace",
        cache: false,
        onlyPluginIds: ["telegram"],
        includeSetupOnlyChannelPlugins: true,
      }),
    );
  });

  it("keeps full reloads when the active plugin registry is already populated", () => {
    const runtime = makeRuntime();
    const cfg: DenebConfig = {};
    const registry = createEmptyPluginRegistry();
    registry.plugins.push({
      id: "loaded",
      name: "loaded",
      source: "/tmp/loaded.cjs",
      origin: "bundled",
      enabled: true,
      status: "loaded",
      toolNames: [],
      hookNames: [],
      channelIds: [],
      providerIds: [],
      speechProviderIds: [],
      mediaUnderstandingProviderIds: [],
      imageGenerationProviderIds: [],
      webSearchProviderIds: [],
      gatewayMethods: [],
      cliCommands: [],
      services: [],
      commands: [],
      httpRoutes: 0,
      hookCount: 0,
      configSchema: true,
    });
    setActivePluginRegistry(registry);

    reloadChannelSetupPluginRegistryForChannel({
      cfg,
      runtime,
      channel: "telegram",
      workspaceDir: "/tmp/deneb-workspace",
    });

    expect(loadDenebPlugins).toHaveBeenCalledWith(
      expect.not.objectContaining({
        onlyPluginIds: expect.anything(),
      }),
    );
  });

  it("can load a channel-scoped snapshot without activating the global registry", () => {
    const runtime = makeRuntime();
    const cfg: DenebConfig = {};

    loadChannelSetupPluginRegistrySnapshotForChannel({
      cfg,
      runtime,
      channel: "telegram",
      workspaceDir: "/tmp/deneb-workspace",
    });

    expect(loadDenebPlugins).toHaveBeenCalledWith(
      expect.objectContaining({
        config: cfg,
        workspaceDir: "/tmp/deneb-workspace",
        cache: false,
        onlyPluginIds: ["telegram"],
        includeSetupOnlyChannelPlugins: true,
        activate: false,
      }),
    );
  });

  it("scopes snapshots by plugin id when channel and plugin ids differ", () => {
    const runtime = makeRuntime();
    const cfg: DenebConfig = {};

    loadChannelSetupPluginRegistrySnapshotForChannel({
      cfg,
      runtime,
      channel: "nostr",
      pluginId: "@deneb/nostr-plugin",
      workspaceDir: "/tmp/deneb-workspace",
    });

    expect(loadDenebPlugins).toHaveBeenCalledWith(
      expect.objectContaining({
        config: cfg,
        workspaceDir: "/tmp/deneb-workspace",
        cache: false,
        onlyPluginIds: ["@deneb/nostr-plugin"],
        includeSetupOnlyChannelPlugins: true,
        activate: false,
      }),
    );
  });
});
