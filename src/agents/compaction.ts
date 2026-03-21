import type { AgentMessage } from "@mariozechner/pi-agent-core";
import type { ExtensionContext } from "@mariozechner/pi-coding-agent";
import { estimateTokens, generateSummary } from "@mariozechner/pi-coding-agent";
import type { AgentCompactionIdentifierPolicy } from "../config/types.agent-defaults.js";
import { retryAsync } from "../infra/retry.js";
import { createSubsystemLogger } from "../logging/subsystem.js";
import { DEFAULT_CONTEXT_TOKENS } from "./defaults.js";
import { repairToolUseResultPairing, stripToolResultDetails } from "./session-transcript-repair.js";

const log = createSubsystemLogger("compaction");

export const BASE_CHUNK_RATIO = 0.4;
export const MIN_CHUNK_RATIO = 0.15;
export const SAFETY_MARGIN = 1.2; // 20% buffer for estimateTokens() inaccuracy
const DEFAULT_SUMMARY_FALLBACK = "No prior history.";

const IDENTIFIER_PRESERVATION_INSTRUCTIONS =
  "Preserve all opaque identifiers exactly as written (no shortening or reconstruction), " +
  "including UUIDs, hashes, IDs, tokens, API keys, hostnames, IPs, ports, URLs, and file names.";

export type CompactionSummarizationInstructions = {
  identifierPolicy?: AgentCompactionIdentifierPolicy;
  identifierInstructions?: string;
};

function resolveIdentifierPreservationInstructions(
  instructions?: CompactionSummarizationInstructions,
): string | undefined {
  const policy = instructions?.identifierPolicy ?? "strict";
  if (policy === "off") {
    return undefined;
  }
  if (policy === "custom") {
    const custom = instructions?.identifierInstructions?.trim();
    return custom && custom.length > 0 ? custom : IDENTIFIER_PRESERVATION_INSTRUCTIONS;
  }
  return IDENTIFIER_PRESERVATION_INSTRUCTIONS;
}

export function buildCompactionSummarizationInstructions(
  customInstructions?: string,
  instructions?: CompactionSummarizationInstructions,
): string | undefined {
  const custom = customInstructions?.trim();
  const identifierPreservation = resolveIdentifierPreservationInstructions(instructions);
  if (!identifierPreservation && !custom) {
    return undefined;
  }
  if (!custom) {
    return identifierPreservation;
  }
  if (!identifierPreservation) {
    return `Additional focus:\n${custom}`;
  }
  return `${identifierPreservation}\n\nAdditional focus:\n${custom}`;
}

export function estimateMessagesTokens(messages: AgentMessage[]): number {
  // SECURITY: toolResult.details can contain untrusted/verbose payloads; never include in LLM-facing compaction.
  const safe = stripToolResultDetails(messages);
  return safe.reduce((sum, message) => sum + estimateTokens(message), 0);
}

function estimateCompactionMessageTokens(message: AgentMessage): number {
  return estimateMessagesTokens([message]);
}

// Overhead reserved for summarization prompt, system prompt, previous summary,
// and serialization wrappers (<conversation> tags, instructions, etc.).
// generateSummary uses reasoning: "high" which also consumes context budget.
export const SUMMARIZATION_OVERHEAD_TOKENS = 4096;

export function chunkMessagesByMaxTokens(
  messages: AgentMessage[],
  maxTokens: number,
): AgentMessage[][] {
  if (messages.length === 0) {
    return [];
  }

  // Apply safety margin to compensate for estimateTokens() underestimation
  const effectiveMax = Math.max(1, Math.floor(maxTokens / SAFETY_MARGIN));

  const chunks: AgentMessage[][] = [];
  let currentChunk: AgentMessage[] = [];
  let currentTokens = 0;

  for (const message of messages) {
    const messageTokens = estimateCompactionMessageTokens(message);
    if (currentChunk.length > 0 && currentTokens + messageTokens > effectiveMax) {
      chunks.push(currentChunk);
      currentChunk = [];
      currentTokens = 0;
    }

    currentChunk.push(message);
    currentTokens += messageTokens;

    if (messageTokens > effectiveMax) {
      // Split oversized messages to avoid unbounded chunk growth.
      chunks.push(currentChunk);
      currentChunk = [];
      currentTokens = 0;
    }
  }

  if (currentChunk.length > 0) {
    chunks.push(currentChunk);
  }

  return chunks;
}

