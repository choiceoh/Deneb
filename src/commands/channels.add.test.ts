import { beforeAll, beforeEach, describe, expect, it, vi } from "vitest";
import type { ChannelPluginCatalogEntry } from "../channels/plugins/catalog.js";
import type { ChannelPlugin } from "../channels/plugins/types.js";
import { setActivePluginRegistry } from "../plugins/runtime.js";
import { createChannelTestPluginBase, createTestRegistry } from "../test-utils/channel-plugins.js";
import {
  ensureChannelSetupPluginInstalled,
  loadChannelSetupPluginRegistrySnapshotForChannel,
} from "./channel-setup/plugin-install.js";
import { setDefaultChannelPluginRegistryForTests } from "./channel-test-helpers.js";
import { configMocks, offsetMocks } from "./channels.mock-harness.js";
import { baseConfigSnapshot, createTestRuntime } from "./test-runtime-config-helpers.js";

const catalogMocks = vi.hoisted(() => ({
  listChannelPluginCatalogEntries: vi.fn((): ChannelPluginCatalogEntry[] => []),
}));

const manifestRegistryMocks = vi.hoisted(() => ({
  loadPluginManifestRegistry: vi.fn(() => ({ plugins: [], diagnostics: [] })),
}));

vi.mock("../channels/plugins/catalog.js", async (importOriginal) => {
  const actual = await importOriginal<typeof import("../channels/plugins/catalog.js")>();
  return {
    ...actual,
    listChannelPluginCatalogEntries: catalogMocks.listChannelPluginCatalogEntries,
  };
});

vi.mock("../plugins/manifest-registry.js", async (importOriginal) => {
  const actual = await importOriginal<typeof import("../plugins/manifest-registry.js")>();
  return {
    ...actual,
    loadPluginManifestRegistry: manifestRegistryMocks.loadPluginManifestRegistry,
  };
});

vi.mock("./channel-setup/plugin-install.js", async (importOriginal) => {
  const actual = await importOriginal<typeof import("./channel-setup/plugin-install.js")>();
  return {
    ...actual,
    ensureChannelSetupPluginInstalled: vi.fn(async ({ cfg }) => ({ cfg, installed: true })),
    loadChannelSetupPluginRegistrySnapshotForChannel: vi.fn(() => createTestRegistry()),
  };
});

const runtime = createTestRuntime();
let channelsAddCommand: typeof import("./channels.js").channelsAddCommand;

