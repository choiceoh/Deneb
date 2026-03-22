// Aggregate barrel for channel plugin development.
// Re-exports the most commonly needed types and utilities from individual
// channel-* subpaths so plugin authors can import from a single surface.

export {
  defineChannelPluginEntry,
  defineSetupPluginEntry,
  createChannelPluginBase,
  type ChannelPlugin,
  type DenebPluginApi,
  type PluginRuntime,
  type PluginCapability,
  type ChannelOutboundSessionRoute,
  type ChannelMessagingAdapter,
  type ChannelMessageActionContext,
  buildChannelOutboundSessionRoute,
  buildChannelConfigSchema,
  DEFAULT_ACCOUNT_ID,
  normalizeAccountId,
  stripChannelTargetPrefix,
  stripTargetKindPrefix,
  getChatChannelMeta,
  emptyPluginConfigSchema,
} from "./core.js";
export type {
  ChannelSetupAdapter,
  ChannelSetupDmPolicy,
  ChannelSetupWizard,
  OptionalChannelSetupSurface,
} from "./channel-setup.js";
export {
  createTopLevelChannelDmPolicy,
  formatDocsLink,
  setSetupChannelEnabled,
  splitSetupEntries,
  createOptionalChannelSetupSurface,
} from "./channel-setup.js";
export type { ChannelSendRawResult } from "./channel-send-result.js";
export {
  buildChannelSendResult,
  createRawChannelSendResultAdapter,
} from "./channel-send-result.js";
