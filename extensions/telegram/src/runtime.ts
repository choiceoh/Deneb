import type { PluginRuntime } from "deneb/plugin-sdk/core";
import { createPluginRuntimeStore } from "deneb/plugin-sdk/runtime-store";

const { setRuntime: setTelegramRuntime, getRuntime: getTelegramRuntime } =
  createPluginRuntimeStore<PluginRuntime>("Telegram runtime not initialized");
export { getTelegramRuntime, setTelegramRuntime };
