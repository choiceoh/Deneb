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
  /** Total source tokens of raw messages that were compressed (excluding fresh tail). */
  sourceTokenCount: number;
  /** Number of raw messages that were compressed (excluding fresh tail). */
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

/**
 * Number of recent raw messages to exclude from observer summarization,
 * matching the concept of the "fresh tail" in CompactionEngine. The fast
 * path only replaces messages outside this tail, so the observer should
 * only summarize what the fast path would replace.
 */
const OBSERVER_FRESH_TAIL_COUNT = 8;

/** Retry delay for transient LLM call failures (ms). */
const RETRY_DELAY_MS = 2_000;
/** Maximum retries per runUpdate LLM call. */
const MAX_CALL_RETRIES = 2;
/** Consecutive update failures before entering cooldown. */
const MAX_CONSECUTIVE_FAILURES = 3;
/** Cooldown duration after MAX_CONSECUTIVE_FAILURES (ms). */
const FAILURE_COOLDOWN_MS = 120_000;

// ── CompressionObserver ──────────────────────────────────────────────────────

/**
 * Background observer that continuously maintains a pre-computed compressed
 * summary of conversation context. The summarizer function must be resolved
 * and validated before constructing the observer — the constructor receives
 * a ready-to-use function, not a resolver.
 */
export class CompressionObserver {
  private cache = new Map<number, CachedSummary>();
  private messageCounters = new Map<number, number>();
  private pendingUpdates = new Map<number, Promise<void>>();
  /** Per-conversation flag: true when a re-trigger was requested while an update was in flight. */
  private retriggerNeeded = new Set<number>();
  private disposed = false;

  /** Consecutive update failure count (reset on success). */
  private consecutiveFailures = 0;
  /** Timestamp when cooldown ends (0 = not in cooldown). */
  private cooldownUntil = 0;

