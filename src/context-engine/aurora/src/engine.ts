import { randomUUID } from "node:crypto";
import { mkdir, writeFile } from "node:fs/promises";
import { homedir } from "node:os";
import { join } from "node:path";
import type {
  ContextEngine,
  ContextEngineInfo,
  AssembleResult,
  BootstrapResult,
  CompactResult,
  IngestBatchResult,
  IngestResult,
  SubagentEndReason,
  SubagentSpawnPreparation,
} from "../../types.js";
import { ContextAssembler } from "./assembler.js";
import { CompactionEngine, type CompactionConfig } from "./compaction.js";
import { CompressionObserver, DEFAULT_OBSERVER_CONFIG } from "./compression-observer.js";
import type { AuroraConfig } from "./db/config.js";
import { getAuroraConnection } from "./db/connection.js";
import { getAuroraDbFeatures } from "./db/features.js";
import { runAuroraMigrations } from "./db/migration.js";
import {
  type AgentMessage,
  type AssembleResultWithSystemPrompt,
  asRecord,
  buildMessageParts,
  estimateSessionTokenCountForAfterTurn,
  estimateTokens,
  messageIdentity,
  readLeafPathMessages,
  toStoredMessage,
} from "./engine-helpers.js";
import {
  createDelegatedExpansionGrant,
  removeDelegatedExpansionGrantForSession,
  revokeDelegatedExpansionGrantForSession,
} from "./expansion-auth.js";
import {
  extensionFromNameOrMime,
  formatFileReference,
  generateExplorationSummary,
  parseFileBlocks,
} from "./large-files.js";
import { RetrievalEngine } from "./retrieval.js";
import { ConversationStore } from "./store/conversation-store.js";
import { SummaryStore } from "./store/summary-store.js";
import { createAuroraSummarize } from "./summarize.js";
import type { AuroraDependencies } from "./types.js";
// ── AuroraContextEngine ──────────────────────────────────────────────────────

export class AuroraContextEngine implements ContextEngine {
  readonly info: ContextEngineInfo = {
    id: "aurora",
    name: "Aurora Context Engine",
    version: "0.1.0",
    ownsCompaction: true,
    acceptsSessionKey: true,
  };

  private config: AuroraConfig;

  /** Get the configured timezone, falling back to system timezone. */
  get timezone(): string {
    return this.config.timezone ?? Intl.DateTimeFormat().resolvedOptions().timeZone;
  }

  private conversationStore: ConversationStore;
  private summaryStore: SummaryStore;
  private assembler: ContextAssembler;
  private compaction: CompactionEngine;
  private retrieval: RetrievalEngine;
  private migrated = false;
  private readonly fts5Available: boolean;
  private sessionOperationQueues = new Map<string, Promise<void>>();
  private largeFileTextSummarizerResolved = false;
  private largeFileTextSummarizer?: (prompt: string) => Promise<string | null>;
  private compressionObserver?: CompressionObserver;
  private deps: AuroraDependencies;

  constructor(deps: AuroraDependencies) {
    this.deps = deps;
    this.config = deps.config;

    const db = getAuroraConnection(this.config.databasePath);
    this.fts5Available = getAuroraDbFeatures(db).fts5Available;

    this.conversationStore = new ConversationStore(db, { fts5Available: this.fts5Available });
    this.summaryStore = new SummaryStore(db, { fts5Available: this.fts5Available });

    if (!this.fts5Available) {
      this.deps.log.warn(
        "[aurora] FTS5 unavailable in the current Node runtime; full_text search will fall back to LIKE and indexing is disabled",
      );
    }

    this.assembler = new ContextAssembler(
      this.conversationStore,
      this.summaryStore,
      this.config.timezone,
    );

    const compactionConfig: CompactionConfig = {
      contextThreshold: this.config.contextThreshold,
      freshTailCount: this.config.freshTailCount,
      leafMinFanout: this.config.leafMinFanout,
      condensedMinFanout: this.config.condensedMinFanout,
      condensedMinFanoutHard: this.config.condensedMinFanoutHard,
      incrementalMaxDepth: this.config.incrementalMaxDepth,
      leafChunkTokens: this.config.leafChunkTokens,
      leafTargetTokens: this.config.leafTargetTokens,
      condensedTargetTokens: this.config.condensedTargetTokens,
      maxRounds: 10,
      timezone: this.config.timezone,
    };
    this.compaction = new CompactionEngine(
      this.conversationStore,
      this.summaryStore,
      compactionConfig,
    );

    // Initialize compression observer if enabled.
    // Summarizer resolution and warmup happen asynchronously after construction.
    const observerCfg = deps.config.observer;
    if (observerCfg.enabled) {
      this.initObserverAsync().catch((err) => {
        this.deps.log.error(
          `[aurora] compression observer initialization failed: ${err instanceof Error ? err.message : String(err)}`,
        );
      });
    }

    this.retrieval = new RetrievalEngine(this.conversationStore, this.summaryStore);
  }

  /** Ensure DB schema is up-to-date. Called lazily on first bootstrap/ingest/assemble/compact. */
  private ensureMigrated(): void {
    if (this.migrated) {
      return;
    }
    const db = getAuroraConnection(this.config.databasePath);
    runAuroraMigrations(db, { fts5Available: this.fts5Available });
    this.migrated = true;
  }

  /**
   * Serialize mutating operations per session to prevent ingest/compaction races.
   */
  private async withSessionQueue<T>(sessionId: string, operation: () => Promise<T>): Promise<T> {
    const previous = this.sessionOperationQueues.get(sessionId) ?? Promise.resolve();
    let releaseQueue: () => void = () => {};
    const current = new Promise<void>((resolve) => {
      releaseQueue = resolve;
    });
    const next = previous.catch(() => {}).then(() => current);
    this.sessionOperationQueues.set(sessionId, next);

    await previous.catch(() => {});
    try {
      return await operation();
    } finally {
      releaseQueue();
      void next.finally(() => {
        if (this.sessionOperationQueues.get(sessionId) === next) {
          this.sessionOperationQueues.delete(sessionId);
        }
      });
    }
  }

