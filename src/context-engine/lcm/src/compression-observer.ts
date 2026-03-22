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
  /**
   * Number of recent raw messages to exclude from summarization.
   * Must match the compaction engine's freshTailCount so the observer
   * summarizes exactly the same messages that the fast path will replace.
   * Passed from CompactionConfig at construction time.
   */
  freshTailCount: number;
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
  freshTailCount: 32,
};

/** Token drift factor — if current tokens exceed source by this factor, summary is stale. */
const TOKEN_DRIFT_FACTOR = 1.3;

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
  private retriggerNeeded = new Set<number>();
  private disposed = false;

  private consecutiveFailures = new Map<number, number>();
  private cooldownUntil = new Map<number, number>();

  constructor(
    private config: CompressionObserverConfig,
    private conversationStore: ConversationStore,
    private summaryStore: SummaryStore,
    private summarize: CompactionSummarizeFn,
  ) {}

  onMessage(conversationId: number): void {
    if (this.disposed || !this.config.enabled || this.isInCooldown(conversationId)) {
      return;
    }

    const count = (this.messageCounters.get(conversationId) ?? 0) + 1;
    this.messageCounters.set(conversationId, count);

    if (count >= this.config.messageInterval) {
      this.messageCounters.set(conversationId, 0);
      this.enqueueUpdate(conversationId);
    }
  }

  getCachedSummary(conversationId: number): CachedSummary | null {
    if (!this.config.enabled) {
      return null;
    }
    return this.cache.get(conversationId) ?? null;
  }

  isSummaryFresh(conversationId: number, currentTokens: number): boolean {
    if (!this.config.enabled) {
      return false;
    }
    const cached = this.cache.get(conversationId);
    if (!cached || cached.hasMixedContext) {
      return false;
    }
    if (Date.now() - cached.updatedAt > this.config.maxStalenessMs) {
      return false;
    }
    if (currentTokens > cached.sourceTokenCount * TOKEN_DRIFT_FACTOR) {
      return false;
    }
    return true;
  }

  triggerUpdate(conversationId: number): void {
    if (this.disposed || !this.config.enabled || this.isInCooldown(conversationId)) {
      return;
    }
    this.messageCounters.set(conversationId, 0);
    this.enqueueUpdate(conversationId);
  }

  invalidate(conversationId: number): void {
    this.cache.delete(conversationId);
    this.messageCounters.set(conversationId, 0);
    this.retriggerNeeded.delete(conversationId);
  }

  dispose(): void {
    this.disposed = true;
    this.cache.clear();
    this.messageCounters.clear();
    this.pendingUpdates.clear();
    this.retriggerNeeded.clear();
    this.consecutiveFailures.clear();
    this.cooldownUntil.clear();
  }

  // ── Private ────────────────────────────────────────────────────────────────

  private isInCooldown(conversationId: number): boolean {
    const until = this.cooldownUntil.get(conversationId) ?? 0;
    if (until === 0) {
      return false;
    }
    if (Date.now() >= until) {
      this.cooldownUntil.delete(conversationId);
      this.consecutiveFailures.delete(conversationId);
      log.info(
        `[compression-observer] cooldown expired for conversation=${conversationId}, resuming`,
      );
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
        if (!this.disposed && !this.isInCooldown(conversationId)) {
          this.enqueueUpdate(conversationId);
        }
      }
    });
    updatePromise.catch(() => {});
    this.pendingUpdates.set(conversationId, updatePromise);
  }

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

      // Exclude the fresh tail using the same count as the compaction engine.
      const freshTailCount = Math.max(0, this.config.freshTailCount);
      const tailStart = Math.max(0, allRawItems.length - freshTailCount);
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

      this.consecutiveFailures.set(conversationId, 0);

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
          `messages=${compactableItems.length} freshTailCount=${freshTailCount} ` +
          `hasMixedContext=${hasMixedContext}`,
      );
    } catch (err) {
      const failures = (this.consecutiveFailures.get(conversationId) ?? 0) + 1;
      this.consecutiveFailures.set(conversationId, failures);
      if (failures >= MAX_CONSECUTIVE_FAILURES) {
        this.cooldownUntil.set(conversationId, Date.now() + FAILURE_COOLDOWN_MS);
        log.warn(
          `[compression-observer] ${failures} consecutive failures for conversation=${conversationId}, ` +
            `entering ${FAILURE_COOLDOWN_MS / 1000}s cooldown: ${
              err instanceof Error ? err.message : String(err)
            }`,
        );
      } else {
        log.warn(
          `[compression-observer] update failed for conversation=${conversationId} ` +
            `(${failures}/${MAX_CONSECUTIVE_FAILURES} before cooldown): ${
              err instanceof Error ? err.message : String(err)
            }`,
        );
      }
    }
  }
}
