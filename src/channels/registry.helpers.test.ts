import { describe, expect, it } from "vitest";
import {
  formatChannelSelectionLine,
  getChatChannelMeta,
  listChatChannels,
  normalizeChannelId,
  normalizeChatChannelId,
} from "./registry.js";

describe("channel registry helpers", () => {
  it("normalizes registered channel IDs + trims whitespace", () => {
    expect(normalizeChatChannelId(" telegram ")).toBe("telegram");
    expect(normalizeChatChannelId("Telegram")).toBe("telegram");
    expect(normalizeChatChannelId("nope")).toBeNull();
    expect(normalizeChatChannelId("web")).toBeNull();
  });

  it("normalizeChannelId delegates the same as normalizeChatChannelId", () => {
    expect(normalizeChannelId("telegram")).toBe("telegram");
    expect(normalizeChannelId("nope")).toBeNull();
  });

  it("keeps Telegram first in the default order", () => {
    const channels = listChatChannels();
    expect(channels[0]?.id).toBe("telegram");
  });

  it("does not include MS Teams by default", () => {
    const channels = listChatChannels();
    expect(channels.some((channel) => channel.id === "msteams")).toBe(false);
  });

  it("formats selection lines with docs labels", () => {
    const meta = getChatChannelMeta("telegram");
    if (!meta) {
      throw new Error("Missing channel metadata.");
    }
    const line = formatChannelSelectionLine(meta, (path, label) =>
      [label, path].filter(Boolean).join(":"),
    );
    // Telegram registration has selectionDocsPrefix: "" — should not add "Docs:" prefix
    expect(line).not.toContain("Docs:");
    expect(line).toContain("/channels/telegram");
  });
});
