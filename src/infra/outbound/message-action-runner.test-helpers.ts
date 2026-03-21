import { telegramPlugin, setTelegramRuntime } from "../../../extensions/telegram/index.js";
import type { ChannelPlugin } from "../../channels/plugins/types.plugin.js";
import type { OpenClawConfig } from "../../config/config.js";
import { setActivePluginRegistry } from "../../plugins/runtime.js";
import { createPluginRuntime } from "../../plugins/runtime/index.js";
import { createTestRegistry } from "../../test-utils/channel-plugins.js";

// Slack extension removed — provide minimal stub for test registry.
const slackPlugin = {
  id: "slack",
  meta: { id: "slack", label: "Slack" },
} as unknown as ChannelPlugin;
function setSlackRuntime(_runtime: unknown) {}

export const slackConfig = {
  channels: {
    slack: {
      botToken: "xoxb-test",
      appToken: "xapp-test",
    },
  },
} as OpenClawConfig;

export const telegramConfig = {
  channels: {
    telegram: {
      botToken: "telegram-test",
    },
  },
} as OpenClawConfig;

export function installMessageActionRunnerTestRegistry() {
  const runtime = createPluginRuntime();
  setSlackRuntime(runtime);
  setTelegramRuntime(runtime);
  setActivePluginRegistry(
    createTestRegistry([
      {
        pluginId: "slack",
        source: "test",
        plugin: slackPlugin,
      },
      {
        pluginId: "telegram",
        source: "test",
        plugin: telegramPlugin,
      },
    ]),
  );
}

export function resetMessageActionRunnerTestRegistry() {
  setActivePluginRegistry(createTestRegistry([]));
}
