import path from "node:path";
import type { Bot } from "grammy";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { STATE_DIR } from "../../../src/config/paths.js";
import type { TelegramBotDeps } from "./bot-deps.js";
import {
  createSequencedTestDraftStream,
  createTestDraftStream,
} from "./draft-stream.test-helpers.js";

const createTelegramDraftStream = vi.hoisted(() => vi.fn());
const dispatchReplyWithBufferedBlockDispatcher = vi.hoisted(() => vi.fn());
const deliverReplies = vi.hoisted(() => vi.fn());
const createForumTopicTelegram = vi.hoisted(() => vi.fn());
const deleteMessageTelegram = vi.hoisted(() => vi.fn());
const editForumTopicTelegram = vi.hoisted(() => vi.fn());
const editMessageTelegram = vi.hoisted(() => vi.fn());
const reactMessageTelegram = vi.hoisted(() => vi.fn());
const sendMessageTelegram = vi.hoisted(() => vi.fn());
const sendPollTelegram = vi.hoisted(() => vi.fn());
const sendStickerTelegram = vi.hoisted(() => vi.fn());
const loadConfig = vi.hoisted(() => vi.fn(() => ({})));
const readChannelAllowFromStore = vi.hoisted(() => vi.fn(async () => []));
const upsertChannelPairingRequest = vi.hoisted(() =>
  vi.fn(async () => ({
    code: "PAIRCODE",
    created: true,
  })),
);
const enqueueSystemEvent = vi.hoisted(() => vi.fn());
const buildModelsProviderData = vi.hoisted(() =>
  vi.fn(async () => ({
    byProvider: new Map<string, Set<string>>(),
    providers: [],
    resolvedDefault: { provider: "openai", model: "gpt-test" },
  })),
);
const listSkillCommandsForAgents = vi.hoisted(() => vi.fn(() => []));
const wasSentByBot = vi.hoisted(() => vi.fn(() => false));
const loadSessionStore = vi.hoisted(() => vi.fn());
const resolveStorePath = vi.hoisted(() => vi.fn(() => "/tmp/sessions.json"));

vi.mock("./draft-stream.js", () => ({
  createTelegramDraftStream,
}));

vi.mock("./bot/delivery.js", () => ({
  deliverReplies,
}));

vi.mock("./send.js", () => ({
  createForumTopicTelegram,
  deleteMessageTelegram,
  editForumTopicTelegram,
  editMessageTelegram,
  reactMessageTelegram,
  sendMessageTelegram,
  sendPollTelegram,
  sendStickerTelegram,
}));

vi.mock("deneb/plugin-sdk/config-runtime", async (importOriginal) => {
  const actual = await importOriginal<typeof import("deneb/plugin-sdk/config-runtime")>();
  return {
    ...actual,
    loadConfig,
    loadSessionStore,
    resolveStorePath,
  };
});

vi.mock("./sticker-cache.js", () => ({
  cacheSticker: vi.fn(),
  getCachedSticker: () => null,
  getCacheStats: () => ({ count: 0 }),
  searchStickers: () => [],
  getAllCachedStickers: () => [],
  describeStickerImage: vi.fn(),
}));

import { dispatchTelegramMessage } from "./bot-message-dispatch.js";

const telegramDepsForTest: TelegramBotDeps = {
  loadConfig: loadConfig as TelegramBotDeps["loadConfig"],
  resolveStorePath: resolveStorePath as TelegramBotDeps["resolveStorePath"],
  readChannelAllowFromStore:
    readChannelAllowFromStore as TelegramBotDeps["readChannelAllowFromStore"],
  upsertChannelPairingRequest:
    upsertChannelPairingRequest as TelegramBotDeps["upsertChannelPairingRequest"],
  enqueueSystemEvent: enqueueSystemEvent as TelegramBotDeps["enqueueSystemEvent"],
  dispatchReplyWithBufferedBlockDispatcher:
    dispatchReplyWithBufferedBlockDispatcher as TelegramBotDeps["dispatchReplyWithBufferedBlockDispatcher"],
  buildModelsProviderData: buildModelsProviderData as TelegramBotDeps["buildModelsProviderData"],
  listSkillCommandsForAgents:
    listSkillCommandsForAgents as TelegramBotDeps["listSkillCommandsForAgents"],
  wasSentByBot: wasSentByBot as TelegramBotDeps["wasSentByBot"],
};

