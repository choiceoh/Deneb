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

      // Add messages to the stores
      const msgId1 = conversationStore.addMessage("Hello, how are you?");
      const msgId2 = conversationStore.addMessage("I am fine, thank you.");
      const msgId3 = conversationStore.addMessage("What are we working on today?");
      summaryStore.addContextMessage(conversationId, msgId1);
      summaryStore.addContextMessage(conversationId, msgId2);
      summaryStore.addContextMessage(conversationId, msgId3);

      const observer = createObserver();

      observer.onMessage(conversationId);
      observer.onMessage(conversationId);
      observer.onMessage(conversationId); // triggers at 3

      // Wait for the background update to complete
      await vi.waitFor(
        () => {
          expect(observer.getCachedSummary(conversationId)).not.toBeNull();
        },
        { timeout: 2000 },
      );

      const cached = observer.getCachedSummary(conversationId);
      expect(cached).toBeDefined();
      expect(cached!.summary.length).toBeGreaterThan(0);
      expect(cached!.messagesCovered).toBe(3);
      expect(cached!.tokenCount).toBeGreaterThan(0);
      expect(cached!.sourceTokenCount).toBeGreaterThan(0);
      expect(cached!.hasMixedContext).toBe(false);
    });

    it("should reset counter after triggering", async () => {
      const conversationId = 1;
      const msgId = conversationStore.addMessage("A".repeat(200));
      summaryStore.addContextMessage(conversationId, msgId);

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
      const msgId = conversationStore.addMessage("A".repeat(400)); // ~100 tokens
      summaryStore.addContextMessage(conversationId, msgId);

      const observer = createObserver();

      // Trigger update
      observer.onMessage(conversationId);
      observer.onMessage(conversationId);
      observer.onMessage(conversationId);

      await vi.waitFor(
        () => {
          expect(observer.getCachedSummary(conversationId)).not.toBeNull();
        },
        { timeout: 2000 },
      );

      const cached = observer.getCachedSummary(conversationId)!;
      // Token count close to source — should be fresh
      expect(observer.isSummaryFresh(conversationId, cached.sourceTokenCount)).toBe(true);
    });

    it("should report stale when tokens have drifted significantly", async () => {
      const conversationId = 1;
      const msgId = conversationStore.addMessage("A".repeat(400)); // ~100 tokens
      summaryStore.addContextMessage(conversationId, msgId);

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
      const msgId = conversationStore.addMessage("A".repeat(200));
      summaryStore.addContextMessage(conversationId, msgId);

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
      expect(observer.getCachedSummary(conversationId)).toBeNull();
    });
  });

  describe("multiple conversations", () => {
    it("should maintain separate caches per conversation", async () => {
      const conv1 = 1;
      const conv2 = 2;

      const msgId1 = conversationStore.addMessage("A".repeat(200) + " conversation 1");
      const msgId2 = conversationStore.addMessage("B".repeat(200) + " conversation 2");
      summaryStore.addContextMessage(conv1, msgId1);
      summaryStore.addContextMessage(conv2, msgId2);

      const observer = createObserver();

      // Trigger both
      for (let i = 0; i < 3; i++) {
        observer.onMessage(conv1);
        observer.onMessage(conv2);
      }

      await vi.waitFor(
        () => {
          expect(observer.getCachedSummary(conv1)).not.toBeNull();
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
      const msgId = conversationStore.addMessage("A".repeat(200));
      summaryStore.addContextMessage(conversationId, msgId);

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
      const msgId = conversationStore.addMessage("A".repeat(200) + " trigger update content");
      summaryStore.addContextMessage(conversationId, msgId);

      const observer = createObserver();

      // Trigger directly without reaching messageInterval
      observer.triggerUpdate(conversationId);

      await vi.waitFor(
        () => {
          expect(observer.getCachedSummary(conversationId)).not.toBeNull();
        },
        { timeout: 2000 },
      );

      expect(observer.getCachedSummary(conversationId)!.messagesCovered).toBe(1);
    });
  });

  describe("error handling", () => {
    it("should handle summarization failure gracefully", async () => {
      const conversationId = 1;
      const msgId = conversationStore.addMessage("A".repeat(200));
      summaryStore.addContextMessage(conversationId, msgId);

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
      // Add a tiny message (few tokens)
      const msgId = conversationStore.addMessage("Hi");
      summaryStore.addContextMessage(conversationId, msgId);

      // Mock summarizer that produces verbose output
      const verboseSummarize = vi.fn(async () => {
        return "A".repeat(10_000); // Much larger than source
      }) as unknown as CompactionSummarizeFn;

      const observer = new CompressionObserver(
        config,
        conversationStore as never,
        summaryStore as never,
        async () => verboseSummarize,
      );

      observer.onMessage(conversationId);
      observer.onMessage(conversationId);
      observer.onMessage(conversationId);

      // Wait for the background update to attempt
      await new Promise((resolve) => setTimeout(resolve, 200));

      // Should reject the summary because it's larger than source
      expect(observer.getCachedSummary(conversationId)).toBeNull();
    });
  });

  describe("mixed context handling", () => {
    it("should flag hasMixedContext when summaries exist in context", async () => {
      const conversationId = 1;
      const msgId = conversationStore.addMessage("A".repeat(200) + " message content");
      summaryStore.addContextMessage(conversationId, msgId);

      // Add a summary item to the context (simulating prior compaction)
      summaryStore._contextItems.push({
        conversationId,
        ordinal: 100,
        itemType: "summary",
        messageId: null,
        summaryId: "sum_existing",
        createdAt: new Date(),
      });

      // Add another raw message after the summary
      const msgId2 = conversationStore.addMessage("B".repeat(200) + " another message");
      summaryStore.addContextMessage(conversationId, msgId2);

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
      // messagesCovered should only count raw messages, not summaries
      expect(cached.messagesCovered).toBe(2);
    });

    it("isSummaryFresh should return false for mixed context summaries", async () => {
      const conversationId = 1;
      const msgId = conversationStore.addMessage("A".repeat(200) + " some content");
      summaryStore.addContextMessage(conversationId, msgId);

      // Add a summary item
      summaryStore._contextItems.push({
        conversationId,
        ordinal: 50,
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
