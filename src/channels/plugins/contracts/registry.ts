import { createTelegramThreadBindingManager } from "../../../../extensions/telegram/runtime-api.js";
import type { DenebConfig } from "../../../config/config.js";
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
    cfg: DenebConfig;
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
    cfg: DenebConfig;
    accountId?: string;
    input: Record<string, unknown>;
    expectedAccountId?: string;
    expectedValidation?: string | null;
    beforeTest?: () => void;
    assertPatchedConfig?: (cfg: DenebConfig) => void;
    assertResolvedAccount?: (account: unknown, cfg: DenebConfig) => void;
  }>;
};

type StatusContractEntry = {
  id: string;
  plugin: Pick<ChannelPlugin, "id" | "config" | "status">;
  cases: Array<{
    name: string;
    cfg: DenebConfig;
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
  cfg?: DenebConfig;
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

// Simple mock function for test contract registry (only used by test files)
function createMockFn() {
  let returnValue: unknown;
  const fn = (..._args: unknown[]) => returnValue;
  fn.mockReset = () => {
    returnValue = undefined;
    return fn;
  };
  fn.mockReturnValue = (val: unknown) => {
    returnValue = val;
    return fn;
  };
  return fn;
}

const telegramDescribeMessageToolMock = createMockFn();

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
    id: "telegram",
    plugin: requireBundledChannelPlugin("telegram"),
    cases: [
      {
        name: "forwards runtime-backed Telegram actions and capabilities",
        cfg: {} as DenebConfig,
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

export const setupContractRegistry: SetupContractEntry[] = [];

export const statusContractRegistry: StatusContractEntry[] = [];

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

const directoryPresenceOnlyIds = new Set(["whatsapp"]);

export const directoryContractRegistry: DirectoryContractEntry[] = surfaceContractRegistry
  .filter((entry) => entry.surfaces.includes("directory"))
  .map((entry) => ({
    id: entry.id,
    plugin: entry.plugin,
    coverage: directoryPresenceOnlyIds.has(entry.id) ? "presence" : "lookups",
  }));

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
