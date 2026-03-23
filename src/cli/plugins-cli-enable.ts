import type { DenebConfig } from "../config/config.js";
import { loadConfig, writeConfigFile } from "../config/config.js";
import { enablePluginInConfig } from "../plugins/enable.js";
import { defaultRuntime } from "../runtime.js";
import { theme } from "../terminal/theme.js";
import { applySlotSelectionForPlugin, logSlotWarnings } from "./plugins-cli-shared.js";
import { setPluginEnabledInConfig } from "./plugins-config.js";

export async function runPluginEnableCommand(id: string) {
  const cfg = loadConfig();
  const enableResult = enablePluginInConfig(cfg, id);
  let next: DenebConfig = enableResult.config;
  const slotResult = applySlotSelectionForPlugin(next, id);
  next = slotResult.config;
  await writeConfigFile(next);
  logSlotWarnings(slotResult.warnings);
  if (enableResult.enabled) {
    defaultRuntime.log(`Enabled plugin "${id}". Restart the gateway to apply.`);
    return;
  }
  defaultRuntime.log(
    theme.warn(`Plugin "${id}" could not be enabled (${enableResult.reason ?? "unknown reason"}).`),
  );
}

export async function runPluginDisableCommand(id: string) {
  const cfg = loadConfig();
  const next = setPluginEnabledInConfig(cfg, id, false);
  await writeConfigFile(next);
  defaultRuntime.log(`Disabled plugin "${id}". Restart the gateway to apply.`);
}