  /** Normalize optional live token estimates supplied by runtime callers. */
  private normalizeObservedTokenCount(value: unknown): number | undefined {
    if (typeof value !== "number" || !Number.isFinite(value) || value <= 0) {
      return undefined;
    }
    return Math.floor(value);
  }

  /** Resolve token budget from direct params or legacy fallback input. */
  private resolveTokenBudget(params: {
    tokenBudget?: number;
    runtimeParams?: Record<string, unknown>;
  }): number | undefined {
    const lp = params.runtimeParams ?? {};
    if (
      typeof params.tokenBudget === "number" &&
      Number.isFinite(params.tokenBudget) &&
      params.tokenBudget > 0
    ) {
      return Math.floor(params.tokenBudget);
    }
    if (
      typeof lp.tokenBudget === "number" &&
      Number.isFinite(lp.tokenBudget) &&
      lp.tokenBudget > 0
    ) {
      return Math.floor(lp.tokenBudget);
    }
    return undefined;
  }

  /** Resolve an Aurora conversation id from a session key via the session store. */
  private async resolveConversationIdForSessionKey(
    sessionKey: string,
  ): Promise<number | undefined> {
    const trimmedKey = sessionKey.trim();
    if (!trimmedKey) {
      return undefined;
    }
    try {
      const runtimeSessionId = await this.deps.resolveSessionIdFromSessionKey(trimmedKey);
      if (!runtimeSessionId) {
        return undefined;
      }
      const conversation =
        await this.conversationStore.getConversationBySessionId(runtimeSessionId);
      return conversation?.conversationId;
    } catch {
      return undefined;
    }
  }

  /** Build a summarize callback with runtime provider fallback handling. */
  private async resolveSummarize(params: {
    runtimeParams?: Record<string, unknown>;
    customInstructions?: string;
  }): Promise<(text: string, aggressive?: boolean) => Promise<string>> {
    const lp = params.runtimeParams ?? {};
    if (typeof lp.summarize === "function") {
      return lp.summarize as (text: string, aggressive?: boolean) => Promise<string>;
    }
    try {
      const runtimeSummarizer = await createAuroraSummarize({
        deps: this.deps,
        runtimeParams: lp,
        customInstructions: params.customInstructions,
      });
      if (runtimeSummarizer) {
        return runtimeSummarizer;
      }
      this.deps.log.warn(`[aurora] resolveSummarize: createAuroraSummarize returned undefined`);
    } catch (err) {
      this.deps.log.error(
        `[aurora] resolveSummarize failed, using emergency fallback: ${err instanceof Error ? err.message : String(err)}`,
      );
    }
    this.deps.log.error(`[aurora] resolveSummarize: FALLING BACK TO EMERGENCY TRUNCATION`);
    return createEmergencyFallbackSummarize();
  }

  /**
   * Resolve an optional model-backed summarizer for large text file exploration.
   *
   * This is opt-in via env so ingest remains deterministic and lightweight when
   * no summarization model is configured.
   */
  private async resolveLargeFileTextSummarizer(): Promise<
    ((prompt: string) => Promise<string | null>) | undefined
  > {
    if (this.largeFileTextSummarizerResolved) {
      return this.largeFileTextSummarizer;
    }
    this.largeFileTextSummarizerResolved = true;

    const provider = this.deps.config.largeFileSummaryProvider;
    const model = this.deps.config.largeFileSummaryModel;
    if (!provider || !model) {
      return undefined;
    }

    try {
      const summarize = await createAuroraSummarize({
        deps: this.deps,
        runtimeParams: { provider, model },
      });
      if (!summarize) {
        return undefined;
      }

      this.largeFileTextSummarizer = async (prompt: string): Promise<string | null> => {
        const summary = await summarize(prompt, false);
        if (typeof summary !== "string") {
          return null;
        }
        const trimmed = summary.trim();
        return trimmed.length > 0 ? trimmed : null;
      };
      return this.largeFileTextSummarizer;
    } catch {
      return undefined;
    }
  }

  /** Persist intercepted large-file text payloads to ~/.deneb/aurora-files. */
  private async storeLargeFileContent(params: {
    conversationId: number;
    fileId: string;
    extension: string;
    content: string;
  }): Promise<string> {
    const dir = join(homedir(), ".deneb", "aurora-files", String(params.conversationId));
    await mkdir(dir, { recursive: true });

    const normalizedExtension = params.extension.replace(/[^a-z0-9]/gi, "").toLowerCase() || "txt";
    const filePath = join(dir, `${params.fileId}.${normalizedExtension}`);
    await writeFile(filePath, params.content, "utf8");
    return filePath;
  }

  /**
   * Intercept oversized <file> blocks before persistence and replace them with
   * compact file references backed by large_files records.
   */
  private async interceptLargeFiles(params: {
    conversationId: number;
    content: string;
  }): Promise<{ rewrittenContent: string; fileIds: string[] } | null> {
    const blocks = parseFileBlocks(params.content);
    if (blocks.length === 0) {
      return null;
    }

    const threshold = Math.max(1, this.config.largeFileTokenThreshold);
    const summarizeText = await this.resolveLargeFileTextSummarizer();
    const fileIds: string[] = [];
    const rewrittenSegments: string[] = [];
    let cursor = 0;
    let interceptedAny = false;

    for (const block of blocks) {
      const blockTokens = estimateTokens(block.text);
      if (blockTokens < threshold) {
        continue;
      }

      interceptedAny = true;
      const fileId = `file_${randomUUID().replace(/-/g, "").slice(0, 16)}`;
      const extension = extensionFromNameOrMime(block.fileName, block.mimeType);
      const storageUri = await this.storeLargeFileContent({
        conversationId: params.conversationId,
        fileId,
        extension,
        content: block.text,
      });
      const byteSize = Buffer.byteLength(block.text, "utf8");
      const explorationSummary = await generateExplorationSummary({
        content: block.text,
        fileName: block.fileName,
        mimeType: block.mimeType,
        summarizeText,
      });

      await this.summaryStore.insertLargeFile({
        fileId,
        conversationId: params.conversationId,
        fileName: block.fileName,
        mimeType: block.mimeType,
        byteSize,
        storageUri,
        explorationSummary,
      });

      rewrittenSegments.push(params.content.slice(cursor, block.start));
      rewrittenSegments.push(
        formatFileReference({
          fileId,
          fileName: block.fileName,
          mimeType: block.mimeType,
          byteSize,
          summary: explorationSummary,
        }),
      );
      cursor = block.end;
      fileIds.push(fileId);
    }

    if (!interceptedAny) {
      return null;
    }

    rewrittenSegments.push(params.content.slice(cursor));
    return {
      rewrittenContent: rewrittenSegments.join(""),
      fileIds,
    };
  }

