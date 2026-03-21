import { createSubsystemLogger } from "../../../logging/subsystem.js";
import type { CompactionSummarizeFn } from "./compaction.js";
import { estimateTokens } from "./engine-helpers.js";
import type { ConversationStore } from "./store/conversation-store.js";
import type { SummaryStore } from "./store/summary-store.js";

const log = createSubsystemLogger("compression-observer");

// ── Public types ─────────────────────────────────────────────────────────────

export interface CompressionObserverConfig {
  /** Whether the observer is enabled (default false). */
  enabled: boolean;
  /** Target compression ratio — summary tokens / source tokens (default 0.2). */
  targetRatio: number;
  /** Number of new messages before triggering a background re-compression (default 5). */
  messageInterval: number;
  /** Model identifier for background compression (e.g. "qwen3.5-35b-a3b"). */
  model?: string;
  /** Provider for the observer model (e.g. "ollama"). */
  provider?: string;
  /** Max staleness in ms before a cached summary is considered expired (default 60000). */
  maxStalenessMs: number;
}

export interface CachedSummary {
  /** The compressed summary text. */
  summary: string;
  /** Estimated token count of the summary. */
  tokenCount: number;
  /** Total source tokens of raw messages that were compressed. */
  sourceTokenCount: number;
  /** Number of raw messages that were compressed. */
  messagesCovered: number;
  /** Timestamp of when this summary was last updated. */
  updatedAt: number;
  /**
   * Whether the source context contained existing summary items (from prior
   * compactions) that are NOT covered by this cached summary. When true, the
   * fast path must not replace the full ordinal range blindly.
   */
  hasMixedContext: boolean;
}

export const DEFAULT_OBSERVER_CONFIG: CompressionObserverConfig = {
  enabled: false,
  targetRatio: 0.2,
  messageInterval: 5,
  maxStalenessMs: 60_000,
};

/** Token drift factor — if current tokens exceed source by this factor, summary is stale. */
const TOKEN_DRIFT_FACTOR = 1.3;

// ── CompressionObserver ──────────────────────────────────────────────────────

export class CompressionObserver {
  private cache = new Map<number, CachedSummary>();
  private messageCounters = new Map<number, number>();
  private pendingUpdates = new Map<number, Promise<void>>();
  private disposed = false;

  constructor(
    private config: CompressionObserverConfig,
    private conversationStore: ConversationStore,
    private summaryStore: SummaryStore,
    private resolveSummarize: () => Promise<CompactionSummarizeFn>,
  ) {}

  /**
   * Called after a new message is ingested into a conversation.
   * Increments the per-conversation counter and enqueues a background
   * compression update when the interval threshold is reached.
   */
  onMessage(conversationId: number): void {
    if (this.disposed || !this.config.enabled) {
      return;
    }

    const count = (this.messageCounters.get(conversationId) ?? 0) + 1;
    this.messageCounters.set(conversationId, count);

    if (count >= this.config.messageInterval) {
      this.messageCounters.set(conversationId, 0);
      this.enqueueUpdate(conversationId);
    }
  }

  /**
   * Get the cached pre-computed summary for a conversation, if available.
   */
  getCachedSummary(conversationId: number): CachedSummary | null {
    if (!this.config.enabled) {
      return null;
    }
    return this.cache.get(conversationId) ?? null;
  }

  /**
   * Check whether the cached summary is fresh enough to use in place of
   * a full compaction pass.
   *
   * A summary is considered fresh when:
   * 1. It exists and is enabled
   * 2. It's not older than maxStalenessMs
   * 3. The current token count hasn't drifted too far from the source
   * 4. The source context was pure raw messages (no mixed summary items)
   */
  isSummaryFresh(conversationId: number, currentTokens: number): boolean {
    if (!this.config.enabled) {
      return false;
    }

    const cached = this.cache.get(conversationId);
    if (!cached) {
      return false;
    }

    // If the context had mixed summary+message items, the observer summary
    // only covers the raw messages. Using it as a fast path replacement would
    // risk destroying existing summary items. Fall back to normal compaction.
    if (cached.hasMixedContext) {
      return false;
    }

    const age = Date.now() - cached.updatedAt;
    if (age > this.config.maxStalenessMs) {
      return false;
    }

    // If current tokens have grown significantly beyond what was compressed,
    // the summary no longer covers enough of the conversation.
    if (currentTokens > cached.sourceTokenCount * TOKEN_DRIFT_FACTOR) {
      return false;
    }

    return true;
  }

