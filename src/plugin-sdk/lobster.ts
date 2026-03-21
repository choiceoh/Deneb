// Public Lobster plugin helpers.
// Keep this surface narrow and limited to the Lobster workflow/tool contract.

export { definePluginEntry } from "./core.js";
export type {
  AnyAgentTool,
  DenebPluginApi,
  DenebPluginToolContext,
  DenebPluginToolFactory,
} from "../plugins/types.js";