  /**
   * Eagerly initialize the compression observer: resolve the summarizer,
   * validate it with a warmup call, then create and attach the observer.
   *
   * If resolution or warmup fails, the observer is simply not created —
   * no silent fallbacks, no lazy failures minutes later.
   */
  private async initObserverAsync(): Promise<void> {
    const observerCfg = this.deps.config.observer;
    const model = observerCfg.model || undefined;
    const provider = observerCfg.provider || undefined;

    // Step 1: Resolve the summarizer function.
    let summarize: ((text: string, aggressive?: boolean) => Promise<string>) | undefined;

    if (model || provider) {
      try {
        summarize =
          (await createAuroraSummarize({
            deps: this.deps,
            runtimeParams: {
              ...(provider ? { provider } : {}),
              ...(model ? { model } : {}),
            },
          })) ?? undefined;
      } catch (err) {
        this.deps.log.error(
          `[aurora] observer model resolution failed: model="${model ?? ""}" provider="${provider ?? ""}": ${err instanceof Error ? err.message : String(err)}`,
        );
      }
    }

    if (!summarize) {
      // No observer-specific model or it failed — try default Aurora summarizer.
      try {
        const defaultFn = await this.resolveSummarize({});
        summarize = defaultFn;
      } catch (err) {
        this.deps.log.error(
          `[aurora] observer default summarizer resolution also failed: ${err instanceof Error ? err.message : String(err)}`,
        );
      }
    }

    if (!summarize) {
      this.deps.log.error(
        `[aurora] compression observer NOT initialized — no summarizer available. ` +
          `Check observer.model/observer.provider or default Aurora summary model config.`,
      );
      return;
    }

    // Step 2: Warmup — validate the model is reachable with a tiny test call.
    try {
      const warmupResult = await summarize("Hello world. This is a test.", false);
      if (!warmupResult || warmupResult.trim().length === 0) {
        this.deps.log.error(
          `[aurora] observer warmup returned empty response — model may not support summarization. ` +
            `model="${model ?? "(default)"}" provider="${provider ?? "(default)"}"`,
        );
        return;
      }
      this.deps.log.info(
        `[aurora] compression observer warmup OK: model="${model ?? "(default)"}" provider="${provider ?? "(default)"}"`,
      );
    } catch (err) {
      this.deps.log.error(
        `[aurora] observer warmup call failed — model is not reachable. Observer NOT started. ` +
          `model="${model ?? "(default)"}" provider="${provider ?? "(default)"}": ${
            err instanceof Error ? err.message : String(err)
          }`,
      );
      return;
    }

    // Step 3: Create and attach the observer with the validated summarizer.
    // Pass the same freshTailCount as the compaction engine uses, so the
    // observer summarizes exactly the messages that the fast path will replace.
    this.compressionObserver = new CompressionObserver(
      {
        ...DEFAULT_OBSERVER_CONFIG,
        enabled: observerCfg.enabled,
        messageInterval: observerCfg.messageInterval,
        model: model,
        provider: provider,
        maxStalenessMs: observerCfg.maxStalenessMs,
        freshTailCount: this.config.freshTailCount,
      },
      this.conversationStore,
      this.summaryStore,
      summarize,
    );
    this.compaction.attachObserver(this.compressionObserver);
  }

  // ── ContextEngine interface ─────────────────────────────────────────────

