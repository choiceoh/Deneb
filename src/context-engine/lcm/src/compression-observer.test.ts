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
    const targetLen = Math.max(4, Math.floor(text.length * 0.1));
    return text.slice(0, targetLen);
  });
}

/**
 * Add enough messages so some are outside the fresh tail (OBSERVER_FRESH_TAIL_COUNT = 8).
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
      mockSummarize,
    );
  }

  describe("message counting and trigger", () => {
    it("should not trigger update before reaching messageInterval", () => {
      const observer = createObserver();
      const conversationId = 1;

      observer.onMessage(conversationId);
      observer.onMessage(conversationId);
      expect(observer.getCachedSummary(conversationId)).toBeNull();
    });

    it("should trigger background update at messageInterval", async () => {
      const conversationId = 1;
      addMessagesForCompaction(conversationStore, summaryStore, conversationId, 12);

      const observer = createObserver();

      observer.onMessage(conversationId);
      observer.onMessage(conversationId);
      observer.onMessage(conversationId);

      await vi.waitFor(
        () => {
          expect(observer.getCachedSummary(conversationId)).not.toBeNull();
        },
        { timeout: 2000 },
      );

      const cached = observer.getCachedSummary(conversationId);
      expect(cached).toBeDefined();
      expect(cached!.summary.length).toBeGreaterThan(0);
      expect(cached!.messagesCovered).toBe(4); // 12 - 8 tail
      expect(cached!.tokenCount).toBeGreaterThan(0);
      expect(cached!.sourceTokenCount).toBeGreaterThan(0);
      expect(cached!.hasMixedContext).toBe(false);
    });

    it("should not produce cache when all messages are in fresh tail", async () => {
      const conversationId = 1;
      addMessagesForCompaction(conversationStore, summaryStore, conversationId, 5);

      const observer = createObserver();
      observer.triggerUpdate(conversationId);

      await new Promise((resolve) => setTimeout(resolve, 200));

      expect(observer.getCachedSummary(conversationId)).toBeNull();
    });

    it("should reset counter after triggering", async () => {
      const conversationId = 1;
      addMessagesForCompaction(conversationStore, summaryStore, conversationId, 12);

      const observer = createObserver();

      observer.onMessage(conversationId);
      observer.onMessage(conversationId);
      observer.onMessage(conversationId);

      await vi.waitFor(
        () => {
          expect(observer.getCachedSummary(conversationId)).not.toBeNull();
        },
        { timeout: 2000 },
      );

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

      observer.triggerUpdate(conv1);
      await vi.waitFor(
        () => {
          expect(observer.getCachedSummary(conv1)).not.toBeNull();
        },
        { timeout: 2000 },
      );

      observer.triggerUpdate(conv2);
      await vi.waitFor(
        () => {
          expect(observer.getCachedSummary(conv2)).not.toBeNull();
        },
        { timeout: 2000 },
      );

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
      observer.triggerUpdate(conversationId);

      await vi.waitFor(
        () => {
          expect(observer.getCachedSummary(conversationId)).not.toBeNull();
        },
        { timeout: 2000 },
      );

      expect(observer.getCachedSummary(conversationId)!.messagesCovered).toBe(4);
    });
  });

  describe("error handling and retry", () => {
    it("should retry transient LLM failures", async () => {
      const conversationId = 1;
      addMessagesForCompaction(conversationStore, summaryStore, conversationId, 12);

      let callCount = 0;
      const flakyFn = vi.fn(async (text: string) => {
        callCount++;
        if (callCount <= 1) {
          throw new Error("Connection refused");
        }
        const targetLen = Math.max(4, Math.floor(text.length * 0.1));
        return text.slice(0, targetLen);
      }) as unknown as CompactionSummarizeFn;

      const observer = new CompressionObserver(
        config,
        conversationStore as never,
        summaryStore as never,
        flakyFn,
      );

      observer.triggerUpdate(conversationId);

      await vi.waitFor(
        () => {
          expect(observer.getCachedSummary(conversationId)).not.toBeNull();
        },
        { timeout: 10000 },
      );

      // First call failed, retry succeeded
      expect(callCount).toBeGreaterThanOrEqual(2);
    });

    it("should not crash on repeated failures", async () => {
      const conversationId = 1;
      addMessagesForCompaction(conversationStore, summaryStore, conversationId, 12);

      const alwaysFail = vi.fn(async () => {
        throw new Error("Model unavailable");
      }) as unknown as CompactionSummarizeFn;

      const observer = new CompressionObserver(
        config,
        conversationStore as never,
        summaryStore as never,
        alwaysFail,
      );

      // Trigger an update that will fail (after retries)
      observer.triggerUpdate(conversationId);

      // Wait for retries to exhaust (MAX_CALL_RETRIES=2, delays=2s+4s)
      await new Promise((resolve) => setTimeout(resolve, 8000));

      // Should have tried (1 initial + 2 retries = 3 calls)
      expect(alwaysFail.mock.calls.length).toBeGreaterThanOrEqual(3);

      // No cached summary
      expect(observer.getCachedSummary(conversationId)).toBeNull();

      // Observer should still be operational (not crashed)
      observer.dispose();
    }, 15000);

    it("should handle empty conversation gracefully", async () => {
      const observer = createObserver();
      observer.triggerUpdate(999);
      await new Promise((resolve) => setTimeout(resolve, 200));
      expect(observer.getCachedSummary(999)).toBeNull();
    });

    it("should reject summary when it's larger than source", async () => {
      const conversationId = 1;
      addMessagesForCompaction(conversationStore, summaryStore, conversationId, 12);

      const verboseFn = vi.fn(async (text: string) => {
        return "A".repeat(text.length * 2);
      }) as unknown as CompactionSummarizeFn;

      const observer = new CompressionObserver(
        config,
        conversationStore as never,
        summaryStore as never,
        verboseFn,
      );

      observer.triggerUpdate(conversationId);
      await new Promise((resolve) => setTimeout(resolve, 200));
      expect(observer.getCachedSummary(conversationId)).toBeNull();
    });
  });

  describe("retrigger after in-flight update", () => {
    it("should process retrigger after current update completes", async () => {
      const conversationId = 1;
      addMessagesForCompaction(conversationStore, summaryStore, conversationId, 12);

      let callCount = 0;
      const slowFn = vi.fn(async (text: string) => {
        callCount++;
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
        slowFn,
      );

      observer.onMessage(conversationId);
      await new Promise((resolve) => setTimeout(resolve, 30));
      observer.onMessage(conversationId);

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
      addMessagesForCompaction(conversationStore, summaryStore, conversationId, 12);

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

      expect(observer.getCachedSummary(conversationId)!.hasMixedContext).toBe(true);
    });

    it("isSummaryFresh should return false for mixed context summaries", async () => {
      const conversationId = 1;
      addMessagesForCompaction(conversationStore, summaryStore, conversationId, 12);

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
      expect(observer.isSummaryFresh(conversationId, cached.sourceTokenCount)).toBe(false);
    });
  });
});