/**
 * Compute adaptive chunk ratio based on average message size.
 * When messages are large, we use smaller chunks to avoid exceeding model limits.
 */
export function computeAdaptiveChunkRatio(messages: AgentMessage[], contextWindow: number): number {
  if (messages.length === 0) {
    return BASE_CHUNK_RATIO;
  }

  const totalTokens = estimateMessagesTokens(messages);
  const avgTokens = totalTokens / messages.length;
  const safeAvgTokens = avgTokens * SAFETY_MARGIN;
  const avgRatio = safeAvgTokens / contextWindow;

  if (avgRatio > 0.1) {
    const reduction = Math.min(avgRatio * 2, BASE_CHUNK_RATIO - MIN_CHUNK_RATIO);
    return Math.max(MIN_CHUNK_RATIO, BASE_CHUNK_RATIO - reduction);
  }

  return BASE_CHUNK_RATIO;
}

/**
 * Check if a single message is too large to summarize.
 * If single message > 50% of context, it can't be summarized safely.
 */
export function isOversizedForSummary(msg: AgentMessage, contextWindow: number): boolean {
  const tokens = estimateCompactionMessageTokens(msg) * SAFETY_MARGIN;
  return tokens > contextWindow * 0.5;
}

/**
 * Summarize messages by chunking and sequentially chaining summaries.
 * Each chunk's summary becomes the previousSummary for the next chunk,
 * building up a complete summary across all messages.
 */
export async function summarizeWithFallback(params: {
  messages: AgentMessage[];
  model: NonNullable<ExtensionContext["model"]>;
  apiKey: string;
  signal: AbortSignal;
  reserveTokens: number;
  maxChunkTokens: number;
  contextWindow: number;
  customInstructions?: string;
  summarizationInstructions?: CompactionSummarizationInstructions;
  previousSummary?: string;
}): Promise<string> {
  const { messages, contextWindow } = params;

  if (messages.length === 0) {
    return params.previousSummary ?? DEFAULT_SUMMARY_FALLBACK;
  }

  // SECURITY: never feed toolResult.details into summarization prompts.
  const safeMessages = stripToolResultDetails(messages);
  const effectiveInstructions = buildCompactionSummarizationInstructions(
    params.customInstructions,
    params.summarizationInstructions,
  );

  // Try full sequential summarization first
  try {
    const chunks = chunkMessagesByMaxTokens(safeMessages, params.maxChunkTokens);
    let summary = params.previousSummary;
    for (const chunk of chunks) {
      summary = await retryAsync(
        () =>
          generateSummary(
            chunk,
            params.model,
            params.reserveTokens,
            params.apiKey,
            params.signal,
            effectiveInstructions,
            summary,
          ),
        {
          attempts: 3,
          minDelayMs: 500,
          maxDelayMs: 5000,
          jitter: 0.2,
          label: "compaction/generateSummary",
          shouldRetry: (err) => !(err instanceof Error && err.name === "AbortError"),
        },
      );
    }
    return summary ?? DEFAULT_SUMMARY_FALLBACK;
  } catch (fullError) {
    log.warn(
      `Full summarization failed, trying partial: ${
        fullError instanceof Error ? fullError.message : String(fullError)
      }`,
    );
  }

  // Fallback: summarize only small messages, note oversized ones
  const smallMessages: AgentMessage[] = [];
  const oversizedNotes: string[] = [];

  for (const msg of messages) {
    if (isOversizedForSummary(msg, contextWindow)) {
      const role = (msg as { role?: string }).role ?? "message";
      const tokens = estimateCompactionMessageTokens(msg);
      oversizedNotes.push(
        `[Large ${role} (~${Math.round(tokens / 1000)}K tokens) omitted from summary]`,
      );
    } else {
      smallMessages.push(msg);
    }
  }

  if (smallMessages.length > 0) {
    try {
      const safeParts = stripToolResultDetails(smallMessages);
      const chunks = chunkMessagesByMaxTokens(safeParts, params.maxChunkTokens);
      let summary = params.previousSummary;
      for (const chunk of chunks) {
        summary = await retryAsync(
          () =>
            generateSummary(
              chunk,
              params.model,
              params.reserveTokens,
              params.apiKey,
              params.signal,
              effectiveInstructions,
              summary,
            ),
          {
            attempts: 3,
            minDelayMs: 500,
            maxDelayMs: 5000,
            jitter: 0.2,
            label: "compaction/generateSummary",
            shouldRetry: (err) => !(err instanceof Error && err.name === "AbortError"),
          },
        );
      }
      const partialSummary = summary ?? DEFAULT_SUMMARY_FALLBACK;
      const notes = oversizedNotes.length > 0 ? `\n\n${oversizedNotes.join("\n")}` : "";
      return partialSummary + notes;
    } catch (partialError) {
      log.warn(
        `Partial summarization also failed: ${
          partialError instanceof Error ? partialError.message : String(partialError)
        }`,
      );
    }
  }

  // Final fallback: just note what was there
  return (
    `Context contained ${messages.length} messages (${oversizedNotes.length} oversized). ` +
    `Summary unavailable due to size limits.`
  );
}