  /**
   * Reconcile session-file history with persisted messages and append only the
   * tail that is present in JSONL but missing from Aurora.
   */
  private async reconcileSessionTail(params: {
    sessionId: string;
    conversationId: number;
    historicalMessages: AgentMessage[];
  }): Promise<{
    importedMessages: number;
    hasOverlap: boolean;
  }> {
    const { sessionId, conversationId, historicalMessages } = params;
    if (historicalMessages.length === 0) {
      return { importedMessages: 0, hasOverlap: false };
    }

    const latestDbMessage = await this.conversationStore.getLastMessage(conversationId);
    if (!latestDbMessage) {
      return { importedMessages: 0, hasOverlap: false };
    }

    const storedHistoricalMessages = historicalMessages.map((message) => toStoredMessage(message));

    // Fast path: one tail comparison for the common in-sync case.
    const latestHistorical = storedHistoricalMessages[storedHistoricalMessages.length - 1];
    const latestIdentity = messageIdentity(latestDbMessage.role, latestDbMessage.content);
    if (latestIdentity === messageIdentity(latestHistorical.role, latestHistorical.content)) {
      const dbOccurrences = await this.conversationStore.countMessagesByIdentity(
        conversationId,
        latestDbMessage.role,
        latestDbMessage.content,
      );
      let historicalOccurrences = 0;
      for (const stored of storedHistoricalMessages) {
        if (messageIdentity(stored.role, stored.content) === latestIdentity) {
          historicalOccurrences += 1;
        }
      }
      if (dbOccurrences === historicalOccurrences) {
        return { importedMessages: 0, hasOverlap: true };
      }
    }

    // Slow path: walk backward through JSONL to find the most recent anchor
    // message that already exists in Aurora, then append everything after it.
    let anchorIndex = -1;
    const historicalIdentityTotals = new Map<string, number>();
    for (const stored of storedHistoricalMessages) {
      const identity = messageIdentity(stored.role, stored.content);
      historicalIdentityTotals.set(identity, (historicalIdentityTotals.get(identity) ?? 0) + 1);
    }

    const historicalIdentityCountsAfterIndex = new Map<string, number>();
    const dbIdentityCounts = new Map<string, number>();
    for (let index = storedHistoricalMessages.length - 1; index >= 0; index--) {
      const stored = storedHistoricalMessages[index];
      const identity = messageIdentity(stored.role, stored.content);
      const seenAfter = historicalIdentityCountsAfterIndex.get(identity) ?? 0;
      const total = historicalIdentityTotals.get(identity) ?? 0;
      const occurrencesThroughIndex = total - seenAfter;
      const exists = await this.conversationStore.hasMessage(
        conversationId,
        stored.role,
        stored.content,
      );
      historicalIdentityCountsAfterIndex.set(identity, seenAfter + 1);
      if (!exists) {
        continue;
      }

      let dbCountForIdentity = dbIdentityCounts.get(identity);
      if (dbCountForIdentity === undefined) {
        dbCountForIdentity = await this.conversationStore.countMessagesByIdentity(
          conversationId,
          stored.role,
          stored.content,
        );
        dbIdentityCounts.set(identity, dbCountForIdentity);
      }

      // Match the same occurrence index as the DB tail so repeated empty
      // tool messages do not anchor against a later, still-missing entry.
      if (dbCountForIdentity !== occurrencesThroughIndex) {
        continue;
      }

      anchorIndex = index;
      break;
    }

    if (anchorIndex < 0) {
      return { importedMessages: 0, hasOverlap: false };
    }
    if (anchorIndex >= historicalMessages.length - 1) {
      return { importedMessages: 0, hasOverlap: true };
    }

    const missingTail = historicalMessages.slice(anchorIndex + 1);
    let importedMessages = 0;
    for (const message of missingTail) {
      const result = await this.ingestSingle({ sessionId, message });
      if (result.ingested) {
        importedMessages += 1;
      }
    }

    return { importedMessages, hasOverlap: true };
  }

  async bootstrap(params: { sessionId: string; sessionFile: string }): Promise<BootstrapResult> {
    this.ensureMigrated();

    const result = await this.withSessionQueue(params.sessionId, async () =>
      this.conversationStore.withTransaction(async () => {
        const conversation = await this.conversationStore.getOrCreateConversation(params.sessionId);
        const conversationId = conversation.conversationId;
        const historicalMessages = readLeafPathMessages(params.sessionFile);

        // First-time import path: no Aurora rows yet, so seed directly from the
        // active leaf context snapshot.
        const existingCount = await this.conversationStore.getMessageCount(conversationId);
        if (existingCount === 0) {
          if (historicalMessages.length === 0) {
            await this.conversationStore.markConversationBootstrapped(conversationId);
            return {
              bootstrapped: false,
              importedMessages: 0,
              reason: "no leaf-path messages in session",
            };
          }

          const nextSeq = (await this.conversationStore.getMaxSeq(conversationId)) + 1;
          const bulkInput = historicalMessages.map((message, index) => {
            const stored = toStoredMessage(message);
            return {
              conversationId,
              seq: nextSeq + index,
              role: stored.role,
              content: stored.content,
              tokenCount: stored.tokenCount,
            };
          });

          const inserted = await this.conversationStore.createMessagesBulk(bulkInput);
          await this.summaryStore.appendContextMessages(
            conversationId,
            inserted.map((record) => record.messageId),
          );
          await this.conversationStore.markConversationBootstrapped(conversationId);

          // Prune HEARTBEAT_OK turns from the freshly imported data
          if (this.config.pruneHeartbeatOk) {
            const pruned = await this.pruneHeartbeatOkTurns(conversationId);
            if (pruned > 0) {
              this.deps.log.info(
                `[aurora] bootstrap: pruned ${pruned} HEARTBEAT_OK messages from conversation ${conversationId}`,
              );
            }
          }

          return {
            bootstrapped: true,
            importedMessages: inserted.length,
          };
        }

        // Existing conversation path: reconcile crash gaps by appending JSONL
        // messages that were never persisted to Aurora.
        const reconcile = await this.reconcileSessionTail({
          sessionId: params.sessionId,
          conversationId,
          historicalMessages,
        });

        if (!conversation.bootstrappedAt) {
          await this.conversationStore.markConversationBootstrapped(conversationId);
        }

        if (reconcile.importedMessages > 0) {
          return {
            bootstrapped: true,
            importedMessages: reconcile.importedMessages,
            reason: "reconciled missing session messages",
          };
        }

        if (conversation.bootstrappedAt) {
          return {
            bootstrapped: false,
            importedMessages: 0,
            reason: "already bootstrapped",
          };
        }

        return {
          bootstrapped: false,
          importedMessages: 0,
          reason: reconcile.hasOverlap
            ? "conversation already up to date"
            : "conversation already has messages",
        };
      }),
    );

    // Post-bootstrap pruning: clean HEARTBEAT_OK turns that were already
    // in the DB from prior bootstrap cycles (before pruning was enabled).
    // Runs inside the session queue to prevent races with concurrent ingest.
    if (this.config.pruneHeartbeatOk && !result.bootstrapped) {
      try {
        await this.withSessionQueue(params.sessionId, async () => {
          const conversation = await this.conversationStore.getConversationBySessionId(
            params.sessionId,
          );
          if (conversation) {
            const pruned = await this.pruneHeartbeatOkTurns(conversation.conversationId);
            if (pruned > 0) {
              this.deps.log.info(
                `[aurora] bootstrap: retroactively pruned ${pruned} HEARTBEAT_OK messages from conversation ${conversation.conversationId}`,
              );
            }
          }
        });
      } catch (err) {
        this.deps.log.error(
          `[aurora] bootstrap: heartbeat pruning failed: ${err instanceof Error ? err.message : String(err)}`,
        );
      }
    }

    return result;
  }

