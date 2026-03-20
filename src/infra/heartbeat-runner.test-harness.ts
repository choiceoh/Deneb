import { beforeEach } from "vitest";
import { telegramPlugin, setTelegramRuntime } from "../../extensions/telegram/index.js";
import type { ChannelPlugin } from "../channels/plugins/types.plugin.js";
import { setActivePluginRegistry } from "../plugins/runtime.js";
import { createPluginRuntime } from "../plugins/runtime/index.js";
import { createTestRegistry } from "../test-utils/channel-plugins.js";

// Removed extensions (slack, whatsapp) — provide minimal stubs.
// eslint-disable-next-line @typescript-eslint/no-explicit-any
const slackChannelPlugin = {
  id: "slack",
  meta: { id: "slack", label: "Slack" },
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
} as any as ChannelPlugin;
const telegramChannelPlugin = telegramPlugin as unknown as ChannelPlugin;
// eslint-disable-next-line @typescript-eslint/no-explicit-any
const whatsappChannelPlugin = {
  id: "whatsapp",
  meta: { id: "whatsapp", label: "WhatsApp" },
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
} as any as ChannelPlugin;

// eslint-disable-next-line @typescript-eslint/no-explicit-any
function setSlackRuntime(_runtime: any) {}
// eslint-disable-next-line @typescript-eslint/no-explicit-any
function setWhatsAppRuntime(_runtime: any) {}

export function installHeartbeatRunnerTestRuntime(params?: { includeSlack?: boolean }): void {
  beforeEach(() => {
    const runtime = createPluginRuntime();
    setTelegramRuntime(runtime);
    setWhatsAppRuntime(runtime);
    if (params?.includeSlack) {
      setSlackRuntime(runtime);
      setActivePluginRegistry(
        createTestRegistry([
          { pluginId: "slack", plugin: slackChannelPlugin, source: "test" },
          { pluginId: "whatsapp", plugin: whatsappChannelPlugin, source: "test" },
          { pluginId: "telegram", plugin: telegramChannelPlugin, source: "test" },
        ]),
      );
      return;
    }
    setActivePluginRegistry(
      createTestRegistry([
        { pluginId: "whatsapp", plugin: whatsappChannelPlugin, source: "test" },
        { pluginId: "telegram", plugin: telegramChannelPlugin, source: "test" },
      ]),
    );
  });
}