describe("channelsAddCommand", () => {
  beforeAll(async () => {
    ({ channelsAddCommand } = await import("./channels.js"));
  });

  beforeEach(async () => {
    configMocks.readConfigFileSnapshot.mockClear();
    configMocks.writeConfigFile.mockClear();
    offsetMocks.deleteTelegramUpdateOffset.mockClear();
    runtime.log.mockClear();
    runtime.error.mockClear();
    runtime.exit.mockClear();
    catalogMocks.listChannelPluginCatalogEntries.mockClear();
    catalogMocks.listChannelPluginCatalogEntries.mockReturnValue([]);
    manifestRegistryMocks.loadPluginManifestRegistry.mockClear();
    manifestRegistryMocks.loadPluginManifestRegistry.mockReturnValue({
      plugins: [],
      diagnostics: [],
    });
    vi.mocked(ensureChannelSetupPluginInstalled).mockClear();
    vi.mocked(ensureChannelSetupPluginInstalled).mockImplementation(async ({ cfg }) => ({
      cfg,
      installed: true,
    }));
    vi.mocked(loadChannelSetupPluginRegistrySnapshotForChannel).mockClear();
    vi.mocked(loadChannelSetupPluginRegistrySnapshotForChannel).mockReturnValue(
      createTestRegistry(),
    );
    setDefaultChannelPluginRegistryForTests();
  });

  it("clears telegram update offsets when the token changes", async () => {
    configMocks.readConfigFileSnapshot.mockResolvedValue({
      ...baseConfigSnapshot,
      config: {
        channels: {
          telegram: { botToken: "old-token", enabled: true },
        },
      },
    });

    await channelsAddCommand(
      { channel: "telegram", account: "default", token: "new-token" },
      runtime,
      { hasFlags: true },
    );

    expect(offsetMocks.deleteTelegramUpdateOffset).toHaveBeenCalledTimes(1);
    expect(offsetMocks.deleteTelegramUpdateOffset).toHaveBeenCalledWith({ accountId: "default" });
  });

  it("does not clear telegram update offsets when the token is unchanged", async () => {
    configMocks.readConfigFileSnapshot.mockResolvedValue({
      ...baseConfigSnapshot,
      config: {
        channels: {
          telegram: { botToken: "same-token", enabled: true },
        },
      },
    });

    await channelsAddCommand(
      { channel: "telegram", account: "default", token: "same-token" },
      runtime,
      { hasFlags: true },
    );

    expect(offsetMocks.deleteTelegramUpdateOffset).not.toHaveBeenCalled();
  });

  it("falls back to a scoped snapshot after installing an external channel plugin", async () => {
    configMocks.readConfigFileSnapshot.mockResolvedValue({ ...baseConfigSnapshot });
    setActivePluginRegistry(createTestRegistry());
    const catalogEntry: ChannelPluginCatalogEntry = {
      id: "mattermost",
      pluginId: "@deneb/mattermost-plugin",
      meta: {
        id: "mattermost",
        label: "Mattermost",
        selectionLabel: "Mattermost",
        docsPath: "/channels/mattermost",
        blurb: "teams channel",
      },
      install: {
        npmSpec: "@deneb/mattermost",
      },
    };
    catalogMocks.listChannelPluginCatalogEntries.mockReturnValue([catalogEntry]);
    const scopedMattermostPlugin = {
      ...createChannelTestPluginBase({
        id: "mattermost",
        label: "Mattermost",
        docsPath: "/channels/mattermost",
      }),
      setup: {
        applyAccountConfig: vi.fn(({ cfg, input }) => ({
          ...cfg,
          channels: {
            ...cfg.channels,
            mattermost: {
              enabled: true,
              tenantId: input.token,
            },
          },
        })),
      },
    };
    vi.mocked(loadChannelSetupPluginRegistrySnapshotForChannel).mockReturnValue(
      createTestRegistry([
        { pluginId: "mattermost", plugin: scopedMattermostPlugin, source: "test" },
      ]),
    );

    await channelsAddCommand(
      {
        channel: "mattermost",
        account: "default",
        token: "tenant-scoped",
      },
      runtime,
      { hasFlags: true },
    );

    expect(ensureChannelSetupPluginInstalled).toHaveBeenCalledWith(
      expect.objectContaining({ entry: catalogEntry }),
    );
    expect(loadChannelSetupPluginRegistrySnapshotForChannel).toHaveBeenCalledWith(
      expect.objectContaining({
        channel: "mattermost",
        pluginId: "@deneb/mattermost-plugin",
      }),
    );
    expect(configMocks.writeConfigFile).toHaveBeenCalledWith(
      expect.objectContaining({
        channels: {
          mattermost: {
            enabled: true,
            tenantId: "tenant-scoped",
          },
        },
      }),
    );
    expect(runtime.error).not.toHaveBeenCalled();
    expect(runtime.exit).not.toHaveBeenCalled();
  });

  it("uses the installed external channel snapshot without reinstalling", async () => {
    configMocks.readConfigFileSnapshot.mockResolvedValue({ ...baseConfigSnapshot });
    setActivePluginRegistry(createTestRegistry());
    const catalogEntry: ChannelPluginCatalogEntry = {
      id: "mattermost",
      pluginId: "@deneb/mattermost-plugin",
      meta: {
        id: "mattermost",
        label: "Mattermost",
        selectionLabel: "Mattermost",
        docsPath: "/channels/mattermost",
        blurb: "teams channel",
      },
      install: {
        npmSpec: "@deneb/mattermost",
      },
    };
    catalogMocks.listChannelPluginCatalogEntries.mockReturnValue([catalogEntry]);
    manifestRegistryMocks.loadPluginManifestRegistry.mockReturnValue({
      plugins: [
        {
          id: "@deneb/mattermost-plugin",
          channels: ["mattermost"],
        } as never,
      ],
      diagnostics: [],
    });
    const scopedMattermostPlugin = {
      ...createChannelTestPluginBase({
        id: "mattermost",
        label: "Mattermost",
        docsPath: "/channels/mattermost",
      }),
      setup: {
        applyAccountConfig: vi.fn(({ cfg, input }) => ({
          ...cfg,
          channels: {
            ...cfg.channels,
            mattermost: {
              enabled: true,
              tenantId: input.token,
            },
          },
        })),
      },
    };
    vi.mocked(loadChannelSetupPluginRegistrySnapshotForChannel).mockReturnValue(
      createTestRegistry([
        { pluginId: "mattermost", plugin: scopedMattermostPlugin, source: "test" },
      ]),
    );

    await channelsAddCommand(
      {
        channel: "mattermost",
        account: "default",
        token: "tenant-installed",
      },
      runtime,
      { hasFlags: true },
    );

    expect(ensureChannelSetupPluginInstalled).not.toHaveBeenCalled();
    expect(loadChannelSetupPluginRegistrySnapshotForChannel).toHaveBeenCalledWith(
      expect.objectContaining({
        channel: "mattermost",
        pluginId: "@deneb/mattermost-plugin",
      }),
    );
    expect(configMocks.writeConfigFile).toHaveBeenCalledWith(
      expect.objectContaining({
        channels: {
          mattermost: {
            enabled: true,
            tenantId: "tenant-installed",
          },
        },
      }),
    );
  });

  it("uses the installed plugin id when channel and plugin ids differ", async () => {
    configMocks.readConfigFileSnapshot.mockResolvedValue({ ...baseConfigSnapshot });
    setActivePluginRegistry(createTestRegistry());
    const catalogEntry: ChannelPluginCatalogEntry = {
      id: "mattermost",
      pluginId: "@deneb/mattermost-plugin",
      meta: {
        id: "mattermost",
        label: "Mattermost",
        selectionLabel: "Mattermost",
        docsPath: "/channels/mattermost",
        blurb: "teams channel",
      },
      install: {
        npmSpec: "@deneb/mattermost",
      },
    };
    catalogMocks.listChannelPluginCatalogEntries.mockReturnValue([catalogEntry]);
    vi.mocked(ensureChannelSetupPluginInstalled).mockImplementation(async ({ cfg }) => ({
      cfg,
      installed: true,
      pluginId: "@vendor/teams-runtime",
    }));
    vi.mocked(loadChannelSetupPluginRegistrySnapshotForChannel).mockReturnValue(
      createTestRegistry([
        {
          pluginId: "@vendor/teams-runtime",
          plugin: {
            ...createChannelTestPluginBase({
              id: "mattermost",
              label: "Mattermost",
              docsPath: "/channels/mattermost",
            }),
            setup: {
              applyAccountConfig: vi.fn(({ cfg, input }) => ({
                ...cfg,
                channels: {
                  ...cfg.channels,
                  mattermost: {
                    enabled: true,
                    tenantId: input.token,
                  },
                },
              })),
            },
          },
          source: "test",
        },
      ]),
    );

    await channelsAddCommand(
      {
        channel: "mattermost",
        account: "default",
        token: "tenant-scoped",
      },
      runtime,
      { hasFlags: true },
    );

    expect(loadChannelSetupPluginRegistrySnapshotForChannel).toHaveBeenCalledWith(
      expect.objectContaining({
        channel: "mattermost",
        pluginId: "@vendor/teams-runtime",
      }),
    );
    expect(runtime.error).not.toHaveBeenCalled();
    expect(runtime.exit).not.toHaveBeenCalled();
  });

  it("runs post-setup hooks after writing config", async () => {
    const afterAccountConfigWritten = vi.fn().mockResolvedValue(undefined);
    const plugin: ChannelPlugin = {
      ...createChannelTestPluginBase({
        id: "signal",
        label: "Signal",
      }),
      setup: {
        applyAccountConfig: ({ cfg, accountId, input }) => ({
          ...cfg,
          channels: {
            ...cfg.channels,
            signal: {
              enabled: true,
              accounts: {
                [accountId]: {
                  signalNumber: input.signalNumber,
                },
              },
            },
          },
        }),
        afterAccountConfigWritten,
      },
    } as ChannelPlugin;
    setActivePluginRegistry(createTestRegistry([{ pluginId: "signal", plugin, source: "test" }]));
    configMocks.readConfigFileSnapshot.mockResolvedValue({ ...baseConfigSnapshot });

    await channelsAddCommand(
      { channel: "signal", account: "ops", signalNumber: "+15550001" },
      runtime,
      { hasFlags: true },
    );

    expect(configMocks.writeConfigFile).toHaveBeenCalledTimes(1);
    expect(afterAccountConfigWritten).toHaveBeenCalledTimes(1);
    expect(configMocks.writeConfigFile.mock.invocationCallOrder[0]).toBeLessThan(
      afterAccountConfigWritten.mock.invocationCallOrder[0] ?? Number.POSITIVE_INFINITY,
    );
    expect(afterAccountConfigWritten).toHaveBeenCalledWith({
      previousCfg: baseConfigSnapshot.config,
      cfg: expect.objectContaining({
        channels: {
          signal: {
            enabled: true,
            accounts: {
              ops: {
                signalNumber: "+15550001",
              },
            },
          },
        },
      }),
      accountId: "ops",
      input: expect.objectContaining({
        signalNumber: "+15550001",
      }),
      runtime,
    });
  });

  it("keeps the saved config when a post-setup hook fails", async () => {
    const afterAccountConfigWritten = vi.fn().mockRejectedValue(new Error("hook failed"));
    const plugin: ChannelPlugin = {
      ...createChannelTestPluginBase({
        id: "signal",
        label: "Signal",
      }),
      setup: {
        applyAccountConfig: ({ cfg, accountId, input }) => ({
          ...cfg,
          channels: {
            ...cfg.channels,
            signal: {
              enabled: true,
              accounts: {
                [accountId]: {
                  signalNumber: input.signalNumber,
                },
              },
            },
          },
        }),
        afterAccountConfigWritten,
      },
    } as ChannelPlugin;
    setActivePluginRegistry(createTestRegistry([{ pluginId: "signal", plugin, source: "test" }]));
    configMocks.readConfigFileSnapshot.mockResolvedValue({ ...baseConfigSnapshot });

    await channelsAddCommand(
      { channel: "signal", account: "ops", signalNumber: "+15550001" },
      runtime,
      { hasFlags: true },
    );

    expect(configMocks.writeConfigFile).toHaveBeenCalledTimes(1);
    expect(runtime.exit).not.toHaveBeenCalled();
    expect(runtime.error).toHaveBeenCalledWith(
      'Channel signal post-setup warning for "ops": hook failed',
    );
  });
});