  private async ingestSingle(params: {
    sessionId: string;
    message: AgentMessage;
    isHeartbeat?: boolean;
  }): Promise<IngestResult> {
    const { sessionId, message, isHeartbeat } = params;
    if (isHeartbeat) {
      return { ingested: false };
    }
    const stored = toStoredMessage(message);

    // Get or create conversation for this session
    const conversation = await this.conversationStore.getOrCreateConversation(sessionId);
    const conversationId = conversation.conversationId;

    let messageForParts = message;
    if (stored.role === "user") {
      const intercepted = await this.interceptLargeFiles({
        conversationId,
        content: stored.content,
      });
      if (intercepted) {
        stored.content = intercepted.rewrittenContent;
        stored.tokenCount = estimateTokens(stored.content);
        if ("content" in message) {
          messageForParts = {
            ...message,
            content: stored.content,
          } as AgentMessage;
        }
      }
    }

    // Determine next sequence number
    const maxSeq = await this.conversationStore.getMaxSeq(conversationId);
    const seq = maxSeq + 1;

    // Persist the message
    const msgRecord = await this.conversationStore.createMessage({
      conversationId,
      seq,
      role: stored.role,
      content: stored.content,
      tokenCount: stored.tokenCount,
    });
    await this.conversationStore.createMessageParts(
      msgRecord.messageId,
      buildMessageParts({
        sessionId,
        message: messageForParts,
        fallbackContent: stored.content,
      }),
    );

    // Append to context items so assembler can see it
    await this.summaryStore.appendContextMessage(conversationId, msgRecord.messageId);

    // Notify the compression observer of the new message.
    if (this.compressionObserver) {
      this.compressionObserver.onMessage(conversationId);
    }

    return { ingested: true };
  }

  async ingest(params: {
    sessionId: string;
    message: AgentMessage;
    isHeartbeat?: boolean;
  }): Promise<IngestResult> {
    this.ensureMigrated();
    return this.withSessionQueue(params.sessionId, () => this.ingestSingle(params));
  }

  async ingestBatch(params: {
    sessionId: string;
    messages: AgentMessage[];
    isHeartbeat?: boolean;
  }): Promise<IngestBatchResult> {
    this.ensureMigrated();
    if (params.messages.length === 0) {
      return { ingestedCount: 0 };
    }
    return this.withSessionQueue(params.sessionId, async () => {
      let ingestedCount = 0;
      for (const message of params.messages) {
        const result = await this.ingestSingle({
          sessionId: params.sessionId,
          message,
          isHeartbeat: params.isHeartbeat,
        });
        if (result.ingested) {
          ingestedCount += 1;
        }
      }
      return { ingestedCount };
    });
  }

  async afterTurn(params: {
    sessionId: string;
    sessionFile: string;
    messages: AgentMessage[];
    prePromptMessageCount: number;
    autoCompactionSummary?: string;
    isHeartbeat?: boolean;
    tokenBudget?: number;
    /** Runtime context for model/provider resolution. */
    runtimeContext?: Record<string, unknown>;
    /** Alternate compaction params (fallback when runtimeContext is absent). */
    compactionParams?: Record<string, unknown>;
  }): Promise<void> {
    this.ensureMigrated();

    const ingestBatch: AgentMessage[] = [];
    if (params.autoCompactionSummary) {
      ingestBatch.push({
        role: "user",
        content: params.autoCompactionSummary,
      } as AgentMessage);
    }

    const newMessages = params.messages.slice(params.prePromptMessageCount);
    ingestBatch.push(...newMessages);
    if (ingestBatch.length === 0) {
      return;
    }

    try {
      await this.ingestBatch({
        sessionId: params.sessionId,
        messages: ingestBatch,
        isHeartbeat: params.isHeartbeat === true,
      });
    } catch (err) {
      // Continue with proactive compaction even if ingest fails,
      // but log the error so Aurora state divergence is observable.
      this.deps.log.warn(
        `[aurora] afterTurn ingestBatch failed for session=${params.sessionId}: ${String(err)}`,
      );
    }

    const tokenBudget =
      typeof params.tokenBudget === "number" &&
      Number.isFinite(params.tokenBudget) &&
      params.tokenBudget > 0
        ? Math.floor(params.tokenBudget)
        : undefined;
    if (!tokenBudget) {
      return;
    }

    const runtimeParams = asRecord(params.runtimeContext) ?? asRecord(params.compactionParams);

    const liveContextTokens = estimateSessionTokenCountForAfterTurn(params.messages);

    try {
      const leafTrigger = await this.evaluateLeafTrigger(params.sessionId);
      if (leafTrigger.shouldCompact) {
        this.compactLeafAsync({
          sessionId: params.sessionId,
          sessionFile: params.sessionFile,
          tokenBudget,
          currentTokenCount: liveContextTokens,
          runtimeParams,
        }).catch((err) => {
          // Leaf compaction is best-effort and should not fail the caller.
          this.deps.log.warn(
            `[aurora] compactLeafAsync failed for session=${params.sessionId}: ${String(err)}`,
          );
        });
      }
    } catch {
      // Leaf trigger checks are best-effort.
    }

    try {
      await this.compact({
        sessionId: params.sessionId,
        sessionFile: params.sessionFile,
        tokenBudget,
        currentTokenCount: liveContextTokens,
        compactionTarget: "threshold",
        runtimeParams,
      });
    } catch {
      // Proactive compaction is best-effort in the post-turn lifecycle.
    }
  }

