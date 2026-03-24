/**
 * Native-accelerated compaction engine wrapper.
 *
 * Drives the Rust `SweepEngine` state machine from TypeScript, executing
 * I/O commands (DB reads, LLM calls, DB writes) in the host and feeding
 * results back to Rust for the next algorithmic decision.
 *
 * Falls back to the existing TypeScript `CompactionEngine` when the native
 * module is unavailable.
 */

import { getNative } from "./loader.js";

// ── Pure function wrappers ──────────────────────────────────────────────────

/**
 * Estimate token count from text (native Rust or TS fallback).
 * Matches `Math.ceil(text.length / 4)`.
 */
export function estimateTokensNative(text: string): number {
  const native = getNative();
  if (native) {
    return native.compactionEstimateTokens(text);
  }
  return Math.ceil(text.length / 4);
}

/**
 * Format a timestamp as `YYYY-MM-DD HH:mm TZ` using Rust chrono.
 * Falls back to UTC formatting if native is unavailable.
 */
export function formatTimestampNative(epochMs: number, timezone: string): string | undefined {
  const native = getNative();
  if (native) {
    return native.compactionFormatTimestamp(epochMs, timezone);
  }
  return undefined;
}

/**
 * Generate a summary ID using Rust SHA256.
 * Falls back to undefined if native is unavailable.
 */
export function generateSummaryIdNative(content: string): string | undefined {
  const native = getNative();
  if (native) {
    return native.compactionGenerateSummaryId(content, Date.now());
  }
  return undefined;
}

/**
 * Deterministic fallback for LLM summarization failure.
 */
export function deterministicFallbackNative(source: string, inputTokens: number): string | undefined {
  const native = getNative();
  if (native) {
    return native.compactionDeterministicFallback(source, inputTokens);
  }
  return undefined;
}

// ── Sweep engine types ──────────────────────────────────────────────────────

/** SweepCommand from Rust — tagged union. */
export type SweepCommand =
  | { type: "fetchContextItems"; conversationId: number }
  | { type: "fetchMessages"; messageIds: number[] }
  | { type: "fetchSummaries"; summaryIds: string[] }
  | { type: "fetchTokenCount"; conversationId: number }
  | { type: "fetchDistinctDepths"; conversationId: number; maxOrdinal: number }
  | {
      type: "summarize";
      text: string;
      aggressive: boolean;
      options?: {
        previousSummary?: string;
        isCondensed?: boolean;
        depth?: number;
        targetTokens?: number;
      };
    }
  | { type: "persistLeafSummary"; input: Record<string, unknown> }
  | { type: "persistCondensedSummary"; input: Record<string, unknown> }
  | { type: "persistEvent"; input: Record<string, unknown> }
  | { type: "done"; result: CompactionResultNative };

export interface CompactionResultNative {
  actionTaken: boolean;
  tokensBefore: number;
  tokensAfter: number;
  createdSummaryId?: string;
  condensed: boolean;
  level?: "normal" | "aggressive" | "fallback";
}

/** Check if the native compaction engine is available. */
export function isNativeCompactionAvailable(): boolean {
  const native = getNative();
  return native != null && typeof native.compactionSweepNew === "function";
}

/**
 * Run a compaction sweep using the native Rust engine.
 *
 * The caller provides callback functions for executing I/O operations.
 * Returns null if native module is unavailable (caller should fall back
 * to the TypeScript CompactionEngine).
 */
