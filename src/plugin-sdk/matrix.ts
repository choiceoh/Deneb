// Narrow plugin-sdk surface for the bundled matrix plugin.
// Keep this list additive and scoped to symbols used under extensions/matrix.

import { createOptionalChannelSetupSurface } from "./channel-setup.js";

export {
  createActionGate,
  jsonResult,
  readNumberParam,
  readReactionParams,
  readStringArrayParam,
  readStringParam,
} from "../agents/tools/common.js";
export type { ReplyPayload } from "../auto-reply/types.js";
export { resolveAckReaction } from "../agents/identity.js";
// Solo-dev stubs for removed allowlist-match module.
export function compileAllowlist(): unknown {
  return {};
}
export function resolveCompiledAllowlistMatch(): { allowed: true } {
  return { allowed: true };
}
export function resolveAllowlistCandidates(): string[] {
  return [];
}
export function resolveAllowlistMatchByCandidates(): { allowed: true } {
  return { allowed: true };
}
export {
  addAllowlistUserEntriesFromConfigEntry,
  buildAllowlistResolutionSummary,
  canonicalizeAllowlistWithResolvedIds,
  mergeAllowlist,
  patchAllowlistUsersInConfigEntries,
  summarizeMapping,
} from "../channels/allowlists/resolve-utils.js";
export { ensureConfiguredAcpBindingReady } from "../acp/persistent-bindings.lifecycle.js";
export { resolveConfiguredAcpBindingRecord } from "../acp/persistent-bindings.resolve.js";
export { resolveControlCommandGate } from "../channels/command-gating.js";
export type { NormalizedLocation } from "../channels/location.js";
export { formatLocationText, toLocationContext } from "../channels/location.js";
export { logInboundDrop, logTypingFailure } from "../channels/logging.js";
// Solo-dev stubs for removed plugins/allowlist-match module.
export type AllowlistMatch<T extends string = string> = {
  allowed: boolean;
  matchKey?: string;
  matchSource?: T;
};
export function formatAllowlistMatchMeta(): string {
  return "";
}
export {
  buildChannelKeyCandidates,
  resolveChannelEntryMatch,
} from "../channels/plugins/channel-config.js";
export { createAccountListHelpers } from "../channels/plugins/account-helpers.js";
export {
  deleteAccountFromConfigSection,
  setAccountEnabledInConfigSection,
} from "../channels/plugins/config-helpers.js";
export { buildChannelConfigSchema } from "../channels/plugins/config-schema.js";
export { formatPairingApproveHint } from "../channels/plugins/helpers.js";
export {
  buildSingleChannelSecretPromptState,
  addWildcardAllowFrom,
  mergeAllowFromEntries,
  promptAccountId,
  promptSingleChannelSecretInput,
  setTopLevelChannelGroupPolicy,
} from "../channels/plugins/setup-wizard-helpers.js";
export { promptChannelAccessConfig } from "../channels/plugins/setup-group-access.js";
// Solo-dev stub for removed pairing-message module.
export const PAIRING_APPROVED_MESSAGE = "Pairing approved.";
export {
  applyAccountNameToChannelSection,
  moveSingleAccountChannelSectionToDefaultAccount,
} from "../channels/plugins/setup-helpers.js";
export type {
  BaseProbeResult,
  ChannelDirectoryEntry,
  ChannelGroupContext,
  ChannelMessageActionAdapter,
  ChannelMessageActionContext,
  ChannelMessageActionName,
  ChannelMessageToolDiscovery,
  ChannelMessageToolSchemaContribution,
  ChannelOutboundAdapter,
  ChannelResolveKind,
  ChannelResolveResult,
  ChannelSetupInput,
  ChannelToolSend,
} from "../channels/plugins/types.js";
export type { ChannelPlugin } from "../channels/plugins/types.plugin.js";
export { createReplyPrefixOptions } from "../channels/reply-prefix.js";
export { resolveThreadBindingFarewellText } from "../channels/thread-bindings-messages.js";
export {
  resolveThreadBindingIdleTimeoutMsForChannel,
  resolveThreadBindingMaxAgeMsForChannel,
} from "../channels/thread-bindings-policy.js";
export { createTypingCallbacks } from "../channels/typing.js";
export { createChannelReplyPipeline } from "./channel-reply-pipeline.js";
export type { DenebConfig } from "../config/config.js";
export {
  GROUP_POLICY_BLOCKED_LABEL,
  resolveAllowlistProviderRuntimeGroupPolicy,
  resolveDefaultGroupPolicy,
  warnMissingProviderGroupPolicyFallbackOnce,
} from "../config/runtime-group-policy.js";
export type {
  DmPolicy,
  GroupPolicy,
  GroupToolPolicyConfig,
  MarkdownTableMode,
} from "../config/types.js";
export type { SecretInput } from "./secret-input.js";
export {
  buildSecretInputSchema,
  hasConfiguredSecretInput,
  normalizeResolvedSecretInputString,
  normalizeSecretInputString,
} from "./secret-input.js";
export { ToolPolicySchema } from "../config/zod-schema.agent-runtime.js";
export { MarkdownConfigSchema } from "../config/zod-schema.core.js";
export { formatZonedTimestamp } from "../infra/format-time/format-datetime.js";
export { fetchWithSsrFGuard } from "../infra/net/fetch-guard.js";
export { maybeCreateMatrixMigrationSnapshot } from "../infra/matrix-migration-snapshot.js";
export {
  getSessionBindingService,
  registerSessionBindingAdapter,
  unregisterSessionBindingAdapter,
} from "../infra/outbound/session-binding-service.js";
export { resolveOutboundSendDep } from "../infra/outbound/send-deps.js";
export type {
  BindingTargetKind,
  SessionBindingRecord,
} from "../infra/outbound/session-binding-service.js";
export { isPrivateOrLoopbackHost } from "../gateway/net.js";
export { getAgentScopedMediaLocalRoots } from "../media/local-roots.js";
export { emptyPluginConfigSchema } from "../plugins/config-schema.js";
export type { PluginRuntime, RuntimeLogger } from "../plugins/runtime/types.js";
export type { DenebPluginApi } from "../plugins/types.js";
export type { PollInput } from "../polls.js";
export { normalizePollInput } from "../polls.js";
export {
  DEFAULT_ACCOUNT_ID,
  normalizeAccountId,
  normalizeOptionalAccountId,
  resolveAgentIdFromSessionKey,
} from "../routing/session-key.js";
export type { RuntimeEnv } from "../runtime.js";
export { normalizeStringEntries } from "../shared/string-normalization.js";
export { formatDocsLink } from "../terminal/links.js";
export { redactSensitiveText } from "../logging/redact.js";
export type { WizardPrompter } from "../wizard/prompts.js";
// Solo-dev stubs for removed group-access module.
export function evaluateGroupRouteAccessForPolicy(): { allowed: true } {
  return { allowed: true };
}
export function resolveSenderScopedGroupPolicy(): unknown {
  return { groupPolicy: "open" };
}
// Solo-dev stub for removed channel-pairing module.
export function createChannelPairingController(): unknown {
  return {};
}
export { readJsonFileWithFallback, writeJsonFileAtomically } from "./json-store.js";
export { formatResolvedUnresolvedNote } from "./resolution-notes.js";
export { runPluginCommandWithTimeout } from "./run-command.js";
export { createLoggerBackedRuntime, resolveRuntimeEnv } from "./runtime.js";
export { dispatchReplyFromConfigWithSettledDispatcher } from "./inbound-reply-dispatch.js";
export {
  buildProbeChannelStatusSummary,
  collectStatusIssuesFromLastError,
} from "./status-helpers.js";

const matrixSetup = createOptionalChannelSetupSurface({
  channel: "matrix",
  label: "Matrix",
  npmSpec: "@deneb/matrix",
  docsPath: "/channels/matrix",
});

export const matrixSetupWizard = matrixSetup.setupWizard;
export const matrixSetupAdapter = matrixSetup.setupAdapter;