  async assemble(params: {
    sessionId: string;
    messages: AgentMessage[];
    tokenBudget?: number;
  }): Promise<AssembleResult> {
    try {
      this.ensureMigrated();

      const conversation = await this.conversationStore.getConversationBySessionId(
        params.sessionId,
      );
      if (!conversation) {
        return {
          messages: params.messages,
          estimatedTokens: 0,
        };
      }

      const contextItems = await this.summaryStore.getContextItems(conversation.conversationId);
      if (contextItems.length === 0) {
        return {
          messages: params.messages,
          estimatedTokens: 0,
        };
      }

      // Guard against incomplete bootstrap/coverage: if the DB only has
      // raw context items and clearly trails the current live history, keep
      // the live path to avoid dropping prompt context.
      const hasSummaryItems = contextItems.some((item) => item.itemType === "summary");
      if (!hasSummaryItems && contextItems.length < params.messages.length) {
        return {
          messages: params.messages,
          estimatedTokens: 0,
        };
      }

      const tokenBudget =
        typeof params.tokenBudget === "number" &&
        Number.isFinite(params.tokenBudget) &&
        params.tokenBudget > 0
          ? Math.floor(params.tokenBudget)
          : 128_000;

      const assembled = await this.assembler.assemble({
        conversationId: conversation.conversationId,
        tokenBudget,
        freshTailCount: this.config.freshTailCount,
      });

      // If assembly produced no messages for a non-empty live session,
      // fail safe to the live context.
      if (assembled.messages.length === 0 && params.messages.length > 0) {
        return {
          messages: params.messages,
          estimatedTokens: 0,
        };
      }

      const result: AssembleResultWithSystemPrompt = {
        messages: assembled.messages,
        estimatedTokens: assembled.estimatedTokens,
        ...(assembled.systemPromptAddition
          ? { systemPromptAddition: assembled.systemPromptAddition }
          : {}),
      };
      return result;
    } catch {
      return {
        messages: params.messages,
        estimatedTokens: 0,
      };
    }
  }

  /** Evaluate whether incremental leaf compaction should run for a session. */
  async evaluateLeafTrigger(sessionId: string): Promise<{
    shouldCompact: boolean;
    rawTokensOutsideTail: number;
    threshold: number;
  }> {
    this.ensureMigrated();
    const conversation = await this.conversationStore.getConversationBySessionId(sessionId);
    if (!conversation) {
      const fallbackThreshold =
        typeof this.config.leafChunkTokens === "number" &&
        Number.isFinite(this.config.leafChunkTokens) &&
        this.config.leafChunkTokens > 0
          ? Math.floor(this.config.leafChunkTokens)
          : 20_000;
      return {
        shouldCompact: false,
        rawTokensOutsideTail: 0,
        threshold: fallbackThreshold,
      };
    }
    return this.compaction.evaluateLeafTrigger(conversation.conversationId);
  }

  /** Run one incremental leaf compaction pass in the per-session queue. */
  async compactLeafAsync(params: {
    sessionId: string;
    sessionFile: string;
    tokenBudget?: number;
    currentTokenCount?: number;
    customInstructions?: string;
    /** Deneb runtime param name (preferred). */
    runtimeContext?: Record<string, unknown>;
    /** Back-compat param name. */
    runtimeParams?: Record<string, unknown>;
    force?: boolean;
    previousSummaryContent?: string;
  }): Promise<CompactResult> {
    this.ensureMigrated();
    return this.withSessionQueue(params.sessionId, async () => {
      const conversation = await this.conversationStore.getConversationBySessionId(
        params.sessionId,
      );
      if (!conversation) {
        return {
          ok: true,
          compacted: false,
          reason: "no conversation found for session",
        };
      }

      const runtimeParams = asRecord(params.runtimeContext) ?? params.runtimeParams;

      const tokenBudget = this.resolveTokenBudget({
        tokenBudget: params.tokenBudget,
        runtimeParams,
      });
      if (!tokenBudget) {
        return {
          ok: false,
          compacted: false,
          reason: "missing token budget in compact params",
        };
      }

      const lp = runtimeParams ?? {};
      const observedTokens = this.normalizeObservedTokenCount(
        params.currentTokenCount ??
          (
            lp as {
              currentTokenCount?: unknown;
            }
          ).currentTokenCount,
      );
      const summarize = await this.resolveSummarize({
        runtimeParams,
        customInstructions: params.customInstructions,
      });

      const leafResult = await this.compaction.compactLeaf({
        conversationId: conversation.conversationId,
        tokenBudget,
        summarize,
        force: params.force,
        previousSummaryContent: params.previousSummaryContent,
      });
      const tokensBefore = observedTokens ?? leafResult.tokensBefore;

      return {
        ok: true,
        compacted: leafResult.actionTaken,
        reason: leafResult.actionTaken ? "compacted" : "below threshold",
        result: {
          tokensBefore,
          tokensAfter: leafResult.tokensAfter,
          details: {
            rounds: leafResult.actionTaken ? 1 : 0,
            targetTokens: tokenBudget,
            mode: "leaf",
          },
        },
      };
    });
  }

