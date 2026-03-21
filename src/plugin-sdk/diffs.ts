// Narrow plugin-sdk surface for the bundled diffs plugin.
// Keep this list additive and scoped to symbols used under extensions/diffs.

export { definePluginEntry } from "./core.js";
export type { DenebConfig } from "../config/config.js";
export { resolvePreferredDenebTmpDir } from "../infra/tmp-deneb-dir.js";
export type {
  AnyAgentTool,
  DenebPluginApi,
  DenebPluginConfigSchema,
  DenebPluginToolContext,
  PluginLogger,
} from "../plugins/types.js";
