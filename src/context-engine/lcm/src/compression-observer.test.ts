import { beforeEach, describe, expect, it, vi } from "vitest";
import type { CompactionSummarizeFn } from "./compaction.js";
import {
  CompressionObserver,
  DEFAULT_OBSERVER_CONFIG,
  type CompressionObserverConfig,
} from "./compression-observer.js";

// ── Mock stores ─────────────────────────────────────────────────────────────

function createMockConversationStore() {
  const messages = new Map<number, { messageId: number; content: string; tokenCount: number }>();
  let nextId = 1;

  return {
    addMessage(content: string, tokenCount?: number) {
      const id = nextId++;
      const tc = tokenCount ?? Math.ceil(content.length / 4);
      messages.set(id, { messageId: id, content, tokenCount: tc });
      return id;
    },
    getMessageById: vi.fn(async (messageId: number) => {
      return messages.get(messageId) ?? null;
    }),
    _messages: messages,
  };
}

function createMockSummaryStore(
  _conversationStore: ReturnType<typeof createMockConversationStore>,
) {
  const contextItems: Array<{
    conversationId: number;
    ordinal: number;
    itemType: "message" | "summary";
    messageId: number | null;
    summaryId: string | null;
    createdAt: Date;
  }> = [];
  let ordinalCounter = 0;

  return {
    addContextMessage(conversationId: number, messageId: number) {
      contextItems.push({
        conversationId,
        ordinal: ordinalCounter++,
        itemType: "message",
        messageId,
        summaryId: null,
        createdAt: new Date(),
      });
    },
    getContextItems: vi.fn(async (conversationId: number) => {
      return contextItems.filter((item) => item.conversationId === conversationId);
    }),
    _contextItems: contextItems,
  };
}

function createMockSummarize(): CompactionSummarizeFn {
  return vi.fn(async (text: string) => {
    // Simple mock: return a short summary that is always shorter than input.
    // Uses ~10% of source to ensure the sanity check (summary < source) passes.
    const targetLen = Math.max(4, Math.floor(text.length * 0.1));
    return text.slice(0, targetLen);
  });
}

/**
 * Add enough messages to a conversation so that some are outside the fresh tail.
 * The observer's OBSERVER_FRESH_TAIL_COUNT is 8, so we need >8 messages for
 * any to be compactable (outside the tail).
 */
function addMessagesForCompaction(
  conversationStore: ReturnType<typeof createMockConversationStore>,
  summaryStore: ReturnType<typeof createMockSummaryStore>,
  conversationId: number,
  count: number,
) {
  const ids: number[] = [];
  for (let i = 0; i < count; i++) {
    const msgId = conversationStore.addMessage(`${"X".repeat(200)} message-${i}`);
    summaryStore.addContextMessage(conversationId, msgId);
    ids.push(msgId);
  }
  return ids;
}

// ── Tests ────────────────────────────────────────────────────────────────────