  async compact(params: {
    sessionId: string;
    sessionFile: string;
    tokenBudget?: number;
    currentTokenCount?: number;
    compactionTarget?: "budget" | "threshold";
    customInstructions?: string;
    /** Deneb runtime param name (preferred). */
    runtimeContext?: Record<string, unknown>;
    /** Back-compat param name. */
    runtimeParams?: Record<string, unknown>;
    /** Force compaction even if below threshold */
    force?: boolean;
  }): Promise<CompactResult> {
    this.ensureMigrated();
    return this.withSessionQueue(params.sessionId, async () => {
      const { sessionId, force = false } = params;

      // Look up conversation
      const conversation = await this.conversationStore.getConversationBySessionId(sessionId);
      if (!conversation) {
        return {
          ok: true,
          compacted: false,
          reason: "no conversation found for session",
        };
      }

      const conversationId = conversation.conversationId;

      const runtimeParams = asRecord(params.runtimeContext) ?? params.runtimeParams;
      const lp = runtimeParams ?? {};
      const manualCompactionRequested =
        (
          lp as {
            manualCompaction?: unknown;
          }
        ).manualCompaction === true;
      const forceCompaction = force || manualCompactionRequested;
      const tokenBudget = this.resolveTokenBudget({
        tokenBudget: params.tokenBudget,
        runtimeParams,
      });
      if (!tokenBudget) {
        return {
          ok: false,
          compacted: false,
          reason: "missing token budget in compact params",
        };
      }

      const summarize = await this.resolveSummarize({
        runtimeParams,
        customInstructions: params.customInstructions,
      });

      // Evaluate whether compaction is needed (unless forced)
      const observedTokens = this.normalizeObservedTokenCount(
        params.currentTokenCount ??
          (
            lp as {
              currentTokenCount?: unknown;
            }
          ).currentTokenCount,
      );
      const decision =
        observedTokens !== undefined
          ? await this.compaction.evaluate(conversationId, tokenBudget, observedTokens)
          : await this.compaction.evaluate(conversationId, tokenBudget);
      const targetTokens =
        params.compactionTarget === "threshold" ? decision.threshold : tokenBudget;
      const liveContextStillExceedsTarget =
        observedTokens !== undefined && observedTokens >= targetTokens;

      if (!forceCompaction && !decision.shouldCompact) {
        return {
          ok: true,
          compacted: false,
          reason: "below threshold",
          result: {
            tokensBefore: decision.currentTokens,
          },
        };
      }

      const useSweep =
        manualCompactionRequested || forceCompaction || params.compactionTarget === "threshold";
      if (useSweep) {
        const startMs = Date.now();
        const sweepResult = await this.compaction.compactFullSweep({
          conversationId,
          tokenBudget,
          summarize,
          force: forceCompaction,
          hardTrigger: false,
        });

        // Notify via callback (e.g. Telegram notification).
        if (sweepResult.actionTaken && this.deps.onCompaction) {
          try {
            this.deps.onCompaction({
              conversationId,
              tokensBefore: sweepResult.tokensBefore,
              tokensAfter: sweepResult.tokensAfter,
              actionTaken: true,
              engine: this.config.summaryProvider || this.config.observer?.provider || undefined,
              durationMs: Date.now() - startMs,
            });
          } catch {
            // Notification failure must never break compaction.
          }
        }

        return {
          ok: sweepResult.actionTaken || !liveContextStillExceedsTarget,
          compacted: sweepResult.actionTaken,
          reason: sweepResult.actionTaken
            ? "compacted"
            : manualCompactionRequested
              ? "nothing to compact"
              : liveContextStillExceedsTarget
                ? "live context still exceeds target"
                : "already under target",
          result: {
            tokensBefore: decision.currentTokens,
            tokensAfter: sweepResult.tokensAfter,
            details: {
              rounds: sweepResult.actionTaken ? 1 : 0,
              targetTokens,
            },
          },
        };
      }

      // When forced, use the token budget as target
      const convergenceTargetTokens = forceCompaction
        ? tokenBudget
        : params.compactionTarget === "threshold"
          ? decision.threshold
          : tokenBudget;

      const compactResult = await this.compaction.compactUntilUnder({
        conversationId,
        tokenBudget,
        targetTokens: convergenceTargetTokens,
        ...(observedTokens !== undefined ? { currentTokens: observedTokens } : {}),
        summarize,
      });
      const didCompact = compactResult.rounds > 0;

      return {
        ok: compactResult.success,
        compacted: didCompact,
        reason: compactResult.success
          ? didCompact
            ? "compacted"
            : "already under target"
          : "could not reach target",
        result: {
          tokensBefore: decision.currentTokens,
          tokensAfter: compactResult.finalTokens,
          details: {
            rounds: compactResult.rounds,
            targetTokens: convergenceTargetTokens,
          },
        },
      };
    });
  }

  async prepareSubagentSpawn(params: {
    parentSessionKey: string;
    childSessionKey: string;
    ttlMs?: number;
  }): Promise<SubagentSpawnPreparation | undefined> {
    this.ensureMigrated();

    const childSessionKey = params.childSessionKey.trim();
    const parentSessionKey = params.parentSessionKey.trim();
    if (!childSessionKey || !parentSessionKey) {
      return undefined;
    }

    const conversationId = await this.resolveConversationIdForSessionKey(parentSessionKey);
    if (typeof conversationId !== "number") {
      return undefined;
    }

    const ttlMs =
      typeof params.ttlMs === "number" && Number.isFinite(params.ttlMs) && params.ttlMs > 0
        ? Math.floor(params.ttlMs)
        : undefined;

    createDelegatedExpansionGrant({
      delegatedSessionKey: childSessionKey,
      issuerSessionId: parentSessionKey,
      allowedConversationIds: [conversationId],
      tokenCap: this.config.maxExpandTokens,
      ttlMs,
    });

    return {
      rollback: () => {
        revokeDelegatedExpansionGrantForSession(childSessionKey, { removeBinding: true });
      },
    };
  }

  async onSubagentEnded(params: {
    childSessionKey: string;
    reason: SubagentEndReason;
  }): Promise<void> {
    const childSessionKey = params.childSessionKey.trim();
    if (!childSessionKey) {
      return;
    }

    switch (params.reason) {
      case "deleted":
        revokeDelegatedExpansionGrantForSession(childSessionKey, { removeBinding: true });
        break;
      case "completed":
        revokeDelegatedExpansionGrantForSession(childSessionKey);
        break;
      case "released":
      case "swept":
        removeDelegatedExpansionGrantForSession(childSessionKey);
        break;
    }
  }

  async dispose(): Promise<void> {
    // No-op for plugin singleton — the connection is shared across runs.
    // Deneb's runner calls dispose() after every run, but the plugin
    // registers a single engine instance reused by the factory. Closing
    // the DB here would break subsequent runs with "database is not open".
    // The connection is cleaned up on process exit via closeAuroraConnection().
    // Note: compression observer is NOT disposed here because the engine
    // instance is a singleton reused across runs. Observer is cleaned up
    // via disposeObserver() on process exit.
  }

