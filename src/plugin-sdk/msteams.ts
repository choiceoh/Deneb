// Narrow plugin-sdk surface for the bundled msteams plugin.
// Keep this list additive and scoped to symbols used under extensions/msteams.

import { createOptionalChannelSetupSurface } from "./channel-setup.js";

export type { ChunkMode } from "../auto-reply/chunk.js";
export type { HistoryEntry } from "../auto-reply/reply/history.js";
export {
  buildPendingHistoryContextFromMap,
  clearHistoryEntriesIfEnabled,
  DEFAULT_GROUP_HISTORY_LIMIT,
  recordPendingHistoryEntryIfEnabled,
} from "../auto-reply/reply/history.js";
export { isSilentReplyText, SILENT_REPLY_TOKEN } from "../auto-reply/tokens.js";
export type { ReplyPayload } from "../auto-reply/types.js";
export { mergeAllowlist, summarizeMapping } from "../channels/allowlists/resolve-utils.js";
export {
  resolveControlCommandGate,
  resolveDualTextControlCommandGate,
} from "../channels/command-gating.js";
export { logInboundDrop, logTypingFailure } from "../channels/logging.js";
export { resolveMentionGating } from "../channels/mention-gating.js";
// Solo-dev stubs for removed plugins/allowlist-match module.
export type AllowlistMatch<T extends string = string> = {
  allowed: boolean;
  matchKey?: string;
  matchSource?: T;
};
export function formatAllowlistMatchMeta(): string {
  return "";
}
export function resolveAllowlistMatchSimple(): { allowed: true } {
  return { allowed: true };
}
export {
  buildChannelKeyCandidates,
  normalizeChannelSlug,
  resolveChannelEntryMatchWithFallback,
  resolveNestedAllowlistDecision,
} from "../channels/plugins/channel-config.js";
export { buildChannelConfigSchema } from "../channels/plugins/config-schema.js";
export { resolveChannelMediaMaxBytes } from "../channels/plugins/media-limits.js";
export { buildMediaPayload } from "../channels/plugins/media-payload.js";
export {
  addWildcardAllowFrom,
  mergeAllowFromEntries,
  setTopLevelChannelAllowFrom,
  setTopLevelChannelDmPolicyWithAllowFrom,
  setTopLevelChannelGroupPolicy,
  splitSetupEntries,
} from "../channels/plugins/setup-wizard-helpers.js";
// Solo-dev stub for removed pairing-message module.
export const PAIRING_APPROVED_MESSAGE = "Pairing approved.";
export { resolveOutboundMediaUrls, resolveSendableOutboundReplyParts } from "./reply-payload.js";
export type {
  BaseProbeResult,
  ChannelDirectoryEntry,
  ChannelGroupContext,
  ChannelMessageActionName,
  ChannelOutboundAdapter,
} from "../channels/plugins/types.js";
export type { ChannelPlugin } from "../channels/plugins/types.plugin.js";
export { createChannelReplyPipeline } from "./channel-reply-pipeline.js";
export type { DenebConfig } from "../config/config.js";
export { isDangerousNameMatchingEnabled } from "../config/dangerous-name-matching.js";
export { resolveToolsBySender } from "../config/group-policy.js";
export {
  resolveAllowlistProviderRuntimeGroupPolicy,
  resolveDefaultGroupPolicy,
} from "../config/runtime-group-policy.js";
export type {
  DmPolicy,
  GroupPolicy,
  GroupToolPolicyConfig,
  MarkdownTableMode,
  MSTeamsChannelConfig,
  MSTeamsConfig,
  MSTeamsReplyStyle,
  MSTeamsTeamConfig,
} from "../config/types.js";
export {
  hasConfiguredSecretInput,
  normalizeResolvedSecretInputString,
  normalizeSecretInputString,
} from "../config/types.secrets.js";
export { MSTeamsConfigSchema } from "../config/zod-schema.providers-core.js";
export { DEFAULT_WEBHOOK_MAX_BODY_BYTES } from "../infra/http-body.js";
export { fetchWithSsrFGuard } from "../infra/net/fetch-guard.js";
export type { SsrFPolicy } from "../infra/net/ssrf.js";
export { isPrivateIpAddress } from "../infra/net/ssrf.js";
export { detectMime, extensionForMime, getFileExtension } from "../media/mime.js";
export { extractOriginalFilename } from "../media/store.js";
export { emptyPluginConfigSchema } from "../plugins/config-schema.js";
export type { PluginRuntime } from "../plugins/runtime/types.js";
export type { DenebPluginApi } from "../plugins/types.js";
export { DEFAULT_ACCOUNT_ID } from "../routing/session-key.js";
export type { RuntimeEnv } from "../runtime.js";
// Solo-dev stubs for removed dm-policy-shared module.
export function readStoreAllowFromForDmPolicy(): string[] {
  return [];
}
export function resolveDmGroupAccessWithLists(): { allowed: true } {
  return { allowed: true };
}
export function resolveEffectiveAllowFromLists(): { combined: string[]; hasWildcard: boolean } {
  return { combined: [], hasWildcard: false };
}
// Solo-dev stubs for removed group-access module.
export function evaluateSenderGroupAccessForPolicy(): { allowed: true } {
  return { allowed: true };
}
export function resolveSenderScopedGroupPolicy(): unknown {
  return { groupPolicy: "open" };
}
export { formatDocsLink } from "../terminal/links.js";
export { sleep } from "../utils.js";
export { loadWebMedia } from "./web-media.js";
export type { WizardPrompter } from "../wizard/prompts.js";
export { keepHttpServerTaskAlive } from "./channel-lifecycle.js";
export { withFileLock } from "./file-lock.js";
export { dispatchReplyFromConfigWithSettledDispatcher } from "./inbound-reply-dispatch.js";
export { readJsonFileWithFallback, writeJsonFileAtomically } from "./json-store.js";
export { loadOutboundMediaFromUrl } from "./outbound-media.js";
// Solo-dev stub for removed channel-pairing module.
export function createChannelPairingController(): unknown {
  return {};
}
export { resolveInboundSessionEnvelopeContext } from "../channels/session-envelope.js";
export {
  buildHostnameAllowlistPolicyFromSuffixAllowlist,
  isHttpsUrlAllowedByHostnameSuffixAllowlist,
  normalizeHostnameSuffixAllowlist,
} from "./ssrf-policy.js";
export {
  buildBaseChannelStatusSummary,
  buildProbeChannelStatusSummary,
  buildRuntimeAccountStatusSnapshot,
  createDefaultChannelRuntimeState,
} from "./status-helpers.js";
export { normalizeStringEntries } from "../shared/string-normalization.js";

const msteamsSetup = createOptionalChannelSetupSurface({
  channel: "msteams",
  label: "Microsoft Teams",
  npmSpec: "@deneb/msteams",
  docsPath: "/channels/msteams",
});

export const msteamsSetupWizard = msteamsSetup.setupWizard;
export const msteamsSetupAdapter = msteamsSetup.setupAdapter;
