// Aggregate barrel for tool plugin development.
// Re-exports the most commonly needed types and utilities so tool
// plugin authors can import from a single surface.

export {
  definePluginEntry,
  type DenebPluginApi,
  type DenebPluginDefinition,
  type DenebPluginConfigSchema,
  type AnyAgentTool,
  type PluginRuntime,
  type PluginCapability,
  type DenebPluginCommandDefinition,
  type PluginCommandContext,
  emptyPluginConfigSchema,
  channelTargetSchema,
  channelTargetsSchema,
  optionalStringEnum,
  stringEnum,
} from "./core.js";