  /** Dispose only the compression observer (for process shutdown). */
  disposeObserver(): void {
    if (this.compressionObserver) {
      this.compressionObserver.dispose();
      this.compressionObserver = undefined;
    }
  }

  /** Clear pending session operation queues to prevent stale Promise references on shutdown. */
  clearSessionQueues(): void {
    this.sessionOperationQueues.clear();
  }

  // ── Public accessors for retrieval (used by subagent expansion) ─────────

  getRetrieval(): RetrievalEngine {
    return this.retrieval;
  }

  getConversationStore(): ConversationStore {
    return this.conversationStore;
  }

  getSummaryStore(): SummaryStore {
    return this.summaryStore;
  }

  /** Get observer status for diagnostics. Returns null if observer is not enabled. */
  getObserverStatus(conversationId: number): {
    enabled: boolean;
    hasCachedSummary: boolean;
    cachedSummaryAge?: number;
    cachedSummaryTokens?: number;
    cachedSourceTokens?: number;
  } | null {
    if (!this.compressionObserver) {
      return null;
    }
    const cached = this.compressionObserver.getCachedSummary(conversationId);
    return {
      enabled: true,
      hasCachedSummary: cached !== null,
      cachedSummaryAge: cached ? Date.now() - cached.updatedAt : undefined,
      cachedSummaryTokens: cached?.tokenCount,
      cachedSourceTokens: cached?.sourceTokenCount,
    };
  }

  // ── Heartbeat pruning ──────────────────────────────────────────────────

  /**
   * Detect HEARTBEAT_OK turn cycles in a conversation and delete them.
   *
   * A HEARTBEAT_OK turn is: a user message (the heartbeat prompt), followed by
   * any tool call/result messages, ending with an assistant message that is a
   * heartbeat ack. The entire sequence has no durable information value for Aurora.
   *
   * Detection: assistant content (trimmed, lowercased) starts with "heartbeat_ok"
   * and any text after is not alphanumeric (matches Deneb core's ack detection).
   * This catches both exact "HEARTBEAT_OK" and chatty variants like
   * "HEARTBEAT_OK — weekend, no market".
   *
   * Returns the number of messages deleted.
   */
  private async pruneHeartbeatOkTurns(conversationId: number): Promise<number> {
    const allMessages = await this.conversationStore.getMessages(conversationId);
    if (allMessages.length === 0) {
      return 0;
    }

    const toDelete: number[] = [];

    // Walk through messages finding HEARTBEAT_OK assistant replies, then
    // collect the entire turn (back to the preceding user message).
    for (let i = 0; i < allMessages.length; i++) {
      const msg = allMessages[i];
      if (msg.role !== "assistant") {
        continue;
      }
      if (!isHeartbeatOkContent(msg.content)) {
        continue;
      }

      // Found a HEARTBEAT_OK reply. Walk backward to find the turn start
      // (the preceding user message).
      const turnMessageIds: number[] = [msg.messageId];
      for (let j = i - 1; j >= 0; j--) {
        const prev = allMessages[j];
        turnMessageIds.push(prev.messageId);
        if (prev.role === "user") {
          break; // Found turn start
        }
      }

      toDelete.push(...turnMessageIds);
    }

    if (toDelete.length === 0) {
      return 0;
    }

    // Deduplicate (a message could theoretically appear in multiple turns)
    const uniqueIds = [...new Set(toDelete)];
    return this.conversationStore.deleteMessages(uniqueIds);
  }
}

// ── Heartbeat detection ─────────────────────────────────────────────────────

const HEARTBEAT_OK_TOKEN = "heartbeat_ok";

/**
 * Detect whether an assistant message is a heartbeat ack.
 *
 * Matches the same pattern as Deneb core's heartbeat-events-filter:
 * content starts with "heartbeat_ok" (case-insensitive) and any character
 * immediately after is not alphanumeric or underscore.
 *
 * This catches:
 *   - "HEARTBEAT_OK"
 *   - "  HEARTBEAT_OK  "
 *   - "HEARTBEAT_OK — weekend, no market."
 *   - "Saturday 10:48 AM PT — weekend, no market. HEARTBEAT_OK"
 *
 * But not:
 *   - "HEARTBEAT_OK_EXTENDED" (alphanumeric continuation)
 */
function isHeartbeatOkContent(content: string): boolean {
  const trimmed = content.trim().toLowerCase();
  if (!trimmed) {
    return false;
  }

  // Check if it starts with the token
  if (trimmed.startsWith(HEARTBEAT_OK_TOKEN)) {
    const suffix = trimmed.slice(HEARTBEAT_OK_TOKEN.length);
    if (suffix.length === 0) {
      return true;
    }
    return !/[a-z0-9_]/.test(suffix[0]);
  }

  // Also check if it ends with the token (chatty prefix + HEARTBEAT_OK)
  if (trimmed.endsWith(HEARTBEAT_OK_TOKEN)) {
    return true;
  }

  return false;
}

// ── Emergency fallback summarization ────────────────────────────────────────

/**
 * Creates a deterministic truncation summarizer used only as an emergency
 * fallback when the model-backed summarizer cannot be created.
 *
 * CompactionEngine already escalates normal -> aggressive -> fallback for
 * convergence. This function simply provides a stable baseline summarize
 * callback to keep compaction operable when runtime setup is unavailable.
 */
function createEmergencyFallbackSummarize(): (
  text: string,
  aggressive?: boolean,
) => Promise<string> {
  return async (text: string, aggressive?: boolean): Promise<string> => {
    const maxChars = aggressive ? 600 * 4 : 900 * 4;
    if (text.length <= maxChars) {
      return text;
    }
    return text.slice(0, maxChars) + "\n[Truncated for context management]";
  };
}
