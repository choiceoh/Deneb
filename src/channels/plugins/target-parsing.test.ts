import { beforeEach, describe, expect, it } from "vitest";
import { setActivePluginRegistry } from "../../plugins/runtime.js";
import { createTestRegistry } from "../../test-utils/channel-plugins.js";
import { parseExplicitTargetForChannel } from "./target-parsing.js";

describe("parseExplicitTargetForChannel", () => {
  beforeEach(() => {
    setActivePluginRegistry(createTestRegistry([]));
  });

  it("parses bundled Telegram targets without an active Telegram registry entry", () => {
    expect(parseExplicitTargetForChannel("telegram", "telegram:group:-100123:topic:77")).toEqual({
      to: "-100123",
      threadId: 77,
      chatType: "group",
    });
    expect(parseExplicitTargetForChannel("telegram", "-100123")).toEqual({
      to: "-100123",
      chatType: "group",
    });
  });

  it("parses registered non-bundled channel targets via the active plugin contract", () => {
    setActivePluginRegistry(
      createTestRegistry([
        {
          pluginId: "mattermost",
          source: "test",
          plugin: {
            id: "mattermost",
            meta: {
              id: "mattermost",
              label: "Microsoft Teams",
              selectionLabel: "Microsoft Teams",
              docsPath: "/channels/mattermost",
              blurb: "test stub",
            },
            capabilities: { chatTypes: ["direct"] },
            config: {
              listAccountIds: () => [],
              resolveAccount: () => ({}),
            },
            messaging: {
              parseExplicitTarget: ({ raw }: { raw: string }) => ({
                to: raw.trim().toUpperCase(),
                chatType: "direct" as const,
              }),
            },
          },
        },
      ]),
    );

    expect(parseExplicitTargetForChannel("mattermost", "team-room")).toEqual({
      to: "TEAM-ROOM",
      chatType: "direct",
    });
  });
});
