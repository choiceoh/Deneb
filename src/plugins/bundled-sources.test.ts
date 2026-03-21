import { beforeEach, describe, expect, it, vi } from "vitest";
import {
  findBundledPluginSource,
  findBundledPluginSourceInMap,
  resolveBundledPluginSources,
} from "./bundled-sources.js";

const discoverDenebPluginsMock = vi.fn();
const loadPluginManifestMock = vi.fn();

vi.mock("./discovery.js", () => ({
  discoverDenebPlugins: (...args: unknown[]) => discoverDenebPluginsMock(...args),
}));

vi.mock("./manifest.js", () => ({
  loadPluginManifest: (...args: unknown[]) => loadPluginManifestMock(...args),
}));

describe("bundled plugin sources", () => {
  beforeEach(() => {
    discoverDenebPluginsMock.mockReset();
    loadPluginManifestMock.mockReset();
  });

  it("resolves bundled sources keyed by plugin id", () => {
    discoverDenebPluginsMock.mockReturnValue({
      candidates: [
        {
          origin: "global",
          rootDir: "/global/feishu",
          packageName: "@deneb/feishu",
          packageManifest: { install: { npmSpec: "@deneb/feishu" } },
        },
        {
          origin: "bundled",
          rootDir: "/app/extensions/feishu",
          packageName: "@deneb/feishu",
          packageManifest: { install: { npmSpec: "@deneb/feishu" } },
        },
        {
          origin: "bundled",
          rootDir: "/app/extensions/feishu-dup",
          packageName: "@deneb/feishu",
          packageManifest: { install: { npmSpec: "@deneb/feishu" } },
        },
        {
          origin: "bundled",
          rootDir: "/app/extensions/msteams",
          packageName: "@deneb/msteams",
          packageManifest: { install: { npmSpec: "@deneb/msteams" } },
        },
      ],
      diagnostics: [],
    });

    loadPluginManifestMock.mockImplementation((rootDir: string) => {
      if (rootDir === "/app/extensions/feishu") {
        return { ok: true, manifest: { id: "feishu" } };
      }
      if (rootDir === "/app/extensions/msteams") {
        return { ok: true, manifest: { id: "msteams" } };
      }
      return {
        ok: false,
        error: "invalid manifest",
        manifestPath: `${rootDir}/deneb.plugin.json`,
      };
    });

    const map = resolveBundledPluginSources({});

    expect(Array.from(map.keys())).toEqual(["feishu", "msteams"]);
    expect(map.get("feishu")).toEqual({
      pluginId: "feishu",
      localPath: "/app/extensions/feishu",
      npmSpec: "@deneb/feishu",
    });
  });

  it("finds bundled source by npm spec", () => {
    discoverDenebPluginsMock.mockReturnValue({
      candidates: [
        {
          origin: "bundled",
          rootDir: "/app/extensions/feishu",
          packageName: "@deneb/feishu",
          packageManifest: { install: { npmSpec: "@deneb/feishu" } },
        },
      ],
      diagnostics: [],
    });
    loadPluginManifestMock.mockReturnValue({ ok: true, manifest: { id: "feishu" } });

    const resolved = findBundledPluginSource({
      lookup: { kind: "npmSpec", value: "@deneb/feishu" },
    });
    const missing = findBundledPluginSource({
      lookup: { kind: "npmSpec", value: "@deneb/not-found" },
    });

    expect(resolved?.pluginId).toBe("feishu");
    expect(resolved?.localPath).toBe("/app/extensions/feishu");
    expect(missing).toBeUndefined();
  });

  it("forwards an explicit env to bundled discovery helpers", () => {
    discoverDenebPluginsMock.mockReturnValue({
      candidates: [],
      diagnostics: [],
    });

    const env = { HOME: "/tmp/deneb-home" } as NodeJS.ProcessEnv;

    resolveBundledPluginSources({
      workspaceDir: "/workspace",
      env,
    });
    findBundledPluginSource({
      lookup: { kind: "pluginId", value: "feishu" },
      workspaceDir: "/workspace",
      env,
    });

    expect(discoverDenebPluginsMock).toHaveBeenNthCalledWith(1, {
      workspaceDir: "/workspace",
      env,
    });
    expect(discoverDenebPluginsMock).toHaveBeenNthCalledWith(2, {
      workspaceDir: "/workspace",
      env,
    });
  });

  it("finds bundled source by plugin id", () => {
    discoverDenebPluginsMock.mockReturnValue({
      candidates: [
        {
          origin: "bundled",
          rootDir: "/app/extensions/diffs",
          packageName: "@deneb/diffs",
          packageManifest: { install: { npmSpec: "@deneb/diffs" } },
        },
      ],
      diagnostics: [],
    });
    loadPluginManifestMock.mockReturnValue({ ok: true, manifest: { id: "diffs" } });

    const resolved = findBundledPluginSource({
      lookup: { kind: "pluginId", value: "diffs" },
    });
    const missing = findBundledPluginSource({
      lookup: { kind: "pluginId", value: "not-found" },
    });

    expect(resolved?.pluginId).toBe("diffs");
    expect(resolved?.localPath).toBe("/app/extensions/diffs");
    expect(missing).toBeUndefined();
  });

  it("reuses a pre-resolved bundled map for repeated lookups", () => {
    const bundled = new Map([
      [
        "feishu",
        {
          pluginId: "feishu",
          localPath: "/app/extensions/feishu",
          npmSpec: "@deneb/feishu",
        },
      ],
    ]);

    expect(
      findBundledPluginSourceInMap({
        bundled,
        lookup: { kind: "pluginId", value: "feishu" },
      }),
    ).toEqual({
      pluginId: "feishu",
      localPath: "/app/extensions/feishu",
      npmSpec: "@deneb/feishu",
    });
    expect(
      findBundledPluginSourceInMap({
        bundled,
        lookup: { kind: "npmSpec", value: "@deneb/feishu" },
      })?.pluginId,
    ).toBe("feishu");
  });
});
