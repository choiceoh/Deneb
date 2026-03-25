import { afterEach, beforeEach, describe, expect, it } from "vitest";
import type { ChannelPlugin } from "../channels/plugins/types.js";
import { setActivePluginRegistry } from "../plugins/runtime.js";
import { createTestRegistry } from "../test-utils/channel-plugins.js";
import { resolveChannelCapabilities } from "./channel-capabilities.js";
import type { DenebConfig } from "./config.js";

describe("resolveChannelCapabilities", () => {
  beforeEach(() => {
    setActivePluginRegistry(baseRegistry);
  });

  afterEach(() => {
    setActivePluginRegistry(baseRegistry);
  });

  it("returns undefined for missing inputs", () => {
    expect(resolveChannelCapabilities({})).toBeUndefined();
    expect(resolveChannelCapabilities({ cfg: {} })).toBeUndefined();
    expect(resolveChannelCapabilities({ cfg: {}, channel: "" })).toBeUndefined();
  });

  it("normalizes and prefers per-account capabilities", () => {
    const cfg = {
      channels: {
        telegram: {
          capabilities: [" inlineButtons ", ""],
          accounts: {
            default: {
              capabilities: [" perAccount ", "  "],
            },
          },
        },
      },
    } satisfies Partial<DenebConfig>;

    expect(
      resolveChannelCapabilities({
        cfg,
        channel: "telegram",
        accountId: "default",
      }),
    ).toEqual(["perAccount"]);
  });

  it("falls back to provider capabilities when account capabilities are missing", () => {
    const cfg = {
      channels: {
        telegram: {
          capabilities: ["inlineButtons"],
          accounts: {
            default: {},
          },
        },
      },
    } satisfies Partial<DenebConfig>;

    expect(
      resolveChannelCapabilities({
        cfg,
        channel: "telegram",
        accountId: "default",
      }),
    ).toEqual(["inlineButtons"]);
  });

  it("matches account keys case-insensitively", () => {
    const cfg = {
      channels: {
        telegram: {
          accounts: {
            Family: { capabilities: ["threads"] },
          },
        },
      },
    } satisfies Partial<DenebConfig>;

    expect(
      resolveChannelCapabilities({
        cfg,
        channel: "telegram",
        accountId: "family",
      }),
    ).toEqual(["threads"]);
  });

  it("supports plugin channel capabilities", () => {
    setActivePluginRegistry(
      createTestRegistry([
        {
          pluginId: "mattermost",
          source: "test",
          plugin: createMattermostPlugin(),
        },
      ]),
    );
    const cfg = {
      channels: { mattermost: { capabilities: [" polls ", ""] } },
    } satisfies Partial<DenebConfig>;

    expect(
      resolveChannelCapabilities({
        cfg,
        channel: "mattermost",
      }),
    ).toEqual(["polls"]);
  });

  it("handles object-format capabilities gracefully (e.g., { inlineButtons: 'dm' })", () => {
    const cfg = {
      channels: {
        telegram: {
          // Object format - used for granular control like inlineButtons scope.
          // Channel-specific handlers (resolveTelegramInlineButtonsScope) process these.
          capabilities: { inlineButtons: "dm" },
        },
      },
    } as unknown as Partial<DenebConfig>;

    // Should return undefined (not crash), allowing channel-specific handlers to process it.
    expect(
      resolveChannelCapabilities({
        cfg,
        channel: "telegram",
      }),
    ).toBeUndefined();
  });

  it("handles object-format capabilities gracefully on registered channel", () => {
    const cfg = {
      channels: {
        telegram: {
          capabilities: { interactiveReplies: true },
        },
      },
    } as unknown as Partial<DenebConfig>;

    expect(
      resolveChannelCapabilities({
        cfg,
        channel: "telegram",
      }),
    ).toBeUndefined();
  });
});

const createStubPlugin = (id: string): ChannelPlugin => ({
  id,
  meta: {
    id,
    label: id,
    selectionLabel: id,
    docsPath: `/channels/${id}`,
    blurb: "test stub.",
  },
  capabilities: { chatTypes: ["direct"] },
  config: {
    listAccountIds: () => [],
    resolveAccount: () => ({}),
  },
});

const baseRegistry = createTestRegistry([
  { pluginId: "telegram", source: "test", plugin: createStubPlugin("telegram") },
]);

const createMattermostPlugin = (): ChannelPlugin => createStubPlugin("mattermost");