describe("CompressionObserver", () => {
  let conversationStore: ReturnType<typeof createMockConversationStore>;
  let summaryStore: ReturnType<typeof createMockSummaryStore>;
  let mockSummarize: CompactionSummarizeFn;
  let config: CompressionObserverConfig;

  beforeEach(() => {
    conversationStore = createMockConversationStore();
    summaryStore = createMockSummaryStore(conversationStore);
    mockSummarize = createMockSummarize();
    config = {
      ...DEFAULT_OBSERVER_CONFIG,
      enabled: true,
      messageInterval: 3,
      maxStalenessMs: 60_000,
    };
  });

  function createObserver(overrides?: Partial<CompressionObserverConfig>) {
    return new CompressionObserver(
      { ...config, ...overrides },
      conversationStore as never,
      summaryStore as never,
      async () => mockSummarize,
    );
  }

  describe("message counting and trigger", () => {
    it("should not trigger update before reaching messageInterval", () => {
      const observer = createObserver();
      const conversationId = 1;

      observer.onMessage(conversationId);
      observer.onMessage(conversationId);
      // Only 2 messages, interval is 3 — no update yet
      expect(observer.getCachedSummary(conversationId)).toBeNull();
    });

    it("should trigger background update at messageInterval", async () => {
      const conversationId = 1;
      // Need >8 messages so some are outside the fresh tail (OBSERVER_FRESH_TAIL_COUNT = 8)
      addMessagesForCompaction(conversationStore, summaryStore, conversationId, 12);

      const observer = createObserver();

      observer.onMessage(conversationId);
      observer.onMessage(conversationId);
      observer.onMessage(conversationId); // triggers at 3

      await vi.waitFor(
        () => {
          expect(observer.getCachedSummary(conversationId)).not.toBeNull();
        },
        { timeout: 2000 },
      );

      const cached = observer.getCachedSummary(conversationId);
      expect(cached).toBeDefined();
      expect(cached!.summary.length).toBeGreaterThan(0);
      // messagesCovered should be 4 (12 total - 8 fresh tail)
      expect(cached!.messagesCovered).toBe(4);
      expect(cached!.tokenCount).toBeGreaterThan(0);
      expect(cached!.sourceTokenCount).toBeGreaterThan(0);
      expect(cached!.hasMixedContext).toBe(false);
    });

    it("should not produce cache when all messages are in fresh tail", async () => {
      const conversationId = 1;
      // Only 5 messages — all within fresh tail (OBSERVER_FRESH_TAIL_COUNT = 8)
      addMessagesForCompaction(conversationStore, summaryStore, conversationId, 5);

      const observer = createObserver();
      observer.triggerUpdate(conversationId);

      // Wait for background attempt
      await new Promise((resolve) => setTimeout(resolve, 200));

      // No compactable messages outside tail — no cache
      expect(observer.getCachedSummary(conversationId)).toBeNull();
    });

    it("should reset counter after triggering", async () => {
      const conversationId = 1;
      addMessagesForCompaction(conversationStore, summaryStore, conversationId, 12);

      const observer = createObserver();

      // First trigger
      observer.onMessage(conversationId);
      observer.onMessage(conversationId);
      observer.onMessage(conversationId);

      await vi.waitFor(
        () => {
          expect(observer.getCachedSummary(conversationId)).not.toBeNull();
        },
        { timeout: 2000 },
      );

      // Counter should be reset — next 2 messages shouldn't trigger
      observer.invalidate(conversationId);
      observer.onMessage(conversationId);
      observer.onMessage(conversationId);
      expect(observer.getCachedSummary(conversationId)).toBeNull();
    });
  });

  describe("cache freshness", () => {
    it("should report fresh for recent summary with matching tokens", async () => {
      const conversationId = 1;
      addMessagesForCompaction(conversationStore, summaryStore, conversationId, 12);

      const observer = createObserver();
      observer.triggerUpdate(conversationId);

      await vi.waitFor(
        () => {
          expect(observer.getCachedSummary(conversationId)).not.toBeNull();
        },
        { timeout: 2000 },
      );

      const cached = observer.getCachedSummary(conversationId)!;
      expect(observer.isSummaryFresh(conversationId, cached.sourceTokenCount)).toBe(true);
    });

    it("should report stale when tokens have drifted significantly", async () => {
      const conversationId = 1;
      addMessagesForCompaction(conversationStore, summaryStore, conversationId, 12);

      const observer = createObserver();
      observer.triggerUpdate(conversationId);

      await vi.waitFor(
        () => {
          expect(observer.getCachedSummary(conversationId)).not.toBeNull();
        },
        { timeout: 2000 },
      );

      const cached = observer.getCachedSummary(conversationId)!;
      // Tokens grown 2x — should be stale (drift factor 1.3)
      expect(observer.isSummaryFresh(conversationId, cached.sourceTokenCount * 2)).toBe(false);
    });

    it("should report stale when no cached summary exists", () => {
      const observer = createObserver();
      expect(observer.isSummaryFresh(999, 1000)).toBe(false);
    });
  });

  describe("invalidation", () => {
    it("should clear cached summary on invalidate", async () => {
      const conversationId = 1;
      addMessagesForCompaction(conversationStore, summaryStore, conversationId, 12);

      const observer = createObserver();
      observer.triggerUpdate(conversationId);

      await vi.waitFor(
        () => {
          expect(observer.getCachedSummary(conversationId)).not.toBeNull();
        },
        { timeout: 2000 },
      );

      observer.invalidate(conversationId);
      expect(observer.getCachedSummary(conversationId)).toBeNull();
    });
  });

  describe("multiple conversations", () => {
    it("should maintain separate caches per conversation", async () => {
      const conv1 = 1;
      const conv2 = 2;

      addMessagesForCompaction(conversationStore, summaryStore, conv1, 12);
      addMessagesForCompaction(conversationStore, summaryStore, conv2, 12);

      const observer = createObserver();

      // Trigger conv1 first
      observer.triggerUpdate(conv1);

      await vi.waitFor(
        () => {
          expect(observer.getCachedSummary(conv1)).not.toBeNull();
        },
        { timeout: 2000 },
      );

      // Then trigger conv2
      observer.triggerUpdate(conv2);

      await vi.waitFor(
        () => {
          expect(observer.getCachedSummary(conv2)).not.toBeNull();
        },
        { timeout: 2000 },
      );

      // Invalidating one should not affect the other
      observer.invalidate(conv1);
      expect(observer.getCachedSummary(conv1)).toBeNull();
      expect(observer.getCachedSummary(conv2)).not.toBeNull();
    });
  });

  describe("disabled observer", () => {
    it("should not track messages when disabled", () => {
      const observer = createObserver({ enabled: false });
      observer.onMessage(1);
      observer.onMessage(1);
      observer.onMessage(1);
      expect(observer.getCachedSummary(1)).toBeNull();
    });

    it("should always return null from getCachedSummary when disabled", () => {
      const observer = createObserver({ enabled: false });
      expect(observer.getCachedSummary(1)).toBeNull();
    });
  });

  describe("dispose", () => {
    it("should clear all state on dispose", async () => {
      const conversationId = 1;
      addMessagesForCompaction(conversationStore, summaryStore, conversationId, 12);

      const observer = createObserver();
      observer.triggerUpdate(conversationId);

      await vi.waitFor(
        () => {
          expect(observer.getCachedSummary(conversationId)).not.toBeNull();
        },
        { timeout: 2000 },
      );

      observer.dispose();

      expect(observer.getCachedSummary(conversationId)).toBeNull();
      // No new updates should be enqueued after dispose
      observer.onMessage(conversationId);
      observer.onMessage(conversationId);
      observer.onMessage(conversationId);
      expect(observer.getCachedSummary(conversationId)).toBeNull();
    });
  });

  describe("triggerUpdate", () => {
    it("should force an immediate background update", async () => {
      const conversationId = 1;
      addMessagesForCompaction(conversationStore, summaryStore, conversationId, 12);

      const observer = createObserver();

      // Trigger directly without reaching messageInterval
      observer.triggerUpdate(conversationId);

      await vi.waitFor(
        () => {
          expect(observer.getCachedSummary(conversationId)).not.toBeNull();
        },
        { timeout: 2000 },
      );

      // Should cover messages outside fresh tail (12 - 8 = 4)
      expect(observer.getCachedSummary(conversationId)!.messagesCovered).toBe(4);
    });
  });

  describe("error handling", () => {
    it("should handle summarization failure gracefully", async () => {
      const conversationId = 1;
      addMessagesForCompaction(conversationStore, summaryStore, conversationId, 12);

      const failingSummarize = vi.fn(async () => {
        throw new Error("Summarization failed");
      }) as unknown as CompactionSummarizeFn;

      const observer = new CompressionObserver(
        config,
        conversationStore as never,
        summaryStore as never,
        async () => failingSummarize,
      );

      observer.onMessage(conversationId);
      observer.onMessage(conversationId);
      observer.onMessage(conversationId);

      // Wait a bit for the background update to attempt and fail
      await new Promise((resolve) => setTimeout(resolve, 200));

      // Should not crash, just no cached summary
      expect(observer.getCachedSummary(conversationId)).toBeNull();
    });

    it("should handle empty conversation gracefully", async () => {
      const observer = createObserver();

      // Trigger for non-existent conversation
      observer.triggerUpdate(999);

      // Wait for attempt to complete
      await new Promise((resolve) => setTimeout(resolve, 200));

      expect(observer.getCachedSummary(999)).toBeNull();
    });

    it("should reject summary when it's larger than source", async () => {
      const conversationId = 1;
      addMessagesForCompaction(conversationStore, summaryStore, conversationId, 12);

      // Mock summarizer that produces verbose output
      const verboseSummarize = vi.fn(async (text: string) => {
        return "A".repeat(text.length * 2); // Much larger than source
      }) as unknown as CompactionSummarizeFn;

      const observer = new CompressionObserver(
        config,
        conversationStore as never,
        summaryStore as never,
        async () => verboseSummarize,
      );

      observer.triggerUpdate(conversationId);

      // Wait for the background update to attempt
      await new Promise((resolve) => setTimeout(resolve, 200));

      // Should reject the summary because it's larger than source
      expect(observer.getCachedSummary(conversationId)).toBeNull();
    });

    it("should permanently disable when summarizer resolution returns null", async () => {
      const conversationId = 1;
      addMessagesForCompaction(conversationStore, summaryStore, conversationId, 12);

      const observer = new CompressionObserver(
        config,
        conversationStore as never,
        summaryStore as never,
        async () => null, // Simulate failed resolution
      );

      observer.onMessage(conversationId);
      observer.onMessage(conversationId);
      observer.onMessage(conversationId);

      await new Promise((resolve) => setTimeout(resolve, 200));

      // Should not have cached anything
      expect(observer.getCachedSummary(conversationId)).toBeNull();

      // Further messages should be ignored (summarizerFailed = true)
      // Even after enough messages, no update should be enqueued
      for (let i = 0; i < 10; i++) {
        observer.onMessage(conversationId);
      }
      await new Promise((resolve) => setTimeout(resolve, 100));
      expect(observer.getCachedSummary(conversationId)).toBeNull();
    });
  });

  describe("retrigger after in-flight update", () => {
    it("should process retrigger after current update completes", async () => {
      const conversationId = 1;
      addMessagesForCompaction(conversationStore, summaryStore, conversationId, 12);

      let callCount = 0;
      const slowSummarize = vi.fn(async (text: string) => {
        callCount++;
        // First call is slow
        if (callCount === 1) {
          await new Promise((resolve) => setTimeout(resolve, 100));
        }
        const targetLen = Math.max(4, Math.floor(text.length * 0.1));
        return text.slice(0, targetLen);
      }) as unknown as CompactionSummarizeFn;

      const observer = new CompressionObserver(
        { ...config, messageInterval: 1 },
        conversationStore as never,
        summaryStore as never,
        async () => slowSummarize,
      );

      // First message triggers update (messageInterval = 1)
      observer.onMessage(conversationId);

      // Second message arrives while first is in-flight — should be re-triggered
      await new Promise((resolve) => setTimeout(resolve, 30));
      observer.onMessage(conversationId);

      // Wait for both updates to complete
      await vi.waitFor(
        () => {
          expect(callCount).toBeGreaterThanOrEqual(2);
        },
        { timeout: 2000 },
      );

      expect(observer.getCachedSummary(conversationId)).not.toBeNull();
    });
  });

  describe("mixed context handling", () => {
    it("should flag hasMixedContext when summaries exist in context", async () => {
      const conversationId = 1;
      // Add enough messages so some are outside fresh tail
      addMessagesForCompaction(conversationStore, summaryStore, conversationId, 12);

      // Add a summary item to the context (simulating prior compaction)
      summaryStore._contextItems.push({
        conversationId,
        ordinal: 1000,
        itemType: "summary",
        messageId: null,
        summaryId: "sum_existing",
        createdAt: new Date(),
      });

      const observer = createObserver();
      observer.triggerUpdate(conversationId);

      await vi.waitFor(
        () => {
          expect(observer.getCachedSummary(conversationId)).not.toBeNull();
        },
        { timeout: 2000 },
      );

      const cached = observer.getCachedSummary(conversationId)!;
      expect(cached.hasMixedContext).toBe(true);
    });

    it("isSummaryFresh should return false for mixed context summaries", async () => {
      const conversationId = 1;
      addMessagesForCompaction(conversationStore, summaryStore, conversationId, 12);

      // Add a summary item
      summaryStore._contextItems.push({
        conversationId,
        ordinal: 500,
        itemType: "summary",
        messageId: null,
        summaryId: "sum_prior",
        createdAt: new Date(),
      });

      const observer = createObserver();
      observer.triggerUpdate(conversationId);

      await vi.waitFor(
        () => {
          expect(observer.getCachedSummary(conversationId)).not.toBeNull();
        },
        { timeout: 2000 },
      );

      const cached = observer.getCachedSummary(conversationId)!;
      // Even though the summary is fresh in time and tokens, it should
      // be rejected because the context has mixed content.
      expect(observer.isSummaryFresh(conversationId, cached.sourceTokenCount)).toBe(false);
    });
  });
});