/**
 * Drop oldest messages until history fits within the token budget.
 * Repairs tool_use/tool_result pairing after dropping to prevent
 * "unexpected tool_use_id" errors from API providers.
 */
export function pruneHistoryForContextShare(params: {
  messages: AgentMessage[];
  maxContextTokens: number;
  maxHistoryShare?: number;
}): {
  messages: AgentMessage[];
  droppedMessagesList: AgentMessage[];
  droppedChunks: number;
  droppedMessages: number;
  droppedTokens: number;
  keptTokens: number;
  budgetTokens: number;
} {
  const maxHistoryShare = params.maxHistoryShare ?? 0.5;
  const budgetTokens = Math.max(1, Math.floor(params.maxContextTokens * maxHistoryShare));
  let keptMessages = params.messages;
  const allDroppedMessages: AgentMessage[] = [];
  let droppedChunks = 0;

  while (keptMessages.length > 1 && estimateMessagesTokens(keptMessages) > budgetTokens) {
    // Drop ~25% of remaining messages from the front each iteration
    const dropCount = Math.max(1, Math.floor(keptMessages.length * 0.25));
    const dropped = keptMessages.slice(0, dropCount);
    allDroppedMessages.push(...dropped);
    keptMessages = keptMessages.slice(dropCount);
    droppedChunks += 1;
  }

  // Repair tool_use/tool_result pairing after dropping
  const repairReport = repairToolUseResultPairing(keptMessages);
  keptMessages = repairReport.messages;
  const orphanedCount = repairReport.droppedOrphanCount;

  return {
    messages: keptMessages,
    droppedMessagesList: allDroppedMessages,
    droppedChunks,
    droppedMessages: allDroppedMessages.length + orphanedCount,
    droppedTokens: estimateMessagesTokens(allDroppedMessages),
    keptTokens: estimateMessagesTokens(keptMessages),
    budgetTokens,
  };
}

export function resolveContextWindowTokens(model?: ExtensionContext["model"]): number {
  return Math.max(1, Math.floor(model?.contextWindow ?? DEFAULT_CONTEXT_TOKENS));
}
