import { afterEach, beforeEach, describe, expect, it } from "vitest";
import type { ChannelPlugin } from "../channels/plugins/types.js";
import { setActivePluginRegistry } from "../plugins/runtime.js";
import {
  createMattermostTestPluginBase,
  createTestRegistry,
} from "../test-utils/channel-plugins.js";
import { resolveGatewayMessageChannel } from "./message-channel.js";

const emptyRegistry = createTestRegistry([]);
const mattermostPlugin: ChannelPlugin = {
  ...createMattermostTestPluginBase(),
};

describe("message-channel", () => {
  beforeEach(() => {
    setActivePluginRegistry(emptyRegistry);
  });

  afterEach(() => {
    setActivePluginRegistry(emptyRegistry);
  });

  it("normalizes gateway message channels and rejects unknown values", () => {
    expect(resolveGatewayMessageChannel("telegram")).toBe("telegram");
    expect(resolveGatewayMessageChannel(" Telegram ")).toBe("telegram");
    expect(resolveGatewayMessageChannel("web")).toBeUndefined();
    expect(resolveGatewayMessageChannel("nope")).toBeUndefined();
  });

  it("normalizes plugin aliases when registered", () => {
    setActivePluginRegistry(
      createTestRegistry([{ pluginId: "mattermost", plugin: mattermostPlugin, source: "test" }]),
    );
    expect(resolveGatewayMessageChannel("mm")).toBe("mattermost");
  });
});
