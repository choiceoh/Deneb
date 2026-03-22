// Aggregate barrel for provider plugin development.
// Re-exports the most commonly needed types and utilities so provider
// plugin authors can import from a single surface.

export {
  definePluginEntry,
  type DenebPluginApi,
  type DenebPluginConfigSchema,
  type DenebPluginDefinition,
  type PluginRuntime,
  type PluginCapability,
  type ProviderAuthContext,
  type ProviderAuthMethod,
  type ProviderAuthResult,
  type ProviderCatalogContext,
  type ProviderCatalogResult,
  type ProviderDiscoveryContext,
  type ProviderPrepareRuntimeAuthContext,
  type ProviderPreparedRuntimeAuth,
  type ProviderWrapStreamFnContext,
  type ProviderRuntimeModel,
  type SpeechProviderPlugin,
  type MediaUnderstandingProviderPlugin,
  emptyPluginConfigSchema,
  buildOauthProviderAuthResult,
} from "./core.js";
export type { DenebConfig } from "./core.js";