describe("dispatchTelegramMessage — preview lanes & multi-message", () => {
  type TelegramMessageContext = Parameters<typeof dispatchTelegramMessage>[0]["context"];

  beforeEach(() => {
    createTelegramDraftStream.mockClear();
    dispatchReplyWithBufferedBlockDispatcher.mockClear();
    deliverReplies.mockClear();
    createForumTopicTelegram.mockClear();
    deleteMessageTelegram.mockClear();
    editForumTopicTelegram.mockClear();
    editMessageTelegram.mockClear();
    reactMessageTelegram.mockClear();
    sendMessageTelegram.mockClear();
    sendPollTelegram.mockClear();
    sendStickerTelegram.mockClear();
    loadConfig.mockClear();
    readChannelAllowFromStore.mockClear();
    upsertChannelPairingRequest.mockClear();
    enqueueSystemEvent.mockClear();
    buildModelsProviderData.mockClear();
    listSkillCommandsForAgents.mockClear();
    wasSentByBot.mockClear();
    loadSessionStore.mockClear();
    resolveStorePath.mockClear();
    loadConfig.mockReturnValue({});
    dispatchReplyWithBufferedBlockDispatcher.mockResolvedValue({
      queuedFinal: false,
      counts: { block: 0, final: 0, tool: 0 },
    });
    resolveStorePath.mockReturnValue("/tmp/sessions.json");
    loadSessionStore.mockReturnValue({});
  });

  const createDraftStream = (messageId?: number) => createTestDraftStream({ messageId });
  const createSequencedDraftStream = (startMessageId = 1001) =>
    createSequencedTestDraftStream(startMessageId);

  function setupDraftStreams(params?: { answerMessageId?: number; reasoningMessageId?: number }) {
    const answerDraftStream = createDraftStream(params?.answerMessageId);
    const reasoningDraftStream = createDraftStream(params?.reasoningMessageId);
    createTelegramDraftStream
      .mockImplementationOnce(() => answerDraftStream)
      .mockImplementationOnce(() => reasoningDraftStream);
    return { answerDraftStream, reasoningDraftStream };
  }

  function createContext(overrides?: Partial<TelegramMessageContext>): TelegramMessageContext {
    const base = {
      ctxPayload: {},
      primaryCtx: { message: { chat: { id: 123, type: "private" } } },
      msg: {
        chat: { id: 123, type: "private" },
        message_id: 456,
        message_thread_id: 777,
      },
      chatId: 123,
      isGroup: false,
      resolvedThreadId: undefined,
      replyThreadId: 777,
      threadSpec: { id: 777, scope: "dm" },
      historyKey: undefined,
      historyLimit: 0,
      groupHistories: new Map(),
      route: { agentId: "default", accountId: "default" },
      skillFilter: undefined,
      sendTyping: vi.fn(),
      sendRecordVoice: vi.fn(),
      ackReactionPromise: null,
      reactionApi: null,
      removeAckAfterReply: false,
    } as unknown as TelegramMessageContext;

    return {
      ...base,
      ...overrides,
      // Merge nested fields when overrides provide partial objects.
      primaryCtx: {
        ...(base.primaryCtx as object),
        ...(overrides?.primaryCtx ? (overrides.primaryCtx as object) : null),
      } as TelegramMessageContext["primaryCtx"],
      msg: {
        ...(base.msg as object),
        ...(overrides?.msg ? (overrides.msg as object) : null),
      } as TelegramMessageContext["msg"],
      route: {
        ...(base.route as object),
        ...(overrides?.route ? (overrides.route as object) : null),
      } as TelegramMessageContext["route"],
    };
  }

  function createBot(): Bot {
    return {
      api: {
        sendMessage: vi.fn(),
        editMessageText: vi.fn(),
        deleteMessage: vi.fn().mockResolvedValue(true),
      },
    } as unknown as Bot;
  }

  function createRuntime(): Parameters<typeof dispatchTelegramMessage>[0]["runtime"] {
    return {
      log: vi.fn(),
      error: vi.fn(),
      exit: () => {
        throw new Error("exit");
      },
    };
  }

  async function dispatchWithContext(params: {
    context: TelegramMessageContext;
    cfg?: Parameters<typeof dispatchTelegramMessage>[0]["cfg"];
    telegramCfg?: Parameters<typeof dispatchTelegramMessage>[0]["telegramCfg"];
    streamMode?: Parameters<typeof dispatchTelegramMessage>[0]["streamMode"];
    telegramDeps?: TelegramBotDeps;
    bot?: Bot;
  }) {
    const bot = params.bot ?? createBot();
    await dispatchTelegramMessage({
      context: params.context,
      bot,
      cfg: params.cfg ?? {},
      runtime: createRuntime(),
      replyToMode: "first",
      streamMode: params.streamMode ?? "partial",
      textLimit: 4096,
      telegramCfg: params.telegramCfg ?? {},
      telegramDeps: params.telegramDeps ?? telegramDepsForTest,
      opts: { token: "token" },
      _deliverReplies: deliverReplies as Parameters<
        typeof dispatchTelegramMessage
      >[0]["_deliverReplies"],
      _createTelegramDraftStream: createTelegramDraftStream as Parameters<
        typeof dispatchTelegramMessage
      >[0]["_createTelegramDraftStream"],
      _editMessageTelegram: editMessageTelegram as Parameters<
        typeof dispatchTelegramMessage
      >[0]["_editMessageTelegram"],
    });
  }

  function createReasoningStreamContext(): TelegramMessageContext {
    loadSessionStore.mockReturnValue({
      s1: { reasoningLevel: "stream" },
    });
    return createContext({
      ctxPayload: { SessionKey: "s1" } as unknown as TelegramMessageContext["ctxPayload"],
    });
  }

  it("keeps streamed preview visible when final text regresses after a tool warning", async () => {
    const draftStream = createDraftStream(999);
    createTelegramDraftStream.mockReturnValue(draftStream);
    dispatchReplyWithBufferedBlockDispatcher.mockImplementation(
      async ({ dispatcherOptions, replyOptions }) => {
        await replyOptions?.onPartialReply?.({ text: "Recovered final answer." });
        await dispatcherOptions.deliver(
          { text: "⚠️ Recovered tool error details", isError: true },
          { kind: "tool" },
        );
        await dispatcherOptions.deliver({ text: "Recovered final answer" }, { kind: "final" });
        return { queuedFinal: true };
      },
    );
    deliverReplies.mockResolvedValue({ delivered: true });

    await dispatchWithContext({ context: createContext(), streamMode: "partial" });

    // Regressive final ("answer." -> "answer") should keep the preview instead
    // of clearing it and leaving only the tool warning visible.
    expect(editMessageTelegram).not.toHaveBeenCalled();
    expect(deliverReplies).toHaveBeenCalledTimes(1);
    expect(deliverReplies).toHaveBeenCalledWith(
      expect.objectContaining({
        replies: [expect.objectContaining({ text: "⚠️ Recovered tool error details" })],
      }),
    );
    expect(draftStream.clear).not.toHaveBeenCalled();
    expect(draftStream.stop).toHaveBeenCalled();
  });

  it.each([
    { label: "default account config", telegramCfg: {} },
    { label: "account blockStreaming override", telegramCfg: { blockStreaming: true } },
  ])("disables block streaming when streamMode is off ($label)", async ({ telegramCfg }) => {
    dispatchReplyWithBufferedBlockDispatcher.mockImplementation(async ({ dispatcherOptions }) => {
      await dispatcherOptions.deliver({ text: "Hello" }, { kind: "final" });
      return { queuedFinal: true };
    });
    deliverReplies.mockResolvedValue({ delivered: true });

    await dispatchWithContext({
      context: createContext(),
      streamMode: "off",
      telegramCfg,
    });

    expect(createTelegramDraftStream).not.toHaveBeenCalled();
    expect(dispatchReplyWithBufferedBlockDispatcher).toHaveBeenCalledWith(
      expect.objectContaining({
        replyOptions: expect.objectContaining({
          disableBlockStreaming: true,
        }),
      }),
    );
  });

  it.each(["block", "partial"] as const)(
    "forces new message when assistant message restarts (%s mode)",
    async (streamMode) => {
      const draftStream = createDraftStream(999);
      createTelegramDraftStream.mockReturnValue(draftStream);
      dispatchReplyWithBufferedBlockDispatcher.mockImplementation(
        async ({ dispatcherOptions, replyOptions }) => {
          await replyOptions?.onPartialReply?.({ text: "First response" });
          await replyOptions?.onAssistantMessageStart?.();
          await replyOptions?.onPartialReply?.({ text: "After tool call" });
          await dispatcherOptions.deliver({ text: "After tool call" }, { kind: "final" });
          return { queuedFinal: true };
        },
      );
      deliverReplies.mockResolvedValue({ delivered: true });

      await dispatchWithContext({ context: createContext(), streamMode });

      expect(draftStream.forceNewMessage).toHaveBeenCalledTimes(1);
    },
  );

  it("materializes boundary preview and keeps it when no matching final arrives", async () => {
    const answerDraftStream = createDraftStream(999);
    answerDraftStream.materialize.mockResolvedValue(4321);
    const reasoningDraftStream = createDraftStream();
    createTelegramDraftStream
      .mockImplementationOnce(() => answerDraftStream)
      .mockImplementationOnce(() => reasoningDraftStream);
    dispatchReplyWithBufferedBlockDispatcher.mockImplementation(async ({ replyOptions }) => {
      await replyOptions?.onPartialReply?.({ text: "Before tool boundary" });
      await replyOptions?.onAssistantMessageStart?.();
      return { queuedFinal: false };
    });

    const bot = createBot();
    await dispatchWithContext({ context: createContext(), streamMode: "partial", bot });

    expect(answerDraftStream.materialize).toHaveBeenCalledTimes(1);
    expect(answerDraftStream.forceNewMessage).toHaveBeenCalledTimes(1);
    expect(answerDraftStream.clear).toHaveBeenCalledTimes(1);
    const deleteMessageCalls = (
      bot.api as unknown as { deleteMessage: { mock: { calls: unknown[][] } } }
    ).deleteMessage.mock.calls;
    expect(deleteMessageCalls).not.toContainEqual([123, 4321]);
  });

  it("waits for queued boundary rotation before final lane delivery", async () => {
    const answerDraftStream = createSequencedDraftStream(1001);
    let resolveMaterialize: ((value: number | undefined) => void) | undefined;
    const materializePromise = new Promise<number | undefined>((resolve) => {
      resolveMaterialize = resolve;
    });
    answerDraftStream.materialize.mockImplementation(() => materializePromise);
    const reasoningDraftStream = createDraftStream();
    createTelegramDraftStream
      .mockImplementationOnce(() => answerDraftStream)
      .mockImplementationOnce(() => reasoningDraftStream);
    dispatchReplyWithBufferedBlockDispatcher.mockImplementation(
      async ({ dispatcherOptions, replyOptions }) => {
        await replyOptions?.onPartialReply?.({ text: "Message A partial" });
        await dispatcherOptions.deliver({ text: "Message A final" }, { kind: "final" });
        const startPromise = replyOptions?.onAssistantMessageStart?.();
        const finalPromise = dispatcherOptions.deliver(
          { text: "Message B final" },
          { kind: "final" },
        );
        resolveMaterialize?.(1001);
        await startPromise;
        await finalPromise;
        return { queuedFinal: true };
      },
    );
    deliverReplies.mockResolvedValue({ delivered: true });
    editMessageTelegram.mockResolvedValue({ ok: true, chatId: "123", messageId: "1001" });

    await dispatchWithContext({ context: createContext(), streamMode: "partial" });

    expect(answerDraftStream.forceNewMessage).toHaveBeenCalledTimes(1);
    expect(editMessageTelegram).toHaveBeenCalledTimes(2);
    expect(editMessageTelegram).toHaveBeenNthCalledWith(
      2,
      123,
      1002,
      "Message B final",
      expect.any(Object),
    );
  });

  it("clears active preview even when an unrelated boundary archive exists", async () => {
    const answerDraftStream = createDraftStream(999);
    answerDraftStream.materialize.mockResolvedValue(4321);
    answerDraftStream.forceNewMessage.mockImplementation(() => {
      answerDraftStream.setMessageId(5555);
    });
    const reasoningDraftStream = createDraftStream();
    createTelegramDraftStream
      .mockImplementationOnce(() => answerDraftStream)
      .mockImplementationOnce(() => reasoningDraftStream);
    dispatchReplyWithBufferedBlockDispatcher.mockImplementation(async ({ replyOptions }) => {
      await replyOptions?.onPartialReply?.({ text: "Before tool boundary" });
      await replyOptions?.onAssistantMessageStart?.();
      await replyOptions?.onPartialReply?.({ text: "Unfinalized next preview" });
      return { queuedFinal: false };
    });

    const bot = createBot();
    await dispatchWithContext({ context: createContext(), streamMode: "partial", bot });

    expect(answerDraftStream.clear).toHaveBeenCalledTimes(1);
    const deleteMessageCalls = (
      bot.api as unknown as { deleteMessage: { mock: { calls: unknown[][] } } }
    ).deleteMessage.mock.calls;
    expect(deleteMessageCalls).not.toContainEqual([123, 4321]);
  });

  it("queues late partials behind async boundary materialization", async () => {
    const answerDraftStream = createDraftStream(999);
    let resolveMaterialize: ((value: number | undefined) => void) | undefined;
    const materializePromise = new Promise<number | undefined>((resolve) => {
      resolveMaterialize = resolve;
    });
    answerDraftStream.materialize.mockImplementation(() => materializePromise);
    const reasoningDraftStream = createDraftStream();
    createTelegramDraftStream
      .mockImplementationOnce(() => answerDraftStream)
      .mockImplementationOnce(() => reasoningDraftStream);
    dispatchReplyWithBufferedBlockDispatcher.mockImplementation(async ({ replyOptions }) => {
      await replyOptions?.onPartialReply?.({ text: "Message A partial" });

      // Simulate provider fire-and-forget ordering: boundary callback starts
      // and a new partial arrives before boundary materialization resolves.
      const startPromise = replyOptions?.onAssistantMessageStart?.();
      const nextPartialPromise = replyOptions?.onPartialReply?.({ text: "Message B early" });

      expect(answerDraftStream.update).toHaveBeenCalledTimes(1);
      resolveMaterialize?.(4321);

      await startPromise;
      await nextPartialPromise;
      return { queuedFinal: false };
    });

    await dispatchWithContext({ context: createContext(), streamMode: "partial" });

    expect(answerDraftStream.materialize).toHaveBeenCalledTimes(1);
    expect(answerDraftStream.forceNewMessage).toHaveBeenCalledTimes(1);
    expect(answerDraftStream.update).toHaveBeenCalledTimes(2);
    expect(answerDraftStream.update).toHaveBeenNthCalledWith(2, "Message B early");
    const boundaryRotationOrder = answerDraftStream.forceNewMessage.mock.invocationCallOrder[0];
    const secondUpdateOrder = answerDraftStream.update.mock.invocationCallOrder[1];
    expect(boundaryRotationOrder).toBeLessThan(secondUpdateOrder);
  });

  it("keeps final-only preview lane finalized until a real boundary rotation happens", async () => {
    const answerDraftStream = createSequencedDraftStream(1001);
    const reasoningDraftStream = createDraftStream();
    createTelegramDraftStream
      .mockImplementationOnce(() => answerDraftStream)
      .mockImplementationOnce(() => reasoningDraftStream);
    dispatchReplyWithBufferedBlockDispatcher.mockImplementation(
      async ({ dispatcherOptions, replyOptions }) => {
        // Final-only first response (no streamed partials).
        await dispatcherOptions.deliver({ text: "Message A final" }, { kind: "final" });
        // Simulate provider ordering bug: first chunk arrives before message-start callback.
        await replyOptions?.onPartialReply?.({ text: "Message B early" });
        await replyOptions?.onAssistantMessageStart?.();
        await replyOptions?.onPartialReply?.({ text: "Message B partial" });
        await dispatcherOptions.deliver({ text: "Message B final" }, { kind: "final" });
        return { queuedFinal: true };
      },
    );
    deliverReplies.mockResolvedValue({ delivered: true });
    editMessageTelegram.mockResolvedValue({ ok: true, chatId: "123", messageId: "1001" });

    await dispatchWithContext({ context: createContext(), streamMode: "partial" });

    expect(answerDraftStream.forceNewMessage).toHaveBeenCalledTimes(1);
    expect(editMessageTelegram).toHaveBeenNthCalledWith(
      1,
      123,
      1001,
      "Message A final",
      expect.any(Object),
    );
    expect(editMessageTelegram).toHaveBeenNthCalledWith(
      2,
      123,
      1002,
      "Message B final",
      expect.any(Object),
    );
  });

  it("does not force new message on first assistant message start", async () => {
    const draftStream = createDraftStream(999);
    createTelegramDraftStream.mockReturnValue(draftStream);
    dispatchReplyWithBufferedBlockDispatcher.mockImplementation(
      async ({ dispatcherOptions, replyOptions }) => {
        // First assistant message starts (no previous output)
        await replyOptions?.onAssistantMessageStart?.();
        // Partial updates
        await replyOptions?.onPartialReply?.({ text: "Hello" });
        await replyOptions?.onPartialReply?.({ text: "Hello world" });
        await dispatcherOptions.deliver({ text: "Hello world" }, { kind: "final" });
        return { queuedFinal: true };
      },
    );
    deliverReplies.mockResolvedValue({ delivered: true });

    await dispatchWithContext({ context: createContext(), streamMode: "block" });

    // First message start shouldn't trigger forceNewMessage (no previous output)
    expect(draftStream.forceNewMessage).not.toHaveBeenCalled();
  });

  it("rotates before a late second-message partial so finalized preview is not overwritten", async () => {
    const answerDraftStream = createSequencedDraftStream(1001);
    const reasoningDraftStream = createDraftStream();
    createTelegramDraftStream
      .mockImplementationOnce(() => answerDraftStream)
      .mockImplementationOnce(() => reasoningDraftStream);
    dispatchReplyWithBufferedBlockDispatcher.mockImplementation(
      async ({ dispatcherOptions, replyOptions }) => {
        await replyOptions?.onPartialReply?.({ text: "Message A partial" });
        await dispatcherOptions.deliver({ text: "Message A final" }, { kind: "final" });
        // Simulate provider ordering bug: first chunk arrives before message-start callback.
        await replyOptions?.onPartialReply?.({ text: "Message B early" });
        await replyOptions?.onAssistantMessageStart?.();
        await replyOptions?.onPartialReply?.({ text: "Message B partial" });
        await dispatcherOptions.deliver({ text: "Message B final" }, { kind: "final" });
        return { queuedFinal: true };
      },
    );
    deliverReplies.mockResolvedValue({ delivered: true });
    editMessageTelegram.mockResolvedValue({ ok: true, chatId: "123", messageId: "1001" });

    await dispatchWithContext({ context: createContext(), streamMode: "partial" });

    expect(answerDraftStream.forceNewMessage).toHaveBeenCalledTimes(1);
    expect(answerDraftStream.update).toHaveBeenNthCalledWith(2, "Message B early");
    const boundaryRotationOrder = answerDraftStream.forceNewMessage.mock.invocationCallOrder[0];
    const secondUpdateOrder = answerDraftStream.update.mock.invocationCallOrder[1];
    expect(boundaryRotationOrder).toBeLessThan(secondUpdateOrder);
    expect(editMessageTelegram).toHaveBeenNthCalledWith(
      1,
      123,
      1001,
      "Message A final",
      expect.any(Object),
    );
    expect(editMessageTelegram).toHaveBeenNthCalledWith(
      2,
      123,
      1002,
      "Message B final",
      expect.any(Object),
    );
  });

  it("does not skip message-start rotation when pre-rotation did not force a new message", async () => {
    const answerDraftStream = createSequencedDraftStream(1002);
    answerDraftStream.setMessageId(1001);
    const reasoningDraftStream = createDraftStream();
    createTelegramDraftStream
      .mockImplementationOnce(() => answerDraftStream)
      .mockImplementationOnce(() => reasoningDraftStream);
    dispatchReplyWithBufferedBlockDispatcher.mockImplementation(
      async ({ dispatcherOptions, replyOptions }) => {
        // First message has only final text (no streamed partials), so answer lane
        // reaches finalized state with hasStreamedMessage still false.
        await dispatcherOptions.deliver({ text: "Message A final" }, { kind: "final" });
        // Provider ordering bug: next message partial arrives before message-start.
        await replyOptions?.onPartialReply?.({ text: "Message B early" });
        await replyOptions?.onAssistantMessageStart?.();
        await replyOptions?.onPartialReply?.({ text: "Message B partial" });
        await dispatcherOptions.deliver({ text: "Message B final" }, { kind: "final" });
        return { queuedFinal: true };
      },
    );
    deliverReplies.mockResolvedValue({ delivered: true });
    editMessageTelegram.mockResolvedValue({ ok: true, chatId: "123", messageId: "1001" });
    const bot = createBot();

    await dispatchWithContext({ context: createContext(), streamMode: "partial", bot });

    // Early pre-rotation could not force (no streamed partials yet), so the
    // real assistant message_start must still rotate once.
    expect(answerDraftStream.forceNewMessage).toHaveBeenCalledTimes(1);
    expect(answerDraftStream.update).toHaveBeenNthCalledWith(1, "Message B early");
    expect(answerDraftStream.update).toHaveBeenNthCalledWith(2, "Message B partial");
    const earlyUpdateOrder = answerDraftStream.update.mock.invocationCallOrder[0];
    const boundaryRotationOrder = answerDraftStream.forceNewMessage.mock.invocationCallOrder[0];
    const secondUpdateOrder = answerDraftStream.update.mock.invocationCallOrder[1];
    expect(earlyUpdateOrder).toBeLessThan(boundaryRotationOrder);
    expect(boundaryRotationOrder).toBeLessThan(secondUpdateOrder);
    expect(editMessageTelegram).toHaveBeenNthCalledWith(
      1,
      123,
      1001,
      "Message A final",
      expect.any(Object),
    );
    expect(editMessageTelegram).toHaveBeenNthCalledWith(
      2,
      123,
      1002,
      "Message B final",
      expect.any(Object),
    );
    expect((bot.api.deleteMessage as ReturnType<typeof vi.fn>).mock.calls).toHaveLength(0);
  });

  it("does not trigger late pre-rotation mid-message after an explicit assistant message start", async () => {
    const answerDraftStream = createDraftStream(1001);
    const reasoningDraftStream = createDraftStream();
    createTelegramDraftStream
      .mockImplementationOnce(() => answerDraftStream)
      .mockImplementationOnce(() => reasoningDraftStream);
    dispatchReplyWithBufferedBlockDispatcher.mockImplementation(
      async ({ dispatcherOptions, replyOptions }) => {
        // Message A finalizes without streamed partials.
        await dispatcherOptions.deliver({ text: "Message A final" }, { kind: "final" });
        // Message B starts normally before partials.
        await replyOptions?.onAssistantMessageStart?.();
        await replyOptions?.onPartialReply?.({ text: "Message B first chunk" });
        await replyOptions?.onPartialReply?.({ text: "Message B second chunk" });
        await dispatcherOptions.deliver({ text: "Message B final" }, { kind: "final" });
        return { queuedFinal: true };
      },
    );
    deliverReplies.mockResolvedValue({ delivered: true });
    editMessageTelegram.mockResolvedValue({ ok: true, chatId: "123", messageId: "1001" });

    await dispatchWithContext({ context: createContext(), streamMode: "partial" });

    // The explicit message_start boundary must clear finalized state so
    // same-message partials do not force a new preview mid-stream.
    expect(answerDraftStream.forceNewMessage).not.toHaveBeenCalled();
    expect(answerDraftStream.update).toHaveBeenNthCalledWith(1, "Message B first chunk");
    expect(answerDraftStream.update).toHaveBeenNthCalledWith(2, "Message B second chunk");
  });

  it("finalizes multi-message assistant stream to matching preview messages in order", async () => {
    const answerDraftStream = createSequencedDraftStream(1001);
    const reasoningDraftStream = createDraftStream();
    createTelegramDraftStream
      .mockImplementationOnce(() => answerDraftStream)
      .mockImplementationOnce(() => reasoningDraftStream);
    dispatchReplyWithBufferedBlockDispatcher.mockImplementation(
      async ({ dispatcherOptions, replyOptions }) => {
        await replyOptions?.onPartialReply?.({ text: "Message A partial" });
        await replyOptions?.onAssistantMessageStart?.();
        await replyOptions?.onPartialReply?.({ text: "Message B partial" });
        await replyOptions?.onAssistantMessageStart?.();
        await replyOptions?.onPartialReply?.({ text: "Message C partial" });

        await dispatcherOptions.deliver({ text: "Message A final" }, { kind: "final" });
        await dispatcherOptions.deliver({ text: "Message B final" }, { kind: "final" });
        await dispatcherOptions.deliver({ text: "Message C final" }, { kind: "final" });
        return { queuedFinal: true };
      },
    );
    deliverReplies.mockResolvedValue({ delivered: true });
    editMessageTelegram.mockResolvedValue({ ok: true, chatId: "123", messageId: "1001" });

    await dispatchWithContext({ context: createContext(), streamMode: "partial" });

    expect(answerDraftStream.forceNewMessage).toHaveBeenCalledTimes(2);
    expect(editMessageTelegram).toHaveBeenNthCalledWith(
      1,
      123,
      1001,
      "Message A final",
      expect.any(Object),
    );
    expect(editMessageTelegram).toHaveBeenNthCalledWith(
      2,
      123,
      1002,
      "Message B final",
      expect.any(Object),
    );
    expect(editMessageTelegram).toHaveBeenNthCalledWith(
      3,
      123,
      1003,
      "Message C final",
      expect.any(Object),
    );
    expect(deliverReplies).not.toHaveBeenCalled();
  });

  it("maps finals correctly when first preview id resolves after message boundary", async () => {
    let answerMessageId: number | undefined;
    let answerDraftParams:
      | {
          onSupersededPreview?: (preview: { messageId: number; textSnapshot: string }) => void;
        }
      | undefined;
    const answerDraftStream = {
      update: vi.fn().mockImplementation((text: string) => {
        if (text.includes("Message B")) {
          answerMessageId = 1002;
        }
      }),
      flush: vi.fn().mockResolvedValue(undefined),
      messageId: vi.fn().mockImplementation(() => answerMessageId),
      clear: vi.fn().mockResolvedValue(undefined),
      stop: vi.fn().mockResolvedValue(undefined),
      forceNewMessage: vi.fn().mockImplementation(() => {
        answerMessageId = undefined;
      }),
    };
    const reasoningDraftStream = createDraftStream();
    createTelegramDraftStream
      .mockImplementationOnce((params) => {
        answerDraftParams = params as typeof answerDraftParams;
        return answerDraftStream;
      })
      .mockImplementationOnce(() => reasoningDraftStream);
    dispatchReplyWithBufferedBlockDispatcher.mockImplementation(
      async ({ dispatcherOptions, replyOptions }) => {
        await replyOptions?.onPartialReply?.({ text: "Message A partial" });
        await replyOptions?.onAssistantMessageStart?.();
        await replyOptions?.onPartialReply?.({ text: "Message B partial" });
        // Simulate late resolution of message A preview ID after boundary rotation.
        answerDraftParams?.onSupersededPreview?.({
          messageId: 1001,
          textSnapshot: "Message A partial",
        });

        await dispatcherOptions.deliver({ text: "Message A final" }, { kind: "final" });
        await dispatcherOptions.deliver({ text: "Message B final" }, { kind: "final" });
        return { queuedFinal: true };
      },
    );
    deliverReplies.mockResolvedValue({ delivered: true });
    editMessageTelegram.mockResolvedValue({ ok: true, chatId: "123", messageId: "1001" });

    await dispatchWithContext({ context: createContext(), streamMode: "partial" });

    expect(editMessageTelegram).toHaveBeenNthCalledWith(
      1,
      123,
      1001,
      "Message A final",
      expect.any(Object),
    );
    expect(editMessageTelegram).toHaveBeenNthCalledWith(
      2,
      123,
      1002,
      "Message B final",
      expect.any(Object),
    );
    expect(deliverReplies).not.toHaveBeenCalled();
  });

  it("keeps the active preview when an archived final edit target is missing", async () => {
    let answerMessageId: number | undefined;
    let answerDraftParams:
      | {
          onSupersededPreview?: (preview: { messageId: number; textSnapshot: string }) => void;
        }
      | undefined;
    const answerDraftStream = {
      update: vi.fn().mockImplementation((text: string) => {
        if (text.includes("Message B")) {
          answerMessageId = 1002;
        }
      }),
      flush: vi.fn().mockResolvedValue(undefined),
      messageId: vi.fn().mockImplementation(() => answerMessageId),
      clear: vi.fn().mockResolvedValue(undefined),
      stop: vi.fn().mockResolvedValue(undefined),
      forceNewMessage: vi.fn().mockImplementation(() => {
        answerMessageId = undefined;
      }),
    };
    const reasoningDraftStream = createDraftStream();
    createTelegramDraftStream
      .mockImplementationOnce((params) => {
        answerDraftParams = params as typeof answerDraftParams;
        return answerDraftStream;
      })
      .mockImplementationOnce(() => reasoningDraftStream);
    dispatchReplyWithBufferedBlockDispatcher.mockImplementation(
      async ({ dispatcherOptions, replyOptions }) => {
        await replyOptions?.onPartialReply?.({ text: "Message A partial" });
        await replyOptions?.onAssistantMessageStart?.();
        await replyOptions?.onPartialReply?.({ text: "Message B partial" });
        answerDraftParams?.onSupersededPreview?.({
          messageId: 1001,
          textSnapshot: "Message A partial",
        });

        await dispatcherOptions.deliver({ text: "Message A final" }, { kind: "final" });
        return { queuedFinal: true };
      },
    );
    deliverReplies.mockResolvedValue({ delivered: true });
    editMessageTelegram.mockRejectedValue(new Error("400: Bad Request: message to edit not found"));

    await dispatchWithContext({ context: createContext(), streamMode: "partial" });

    expect(editMessageTelegram).toHaveBeenCalledWith(
      123,
      1001,
      "Message A final",
      expect.any(Object),
    );
    expect(answerDraftStream.clear).not.toHaveBeenCalled();
    expect(deliverReplies).not.toHaveBeenCalled();
  });

  it("still finalizes the active preview after an archived final edit is retained", async () => {
    let answerMessageId: number | undefined;
    let answerDraftParams:
      | {
          onSupersededPreview?: (preview: { messageId: number; textSnapshot: string }) => void;
        }
      | undefined;
    const answerDraftStream = {
      update: vi.fn().mockImplementation((text: string) => {
        if (text.includes("Message B")) {
          answerMessageId = 1002;
        }
      }),
      flush: vi.fn().mockResolvedValue(undefined),
      messageId: vi.fn().mockImplementation(() => answerMessageId),
      clear: vi.fn().mockResolvedValue(undefined),
      stop: vi.fn().mockResolvedValue(undefined),
      forceNewMessage: vi.fn().mockImplementation(() => {
        answerMessageId = undefined;
      }),
    };
    const reasoningDraftStream = createDraftStream();
    createTelegramDraftStream
      .mockImplementationOnce((params) => {
        answerDraftParams = params as typeof answerDraftParams;
        return answerDraftStream;
      })
      .mockImplementationOnce(() => reasoningDraftStream);
    dispatchReplyWithBufferedBlockDispatcher.mockImplementation(
      async ({ dispatcherOptions, replyOptions }) => {
        await replyOptions?.onPartialReply?.({ text: "Message A partial" });
        await replyOptions?.onAssistantMessageStart?.();
        await replyOptions?.onPartialReply?.({ text: "Message B partial" });
        answerDraftParams?.onSupersededPreview?.({
          messageId: 1001,
          textSnapshot: "Message A partial",
        });

        await dispatcherOptions.deliver({ text: "Message A final" }, { kind: "final" });
        await dispatcherOptions.deliver({ text: "Message B final" }, { kind: "final" });
        return { queuedFinal: true };
      },
    );
    deliverReplies.mockResolvedValue({ delivered: true });
    editMessageTelegram
      .mockRejectedValueOnce(new Error("400: Bad Request: message to edit not found"))
      .mockResolvedValueOnce({ ok: true, chatId: "123", messageId: "1002" });

    await dispatchWithContext({ context: createContext(), streamMode: "partial" });

    expect(editMessageTelegram).toHaveBeenNthCalledWith(
      1,
      123,
      1001,
      "Message A final",
      expect.any(Object),
    );
    expect(editMessageTelegram).toHaveBeenNthCalledWith(
      2,
      123,
      1002,
      "Message B final",
      expect.any(Object),
    );
    expect(answerDraftStream.clear).not.toHaveBeenCalled();
    expect(deliverReplies).not.toHaveBeenCalled();
  });
});
