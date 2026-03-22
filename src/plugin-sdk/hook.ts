// Aggregate barrel for hook plugin development.
// Re-exports the most commonly needed types for plugins that primarily
// register hooks (lifecycle, message processing, etc.).

export {
  definePluginEntry,
  type DenebPluginApi,
  type DenebPluginDefinition,
  type DenebPluginConfigSchema,
  type PluginRuntime,
  type PluginCapability,
  emptyPluginConfigSchema,
} from "./core.js";