  /**
   * Force an immediate background update for a conversation.
   */
  triggerUpdate(conversationId: number): void {
    if (this.disposed || !this.config.enabled) {
      return;
    }
    this.messageCounters.set(conversationId, 0);
    this.enqueueUpdate(conversationId);
  }

  /** Invalidate the cached summary for a conversation. */
  invalidate(conversationId: number): void {
    this.cache.delete(conversationId);
    this.messageCounters.set(conversationId, 0);
  }

  /** Clean up all resources and cancel pending updates. */
  dispose(): void {
    this.disposed = true;
    this.cache.clear();
    this.messageCounters.clear();
    this.pendingUpdates.clear();
  }

  // ── Private ────────────────────────────────────────────────────────────────

  private enqueueUpdate(conversationId: number): void {
    // Skip if an update for this conversation is already in flight.
    if (this.pendingUpdates.has(conversationId)) {
      return;
    }

    const updatePromise = this.runUpdate(conversationId).finally(() => {
      this.pendingUpdates.delete(conversationId);
    });
    // Prevent unhandled promise rejection warnings.
    updatePromise.catch(() => {});
    this.pendingUpdates.set(conversationId, updatePromise);
  }

  private async runUpdate(conversationId: number): Promise<void> {
    if (this.disposed) {
      return;
    }

    try {
      const contextItems = await this.summaryStore.getContextItems(conversationId);
      if (contextItems.length === 0) {
        return;
      }

      // Detect whether the context has mixed content (existing summaries
      // interspersed with raw messages). When mixed, the observer summary
      // cannot safely replace the full raw-message range without risking
      // loss of prior summary information.
      let hasMixedContext = false;

      // Only summarize raw messages — skip existing summary items.
      // This matches what the compaction leafPass operates on.
      const messageTexts: string[] = [];
      let totalSourceTokens = 0;
      let messageCount = 0;

      for (const item of contextItems) {
        if (item.itemType === "summary") {
          hasMixedContext = true;
          continue;
        }
        if (item.itemType !== "message" || item.messageId == null) {
          continue;
        }
        const message = await this.conversationStore.getMessageById(item.messageId);
        if (!message) {
          continue;
        }
        messageTexts.push(message.content);
        totalSourceTokens +=
          message.tokenCount > 0 ? message.tokenCount : estimateTokens(message.content);
        messageCount++;
      }

      if (messageCount === 0 || totalSourceTokens === 0) {
        return;
      }

      // Build the source text for summarization.
      const sourceText = messageTexts.join("\n\n---\n\n");

      // Resolve summarizer and run compression.
      const summarize = await this.resolveSummarize();
      const isAggressive = this.config.targetRatio <= 0.15;
      const summary = await summarize(sourceText, isAggressive);

      if (this.disposed) {
        return;
      }

      const summaryTokens = estimateTokens(summary);

      // Sanity check: reject summaries that are larger than the source.
      // This can happen with poor models that "expand" rather than compress.
      if (summaryTokens >= totalSourceTokens) {
        log.warn(
          `[compression-observer] rejecting summary for conversation=${conversationId}: ` +
            `summaryTokens=${summaryTokens} >= sourceTokens=${totalSourceTokens}`,
        );
        return;
      }

      this.cache.set(conversationId, {
        summary,
        tokenCount: summaryTokens,
        sourceTokenCount: totalSourceTokens,
        messagesCovered: messageCount,
        updatedAt: Date.now(),
        hasMixedContext,
      });

      log.info(
        `[compression-observer] updated cache for conversation=${conversationId} ` +
          `sourceTokens=${totalSourceTokens} summaryTokens=${summaryTokens} ` +
          `ratio=${(summaryTokens / totalSourceTokens).toFixed(3)} messages=${messageCount} ` +
          `hasMixedContext=${hasMixedContext}`,
      );
    } catch (err) {
      log.warn(
        `[compression-observer] background update failed for conversation=${conversationId}: ${
          err instanceof Error ? err.message : String(err)
        }`,
      );
    }
  }
}
