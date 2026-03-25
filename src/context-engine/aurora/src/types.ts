/**
 * Core type definitions for the Aurora context engine.
 *
 * These types define the dependency-injection contracts used by the Aurora engine
 * for model completion, gateway RPC, and session key utilities.
 */

import type { AuroraConfig } from "./db/config.js";

/**
 * Minimal LLM completion interface needed by Aurora for summarization.
 * Matches the signature of completeSimple from @mariozechner/pi-ai.
 */
export type CompletionContentBlock = {
  type: string;
  text?: string;
  [key: string]: unknown;
};

export type CompletionResult = {
  content: CompletionContentBlock[];
  [key: string]: unknown;
};

export type CompleteFn = (params: {
  provider?: string;
  model: string;
  apiKey?: string;
  providerApi?: string;
  authProfileId?: string;
  agentDir?: string;
  runtimeConfig?: unknown;
  messages: Array<{ role: string; content: unknown }>;
  system?: string;
  maxTokens: number;
  temperature?: number;
  reasoning?: string;
}) => Promise<CompletionResult>;

/**
 * Gateway RPC call interface.
 */
export type CallGatewayFn = (params: {
  method: string;
  params?: Record<string, unknown>;
  timeoutMs?: number;
}) => Promise<unknown>;

/**
 * Model resolution function — resolves model aliases and defaults.
 * When providerHint is supplied, it takes precedence over env/defaults.
 */
export type ResolveModelFn = (
  modelRef?: string,
  providerHint?: string,
) => {
  provider: string;
  model: string;
};

/**
 * API key resolution function.
 */
export type ApiKeyLookupOptions = {
  profileId?: string;
  preferredProfile?: string;
  agentDir?: string;
  runtimeConfig?: unknown;
};

export type GetApiKeyFn = (
  provider: string,
  model: string,
  options?: ApiKeyLookupOptions,
) => Promise<string | undefined>;

export type RequireApiKeyFn = (
  provider: string,
  model: string,
  options?: ApiKeyLookupOptions,
) => Promise<string>;

/**
 * Session key utilities.
 */
export type ParseAgentSessionKeyFn = (sessionKey: string) => {
  agentId: string;
  suffix: string;
} | null;

export type IsSubagentSessionKeyFn = (sessionKey: string) => boolean;

/**
 * Dependencies injected into the Aurora engine at registration time.
 * These replace all direct imports from Deneb core.
 */
export interface AuroraDependencies {
  /** Aurora configuration (from env vars + plugin config) */
  config: AuroraConfig;

  /** Aurora LLM completion function for summarization */
  complete: CompleteFn;

  /** Gateway RPC call function (for subagent spawning, session ops) */
  callGateway: CallGatewayFn;

  /** Resolve model alias to provider/model pair */
  resolveModel: ResolveModelFn;

  /** Get API key for a provider/model pair */
  getApiKey: GetApiKeyFn;

  /** Require API key (throws if missing) */
  requireApiKey: RequireApiKeyFn;

  /** Parse agent session key into components */
  parseAgentSessionKey: ParseAgentSessionKeyFn;

  /** Check if a session key is a subagent key */
  isSubagentSessionKey: IsSubagentSessionKeyFn;

  /** Normalize an agent ID */
  normalizeAgentId: (id?: string) => string;

  /** Build system prompt for subagent sessions */
  buildSubagentSystemPrompt: (params: {
    depth: number;
    maxDepth: number;
    taskSummary?: string;
  }) => string;

  /** Read the latest assistant reply from a session's messages */
  readLatestAssistantReply: (messages: unknown[]) => string | undefined;

  /** Sanitize tool use/result pairing in message arrays */
  // sanitizeToolUseResultPairing removed — now imported directly in assembler from transcript-repair.ts

  /** Resolve the Deneb agent directory */
  resolveAgentDir: () => string;

  /** Resolve runtime session id from an agent session key */
  resolveSessionIdFromSessionKey: (sessionKey: string) => Promise<string | undefined>;

  /** Agent lane constant for subagents */
  agentLaneSubagent: string;

  /** Logger */
  log: {
    info: (msg: string) => void;
    warn: (msg: string) => void;
    error: (msg: string) => void;
    debug: (msg: string) => void;
  };

  /** Optional callback fired after compaction completes (for user notifications). */
  onCompaction?: (event: {
    conversationId: number;
    tokensBefore: number;
    tokensAfter: number;
    actionTaken: boolean;
    engine?: string;
    durationMs?: number;
  }) => void;
}