export async function runNativeCompactionSweep(params: {
  configJson: string;
  conversationId: number;
  tokenBudget: number;
  force: boolean;
  hardTrigger: boolean;
  // I/O callbacks
  fetchContextItems: (conversationId: number) => Promise<unknown[]>;
  fetchMessages: (messageIds: number[]) => Promise<Record<number, unknown>>;
  fetchSummaries: (summaryIds: string[]) => Promise<Record<string, unknown>>;
  fetchTokenCount: (conversationId: number) => Promise<number>;
  fetchDistinctDepths: (conversationId: number, maxOrdinal: number) => Promise<number[]>;
  summarize: (text: string, aggressive: boolean, options?: Record<string, unknown>) => Promise<string>;
  persistLeafSummary: (input: Record<string, unknown>) => Promise<void>;
  persistCondensedSummary: (input: Record<string, unknown>) => Promise<void>;
  persistEvent: (input: Record<string, unknown>) => Promise<void>;
}): Promise<CompactionResultNative | null> {
  const native = getNative();
  if (!native || typeof native.compactionSweepNew !== "function") {
    return null;
  }

  const handle = native.compactionSweepNew(
    params.configJson,
    params.conversationId,
    params.tokenBudget,
    params.force,
    params.hardTrigger,
    Date.now(),
  );

  try {
    let cmdJson = native.compactionSweepStart(handle);
    let cmd = JSON.parse(cmdJson) as SweepCommand;

    while (cmd.type !== "done") {
      const response = await executeCommand(cmd, params);
      const responseJson = JSON.stringify(response);
      cmdJson = native.compactionSweepStep(handle, responseJson);
      cmd = JSON.parse(cmdJson) as SweepCommand;
    }

    return cmd.result;
  } finally {
    native.compactionSweepDrop(handle);
  }
}

// ── Internal ────────────────────────────────────────────────────────────────

async function executeCommand(
  cmd: SweepCommand,
  params: {
    fetchContextItems: (conversationId: number) => Promise<unknown[]>;
    fetchMessages: (messageIds: number[]) => Promise<Record<number, unknown>>;
    fetchSummaries: (summaryIds: string[]) => Promise<Record<string, unknown>>;
    fetchTokenCount: (conversationId: number) => Promise<number>;
    fetchDistinctDepths: (conversationId: number, maxOrdinal: number) => Promise<number[]>;
    summarize: (text: string, aggressive: boolean, options?: Record<string, unknown>) => Promise<string>;
    persistLeafSummary: (input: Record<string, unknown>) => Promise<void>;
    persistCondensedSummary: (input: Record<string, unknown>) => Promise<void>;
    persistEvent: (input: Record<string, unknown>) => Promise<void>;
  },
): Promise<Record<string, unknown>> {
  switch (cmd.type) {
    case "fetchContextItems": {
      const items = await params.fetchContextItems(cmd.conversationId);
      return { type: "contextItems", items };
    }
    case "fetchMessages": {
      const messages = await params.fetchMessages(cmd.messageIds);
      return { type: "messages", messages };
    }
    case "fetchSummaries": {
      const summaries = await params.fetchSummaries(cmd.summaryIds);
      return { type: "summaries", summaries };
    }
    case "fetchTokenCount": {
      const count = await params.fetchTokenCount(cmd.conversationId);
      return { type: "tokenCount", count };
    }
    case "fetchDistinctDepths": {
      const depths = await params.fetchDistinctDepths(cmd.conversationId, cmd.maxOrdinal);
      return { type: "distinctDepths", depths };
    }
    case "summarize": {
      const text = await params.summarize(cmd.text, cmd.aggressive, cmd.options);
      return { type: "summaryText", text };
    }
    case "persistLeafSummary": {
      try {
        await params.persistLeafSummary(cmd.input);
        return { type: "persistOk" };
      } catch (err) {
        return { type: "persistError", error: err instanceof Error ? err.message : String(err) };
      }
    }
    case "persistCondensedSummary": {
      try {
        await params.persistCondensedSummary(cmd.input);
        return { type: "persistOk" };
      } catch (err) {
        return { type: "persistError", error: err instanceof Error ? err.message : String(err) };
      }
    }
    case "persistEvent": {
      try {
        await params.persistEvent(cmd.input);
        return { type: "persistOk" };
      } catch (err) {
        return { type: "persistError", error: err instanceof Error ? err.message : String(err) };
      }
    }
    default:
      return { type: "persistError", error: `Unknown command type: ${(cmd as { type: string }).type}` };
  }
}
