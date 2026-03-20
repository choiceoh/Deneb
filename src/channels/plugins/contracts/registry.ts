
import { createTelegramThreadBindingManager } from "../../../../extensions/telegram/runtime-api.js";
import type { OpenClawConfig } from "../../../config/config.js";
import {
  getSessionBindingService,
  type SessionBindingCapabilities,
  type SessionBindingRecord,
} from "../../../infra/outbound/session-binding-service.js";

import {
  bundledChannelPlugins,
  bundledChannelRuntimeSetters,
  requireBundledChannelPlugin,
} from "../bundled.js";
import type { ChannelPlugin } from "../types.js";

type PluginContractEntry = {
  id: string;
  plugin: Pick<ChannelPlugin, "id" | "meta" | "capabilities" | "config">;
};

type ActionsContractEntry = {
  id: string;
  plugin: Pick<ChannelPlugin, "id" | "actions">;
  unsupportedAction?: string;
  cases: Array<{
    name: string;
    cfg: OpenClawConfig;
    expectedActions: string[];
    expectedCapabilities?: string[];
    beforeTest?: () => void;
  }>;
};

type SetupContractEntry = {
  id: string;
  plugin: Pick<ChannelPlugin, "id" | "config" | "setup">;
  cases: Array<{
    name: string;
    cfg: OpenClawConfig;
    accountId?: string;
    input: Record<string, unknown>;
    expectedAccountId?: string;
    expectedValidation?: string | null;
    beforeTest?: () => void;
    assertPatchedConfig?: (cfg: OpenClawConfig) => void;
    assertResolvedAccount?: (account: unknown, cfg: OpenClawConfig) => void;
  }>;
};

type StatusContractEntry = {
  id: string;
  plugin: Pick<ChannelPlugin, "id" | "config" | "status">;
  cases: Array<{
    name: string;
    cfg: OpenClawConfig;
    accountId?: string;
    runtime?: Record<string, unknown>;
    probe?: unknown;
    beforeTest?: () => void;
    assertSnapshot?: (snapshot: Record<string, unknown>) => void;
    assertSummary?: (summary: Record<string, unknown>) => void;
  }>;
};

export const channelPluginSurfaceKeys = [
  "actions",
  "setup",
  "status",
  "outbound",
  "messaging",
  "threading",
  "directory",
  "gateway",
] as const;

export type ChannelPluginSurface =
  | "actions"
  | "setup"
  | "status"
  | "outbound"
  | "messaging"
  | "threading"
  | "directory"
  | "gateway";

type SurfaceContractEntry = {
  id: string;
  plugin: Pick<
    ChannelPlugin,
    | "id"
    | "actions"
    | "setup"
    | "status"
    | "outbound"
    | "messaging"
    | "threading"
    | "directory"
    | "gateway"
  >;
  surfaces: readonly ChannelPluginSurface[];
};

type ThreadingContractEntry = {
  id: string;
  plugin: Pick<ChannelPlugin, "id" | "threading">;
};

type DirectoryContractEntry = {
  id: string;
  plugin: Pick<ChannelPlugin, "id" | "directory">;
  coverage: "lookups" | "presence";
  cfg?: OpenClawConfig;
  accountId?: string;
};

type SessionBindingContractEntry = {
  id: string;
  expectedCapabilities: SessionBindingCapabilities;
  getCapabilities: () => SessionBindingCapabilities;
  bindAndResolve: () => Promise<SessionBindingRecord>;
  unbindAndVerify: (binding: SessionBindingRecord) => Promise<void>;
  cleanup: () => Promise<void> | void;
};

function expectResolvedSessionBinding(params: {
  channel: string;
  accountId: string;
  conversationId: string;
  targetSessionKey: string;
}) {
  expect(
    getSessionBindingService().resolveByConversation({
      channel: params.channel,
      accountId: params.accountId,
      conversationId: params.conversationId,
    }),
  )?.toMatchObject({
    targetSessionKey: params.targetSessionKey,
  });
}

async function unbindAndExpectClearedSessionBinding(binding: SessionBindingRecord) {
  const service = getSessionBindingService();
  const removed = await service.unbind({
    bindingId: binding.bindingId,
    reason: "contract-test",
  });
  expect(removed.map((entry) => entry.bindingId)).toContain(binding.bindingId);
  expect(service.resolveByConversation(binding.conversation)).toBeNull();
}

function expectClearedSessionBinding(params: {
  channel: string;
  accountId: string;
  conversationId: string;
}) {
  expect(
    getSessionBindingService().resolveByConversation({
      channel: params.channel,
      accountId: params.accountId,
      conversationId: params.conversationId,
    }),
  ).toBeNull();
}

const telegramDescribeMessageToolMock: any = null;

bundledChannelRuntimeSetters.setTelegramRuntime({
  channel: {
    telegram: {
      messageActions: {
        describeMessageTool: telegramDescribeMessageToolMock,
      },
    },
  },
} as never);



export const pluginContractRegistry: PluginContractEntry[] = bundledChannelPlugins.map(
  (plugin) => ({
    id: plugin.id,
    plugin,
  }),
);

