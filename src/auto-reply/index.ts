// Public API barrel for src/auto-reply/

// Core types
export type {
  BlockReplyContext,
  GetReplyOptions,
  ModelSelectedContext,
  ReplyPayload,
  TypingPolicy,
} from "./types.js";

// Dispatch
export {
  dispatchInboundMessage,
  dispatchInboundMessageWithBufferedDispatcher,
  dispatchInboundMessageWithDispatcher,
  withReplyDispatcher,
} from "./dispatch.js";
export type { DispatchInboundResult } from "./dispatch.js";

// Reply
export {
  extractElevatedDirective,
  extractExecDirective,
  extractQueueDirective,
  extractReasoningDirective,
  extractReplyToTag,
  extractThinkDirective,
  extractVerboseDirective,
  getReplyFromConfig,
} from "./reply.js";

// Tokens
export {
  HEARTBEAT_TOKEN,
  SILENT_REPLY_TOKEN,
  isSilentReplyPrefixText,
  isSilentReplyText,
  stripSilentToken,
} from "./tokens.js";

// Heartbeat
export {
  DEFAULT_HEARTBEAT_ACK_MAX_CHARS,
  DEFAULT_HEARTBEAT_EVERY,
  HEARTBEAT_PROMPT,
  isHeartbeatContentEffectivelyEmpty,
  resolveHeartbeatPrompt,
  stripHeartbeatToken,
} from "./heartbeat.js";
export type { StripHeartbeatMode } from "./heartbeat.js";

// Thinking & model reasoning
export {
  formatThinkingLevels,
  formatXHighModelHint,
  isBinaryThinkingProvider,
  listThinkingLevelLabels,
  listThinkingLevels,
  normalizeElevatedLevel,
  normalizeFastMode,
  normalizeReasoningLevel,
  normalizeThinkLevel,
  normalizeUsageDisplay,
  normalizeVerboseLevel,
  resolveResponseUsageMode,
  resolveThinkingDefaultForModel,
  supportsXHighThinking,
} from "./thinking.js";
export type {
  ElevatedLevel,
  ReasoningLevel,
  ThinkLevel,
  ThinkingCatalogEntry,
  UsageDisplayLevel,
  VerboseLevel,
} from "./thinking.js";

// Model & status
export { extractModelDirective } from "./model.js";
export {
  buildCommandsMessage,
  buildCommandsMessagePaginated,
  buildHelpMessage,
  buildStatusMessage,
  formatContextUsageShort,
  formatTokenCount,
} from "./status.js";
export type { CommandsMessageOptions, CommandsMessageResult } from "./status.js";

// Command registry
export {
  buildCommandText,
  buildCommandTextFromArgs,
  findCommandByNativeName,
  getCommandDetection,
  isCommandEnabled,
  isNativeCommandSurface,
  listChatCommands,
  listChatCommandsForConfig,
  listNativeCommandSpecs,
  listNativeCommandSpecsForConfig,
  maybeResolveTextAlias,
  normalizeCommandBody,
  parseCommandArgs,
  resolveCommandArgChoices,
  resolveCommandArgMenu,
  serializeCommandArgs,
  shouldHandleTextCommands,
} from "./commands-registry.js";
export type {
  ChatCommandDefinition,
  CommandArgChoiceContext,
  CommandArgDefinition,
  CommandArgMenuSpec,
  CommandArgValues,
  CommandArgs,
  CommandDetection,
  CommandNormalizeOptions,
  CommandScope,
  NativeCommandSpec,
  ResolvedCommandArgChoice,
  ShouldHandleTextCommandsParams,
} from "./commands-registry.js";

// Command detection
export {
  hasControlCommand,
  hasInlineCommandTokens,
  isControlCommandMessage,
  shouldComputeCommandAuthorized,
} from "./command-detection.js";

// Text chunking
export {
  chunkByNewline,
  chunkByParagraph,
  chunkMarkdownText,
  chunkMarkdownTextWithMode,
  chunkText,
  chunkTextWithMode,
  resolveChunkMode,
  resolveTextChunkLimit,
} from "./chunk.js";
export type { ChunkMode, TextChunkProvider } from "./chunk.js";

// Envelope formatting
export {
  formatAgentEnvelope,
  formatEnvelopeTimestamp,
  formatInboundEnvelope,
  formatInboundFromLabel,
  resolveEnvelopeFormatOptions,
} from "./envelope.js";
export type { AgentEnvelopeParams, EnvelopeFormatOptions } from "./envelope.js";

// Templating
export { applyTemplate } from "./templating.js";
export type {
  FinalizedMsgContext,
  MsgContext,
  OriginatingChannelType,
  TemplateContext,
} from "./templating.js";

// Group activation
export { normalizeGroupActivation, parseActivationCommand } from "./group-activation.js";
export type { GroupActivationMode } from "./group-activation.js";

// Inbound debounce
export { createInboundDebouncer, resolveInboundDebounceMs } from "./inbound-debounce.js";
export type { InboundDebounceCreateParams } from "./inbound-debounce.js";

// Fallback state
export {
  buildFallbackAttemptSummaries,
  buildFallbackClearedNotice,
  buildFallbackNotice,
  buildFallbackReasonSummary,
  formatFallbackAttemptReason,
  normalizeFallbackModelRef,
  resolveActiveFallbackState,
  resolveFallbackTransition,
} from "./fallback-state.js";
export type { FallbackNoticeState, ResolvedFallbackTransition } from "./fallback-state.js";

// Skill commands
export {
  listReservedChatSlashCommandNames,
  listSkillCommandsForAgents,
  listSkillCommandsForWorkspace,
  resolveSkillCommandInvocation,
} from "./skill-commands.js";
