// Public API barrel for src/channels/

// Channel plugin registry
export {
  applyChannelMatchMeta,
  buildChannelKeyCandidates,
  getChannelPlugin,
  listChannelPlugins,
  normalizeChannelId,
  normalizeChannelSlug,
  resolveChannelEntryMatch,
  resolveChannelEntryMatchWithFallback,
  resolveChannelMatchConfig,
  resolveNestedAllowlistDecision,
} from "./plugins/index.js";
export type {
  ChannelEntryMatch,
  ChannelId,
  ChannelMatchSource,
  ChannelPlugin,
} from "./plugins/index.js";

// Channel plugin types
export { CHANNEL_MESSAGE_ACTION_NAMES, CHANNEL_MESSAGE_CAPABILITIES } from "./plugins/types.js";
export type {
  BaseProbeResult,
  BaseTokenResolution,
  ChannelAccountSnapshot,
  ChannelAccountState,
  ChannelAgentPromptAdapter,
  ChannelAgentTool,
  ChannelAgentToolFactory,
  ChannelAllowlistAdapter,
  ChannelAuthAdapter,
  ChannelCapabilities,
  ChannelCapabilitiesDiagnostics,
  ChannelCapabilitiesDisplayLine,
  ChannelCapabilitiesDisplayTone,
  ChannelCommandAdapter,
  ChannelConfigAdapter,
  ChannelConfiguredBindingConversationRef,
  ChannelConfiguredBindingMatch,
  ChannelConfiguredBindingProvider,
  ChannelDirectoryAdapter,
  ChannelDirectoryEntry,
  ChannelDirectoryEntryKind,
  ChannelElevatedAdapter,
  ChannelExecApprovalAdapter,
  ChannelExecApprovalForwardTarget,
  ChannelExecApprovalInitiatingSurfaceState,
  ChannelGatewayAdapter,
  ChannelGatewayContext,
  ChannelGroupAdapter,
  ChannelGroupContext,
  ChannelHeartbeatAdapter,
  ChannelHeartbeatDeps,
  ChannelLifecycleAdapter,
  ChannelLogSink,
  ChannelLoginWithQrStartResult,
  ChannelLoginWithQrWaitResult,
  ChannelLogoutContext,
  ChannelLogoutResult,
  ChannelMentionAdapter,
  ChannelMessageActionAdapter,
  ChannelMessageActionContext,
  ChannelMessageActionDiscoveryContext,
  ChannelMessageActionName,
  ChannelMessageCapability,
  ChannelMessageToolDiscovery,
  ChannelMessageToolSchemaContribution,
  ChannelMessagingAdapter,
  ChannelMeta,
  ChannelOutboundAdapter,
  ChannelOutboundContext,
  ChannelOutboundTargetMode,
  ChannelPairingAdapter,
  ChannelPollContext,
  ChannelPollResult,
  ChannelResolveKind,
  ChannelResolveResult,
  ChannelResolverAdapter,
  ChannelSecurityAdapter,
  ChannelSecurityContext,
  ChannelSecurityDmPolicy,
  ChannelSetupAdapter,
  ChannelSetupInput,
  ChannelStatusAdapter,
  ChannelStatusIssue,
  ChannelStreamingAdapter,
  ChannelStructuredComponents,
  ChannelThreadingAdapter,
  ChannelThreadingContext,
  ChannelThreadingToolContext,
  ChannelToolSend,
} from "./plugins/types.js";

// Config writes
export {
  authorizeConfigWrite,
  canBypassConfigWritePolicy,
  formatConfigWriteDeniedMessage,
  resolveChannelConfigWrites,
  resolveConfigWriteTargetFromPath,
  resolveExplicitConfigWriteTarget,
} from "./plugins/config-writes.js";
export type {
  ConfigWriteAuthorizationResult,
  ConfigWriteScope,
  ConfigWriteTarget,
} from "./plugins/config-writes.js";

// Plugin helpers
export {
  buildAccountScopedDmSecurityPolicy,
  formatPairingApproveHint,
  parseOptionalDelimitedEntries,
  resolveChannelDefaultAccountId,
} from "./plugins/helpers.js";

// Target parsing
export { parseExplicitTargetForChannel } from "./plugins/target-parsing.js";
export type { ParsedChannelExplicitTarget } from "./plugins/target-parsing.js";

// Chat type
export { normalizeChatType } from "./chat-type.js";
export type { ChatType } from "./chat-type.js";

// Conversation label
export { resolveConversationLabel } from "./conversation-label.js";

// Typing
export { createTypingCallbacks } from "./typing.js";
export type { CreateTypingCallbacksParams, TypingCallbacks } from "./typing.js";