  constructor(
    private config: CompressionObserverConfig,
    private conversationStore: ConversationStore,
    private summaryStore: SummaryStore,
    /** Ready-to-use summarizer. Resolved and validated before construction. */
    private summarize: CompactionSummarizeFn,
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

    // Skip if in failure cooldown.
    if (this.isInCooldown()) {
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
   */
  isSummaryFresh(conversationId: number, currentTokens: number): boolean {
    if (!this.config.enabled) {
      return false;
    }

    const cached = this.cache.get(conversationId);
    if (!cached) {
      return false;
    }

    if (cached.hasMixedContext) {
      return false;
    }

    const age = Date.now() - cached.updatedAt;
    if (age > this.config.maxStalenessMs) {
      return false;
    }

    if (currentTokens > cached.sourceTokenCount * TOKEN_DRIFT_FACTOR) {
      return false;
    }

    return true;
  }

  /** Force an immediate background update for a conversation. */
  triggerUpdate(conversationId: number): void {
    if (this.disposed || !this.config.enabled || this.isInCooldown()) {
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
    this.retriggerNeeded.clear();
  }

  // ── Private ────────────────────────────────────────────────────────────────

  private isInCooldown(): boolean {
    if (this.cooldownUntil === 0) {
      return false;
    }
    if (Date.now() >= this.cooldownUntil) {
      // Cooldown expired — reset.
      this.cooldownUntil = 0;
      this.consecutiveFailures = 0;
      log.info(`[compression-observer] cooldown expired, resuming`);
      return false;
    }
    return true;
  }

  private enqueueUpdate(conversationId: number): void {
    if (this.pendingUpdates.has(conversationId)) {
      this.retriggerNeeded.add(conversationId);
      return;
    }

    const updatePromise = this.runUpdate(conversationId).finally(() => {
      this.pendingUpdates.delete(conversationId);

      if (this.retriggerNeeded.has(conversationId)) {
        this.retriggerNeeded.delete(conversationId);
        if (!this.disposed && !this.isInCooldown()) {
          this.enqueueUpdate(conversationId);
        }
      }
    });
    updatePromise.catch(() => {});
    this.pendingUpdates.set(conversationId, updatePromise);
  }

  /**
   * Call the summarizer with retry for transient failures.
   * Local models (ollama) may need a moment to load or recover.
   */
  private async summarizeWithRetry(text: string, aggressive: boolean): Promise<string> {
    let lastErr: unknown;
    for (let attempt = 0; attempt <= MAX_CALL_RETRIES; attempt++) {
      try {
        return await this.summarize(text, aggressive);
      } catch (err) {
        lastErr = err;
        if (attempt < MAX_CALL_RETRIES) {
          const delay = RETRY_DELAY_MS * (attempt + 1);
          log.info(
            `[compression-observer] summarize attempt ${attempt + 1} failed, retrying in ${delay}ms: ${
              err instanceof Error ? err.message : String(err)
            }`,
          );
          await new Promise((resolve) => setTimeout(resolve, delay));
          if (this.disposed) {
            throw lastErr;
          }
        }
      }
    }
    throw lastErr;
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

      let hasMixedContext = false;

      const allRawItems: Array<{
        messageId: number;
        content: string;
        tokenCount: number;
      }> = [];

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
        allRawItems.push({
          messageId: message.messageId,
          content: message.content,
          tokenCount: message.tokenCount > 0 ? message.tokenCount : estimateTokens(message.content),
        });
      }

      // Exclude the fresh tail — only summarize what the fast path will replace.
      const tailStart = Math.max(0, allRawItems.length - OBSERVER_FRESH_TAIL_COUNT);
      const compactableItems = allRawItems.slice(0, tailStart);

      if (compactableItems.length === 0) {
        return;
      }

      let totalSourceTokens = 0;
      const messageTexts: string[] = [];
      for (const item of compactableItems) {
        messageTexts.push(item.content);
        totalSourceTokens += item.tokenCount;
      }

      if (totalSourceTokens === 0) {
        return;
      }

      const sourceText = messageTexts.join("\n\n---\n\n");
      const isAggressive = this.config.targetRatio <= 0.15;

      const summary = await this.summarizeWithRetry(sourceText, isAggressive);

      if (this.disposed) {
        return;
      }

      const summaryTokens = estimateTokens(summary);

      if (summaryTokens >= totalSourceTokens) {
        log.warn(
          `[compression-observer] rejecting summary for conversation=${conversationId}: ` +
            `summaryTokens=${summaryTokens} >= sourceTokens=${totalSourceTokens}`,
        );
        return;
      }

      // Success — reset failure counter.
      this.consecutiveFailures = 0;

      this.cache.set(conversationId, {
        summary,
        tokenCount: summaryTokens,
        sourceTokenCount: totalSourceTokens,
        messagesCovered: compactableItems.length,
        updatedAt: Date.now(),
        hasMixedContext,
      });

      log.info(
        `[compression-observer] updated cache for conversation=${conversationId} ` +
          `sourceTokens=${totalSourceTokens} summaryTokens=${summaryTokens} ` +
          `ratio=${(summaryTokens / totalSourceTokens).toFixed(3)} ` +
          `messages=${compactableItems.length} tailExcluded=${allRawItems.length - compactableItems.length} ` +
          `hasMixedContext=${hasMixedContext}`,
      );
    } catch (err) {
      this.consecutiveFailures++;

      if (this.consecutiveFailures >= MAX_CONSECUTIVE_FAILURES) {
        this.cooldownUntil = Date.now() + FAILURE_COOLDOWN_MS;
        log.warn(
          `[compression-observer] ${this.consecutiveFailures} consecutive failures, ` +
            `entering ${FAILURE_COOLDOWN_MS / 1000}s cooldown: ${
              err instanceof Error ? err.message : String(err)
            }`,
        );
      } else {
        log.warn(
          `[compression-observer] update failed for conversation=${conversationId} ` +
            `(${this.consecutiveFailures}/${MAX_CONSECUTIVE_FAILURES} before cooldown): ${
              err instanceof Error ? err.message : String(err)
            }`,
        );
      }
    }
  }
}