export const actionContractRegistry: ActionsContractEntry[] = [
  {
    id: "mattermost",
    plugin: requireBundledChannelPlugin("mattermost"),
    unsupportedAction: "poll",
    cases: [
      {
        name: "configured account exposes send and react",
        cfg: {
          channels: {
            mattermost: {
              enabled: true,
              botToken: "test-token",
              baseUrl: "https://chat.example.com",
            },
          },
        } as OpenClawConfig,
        expectedActions: ["send", "react"],
        expectedCapabilities: ["buttons"],
      },
      {
        name: "reactions can be disabled while send stays available",
        cfg: {
          channels: {
            mattermost: {
              enabled: true,
              botToken: "test-token",
              baseUrl: "https://chat.example.com",
              actions: { reactions: false },
            },
          },
        } as OpenClawConfig,
        expectedActions: ["send"],
        expectedCapabilities: ["buttons"],
      },
      {
        name: "missing bot credentials disables the actions surface",
        cfg: {
          channels: {
            mattermost: {
              enabled: true,
            },
          },
        } as OpenClawConfig,
        expectedActions: [],
        expectedCapabilities: [],
      },
    ],
  },
  {
    id: "telegram",
    plugin: requireBundledChannelPlugin("telegram"),
    cases: [
      {
        name: "forwards runtime-backed Telegram actions and capabilities",
        cfg: {} as OpenClawConfig,
        expectedActions: ["send", "poll", "react"],
        expectedCapabilities: ["interactive", "buttons"],
        beforeTest: () => {
          telegramDescribeMessageToolMock.mockReset();
          telegramDescribeMessageToolMock.mockReturnValue({
            actions: ["send", "poll", "react"],
            capabilities: ["interactive", "buttons"],
          });
        },
      },
    ],
  },
];

export const setupContractRegistry: SetupContractEntry[] = [
  {
    id: "mattermost",
    plugin: requireBundledChannelPlugin("mattermost"),
    cases: [
      {
        name: "default account stores token and normalized base URL",
        cfg: {} as OpenClawConfig,
        input: {
          botToken: "test-token",
          httpUrl: "https://chat.example.com/",
        },
        expectedAccountId: "default",
        assertPatchedConfig: (cfg) => {
          expect(cfg.channels?.mattermost?.enabled).toBe(true);
          expect(cfg.channels?.mattermost?.botToken).toBe("test-token");
          expect(cfg.channels?.mattermost?.baseUrl).toBe("https://chat.example.com");
        },
      },
      {
        name: "missing credentials are rejected",
        cfg: {} as OpenClawConfig,
        input: {
          httpUrl: "",
        },
        expectedAccountId: "default",
        expectedValidation: "Mattermost requires --bot-token and --http-url (or --use-env).",
      },
    ],
];

export const statusContractRegistry: StatusContractEntry[] = [
  {
    id: "mattermost",
    plugin: requireBundledChannelPlugin("mattermost"),
    cases: [
      {
        name: "configured account preserves connectivity details in the snapshot",
        cfg: {
          channels: {
            mattermost: {
              enabled: true,
              botToken: "test-token",
              baseUrl: "https://chat.example.com",
            },
          },
        } as OpenClawConfig,
        runtime: {
          accountId: "default",
          connected: true,
          lastConnectedAt: 1234,
        },
        probe: { ok: true },
        assertSnapshot: (snapshot) => {
          expect(snapshot.accountId).toBe("default");
          expect(snapshot.enabled).toBe(true);
          expect(snapshot.configured).toBe(true);
          expect(snapshot.connected).toBe(true);
          expect(snapshot.baseUrl).toBe("https://chat.example.com");
        },
      },
    ],
];

export const surfaceContractRegistry: SurfaceContractEntry[] = bundledChannelPlugins.map(
  (plugin) => ({
    id: plugin.id,
    plugin,
    surfaces: channelPluginSurfaceKeys.filter((surface) => Boolean(plugin[surface])),
  }),
);

export const threadingContractRegistry: ThreadingContractEntry[] = surfaceContractRegistry
  .filter((entry) => entry.surfaces.includes("threading"))
  .map((entry) => ({
    id: entry.id,
    plugin: entry.plugin,
  }));

const directoryPresenceOnlyIds = new Set(["whatsapp", "zalouser"]);

export const directoryContractRegistry: DirectoryContractEntry[] = surfaceContractRegistry
  .filter((entry) => entry.surfaces.includes("directory"))
  .map((entry) => ({
    id: entry.id,
    plugin: entry.plugin,
    coverage: directoryPresenceOnlyIds.has(entry.id) ? "presence" : "lookups",
  }));

const baseSessionBindingCfg = {
  session: { mainKey: "main", scope: "per-sender" },
} satisfies OpenClawConfig;

export const sessionBindingContractRegistry: SessionBindingContractEntry[] = [
  {
    id: "telegram",
    expectedCapabilities: {
      adapterAvailable: true,
      bindSupported: true,
      unbindSupported: true,
      placements: ["current"],
    },
    getCapabilities: () => {
      createTelegramThreadBindingManager({
        accountId: "default",
        persist: false,
        enableSweeper: false,
      });
      return getSessionBindingService().getCapabilities({
        channel: "telegram",
        accountId: "default",
      });
    },
    bindAndResolve: async () => {
      createTelegramThreadBindingManager({
        accountId: "default",
        persist: false,
        enableSweeper: false,
      });
      const service = getSessionBindingService();
      const binding = await service.bind({
        targetSessionKey: "agent:main:subagent:child-1",
        targetKind: "subagent",
        conversation: {
          channel: "telegram",
          accountId: "default",
          conversationId: "-100200300:topic:77",
        },
        placement: "current",
        metadata: {
          boundBy: "user-1",
        },
      });
      expectResolvedSessionBinding({
        channel: "telegram",
        accountId: "default",
        conversationId: "-100200300:topic:77",
        targetSessionKey: "agent:main:subagent:child-1",
      });
      return binding;
    },
    unbindAndVerify: unbindAndExpectClearedSessionBinding,
    cleanup: async () => {
      const manager = createTelegramThreadBindingManager({
        accountId: "default",
        persist: false,
        enableSweeper: false,
      });
      manager.stop();
      expectClearedSessionBinding({
        channel: "telegram",
        accountId: "default",
        conversationId: "-100200300:topic:77",
      });
    },
  },
];
