import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import type { GetReplyOptions, MsgContext } from "deneb/plugin-sdk/reply-runtime";
import { afterAll, beforeAll, describe, expect, it, vi } from "vitest";
import { escapeRegExp, formatEnvelopeTimestamp } from "../../../test/helpers/envelope-timestamp.js";
import { withEnvAsync } from "../../../test/helpers/extensions/env.js";
import { useFrozenTime, useRealTime } from "../../../test/helpers/extensions/frozen-time.js";
const harness = await import("./bot.create-telegram-bot.test-harness.js");
const {
  answerCallbackQuerySpy,
  botCtorSpy,
  commandSpy,
  dispatchReplyWithBufferedBlockDispatcher,
  getLoadConfigMock,
  getOnHandler,
  getReadChannelAllowFromStoreMock,
  getUpsertChannelPairingRequestMock,
  makeForumGroupMessageCtx,
  middlewareUseSpy,
  onSpy,
  replySpy,
  sendAnimationSpy,
  sendChatActionSpy,
  sendMessageSpy,
  sendPhotoSpy,
  sequentializeSpy,
  setMessageReactionSpy,
  setMyCommandsSpy,
  telegramBotDepsForTest,
  telegramBotRuntimeForTest,
  throttlerSpy,
  useSpy,
} = harness;
import { resolveTelegramFetch } from "./fetch.js";

// Import after the harness registers `vi.mock(...)` for grammY and Telegram internals.
const {
  createTelegramBot: createTelegramBotBase,
  getTelegramSequentialKey,
  setTelegramBotRuntimeForTest,
} = await import("./bot.js");
setTelegramBotRuntimeForTest(
  telegramBotRuntimeForTest as unknown as Parameters<typeof setTelegramBotRuntimeForTest>[0],
);
const createTelegramBot = (opts: Parameters<typeof createTelegramBotBase>[0]) =>
  createTelegramBotBase({
    ...opts,
    telegramDeps: telegramBotDepsForTest,
  });

const loadConfig = getLoadConfigMock();
const readChannelAllowFromStore = getReadChannelAllowFromStoreMock();
const upsertChannelPairingRequest = getUpsertChannelPairingRequestMock();

const ORIGINAL_TZ = process.env.TZ;
const TELEGRAM_TEST_TIMINGS = {
  mediaGroupFlushMs: 20,
  textFragmentGapMs: 30,
} as const;

async function withIsolatedStateDirAsync<T>(fn: () => Promise<T>): Promise<T> {
  const stateDir = fs.mkdtempSync(path.join(os.tmpdir(), "deneb-telegram-state-"));
  return await withEnvAsync({ DENEB_STATE_DIR: stateDir }, async () => {
    try {
      return await fn();
    } finally {
      fs.rmSync(stateDir, { recursive: true, force: true });
    }
  });
}

async function withConfigPathAsync<T>(cfg: unknown, fn: () => Promise<T>): Promise<T> {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "deneb-telegram-cfg-"));
  const configPath = path.join(dir, "deneb.json");
  fs.writeFileSync(configPath, JSON.stringify(cfg), "utf-8");
  return await withEnvAsync({ DENEB_CONFIG_PATH: configPath }, async () => {
    try {
      return await fn();
    } finally {
      fs.rmSync(dir, { recursive: true, force: true });
    }
  });
}
describe("createTelegramBot", () => {
  beforeAll(() => {
    process.env.TZ = "UTC";
  });
  afterAll(() => {
    process.env.TZ = ORIGINAL_TZ;
  });

  // groupPolicy tests

  it("installs grammY throttler", () => {
    createTelegramBot({ token: "tok" });
    expect(throttlerSpy).toHaveBeenCalledTimes(1);
    expect(useSpy).toHaveBeenCalledWith("throttler");
  });
  it("uses wrapped fetch when global fetch is available", () => {
    const originalFetch = globalThis.fetch;
    const fetchSpy = vi.fn() as unknown as typeof fetch;
    globalThis.fetch = fetchSpy;
    try {
      createTelegramBot({ token: "tok" });
      const fetchImpl = resolveTelegramFetch();
      expect(fetchImpl).toBeTypeOf("function");
      expect(fetchImpl).not.toBe(fetchSpy);
      const clientFetch = (botCtorSpy.mock.calls[0]?.[1] as { client?: { fetch?: unknown } })
        ?.client?.fetch;
      expect(clientFetch).toBeTypeOf("function");
      expect(clientFetch).not.toBe(fetchSpy);
    } finally {
      globalThis.fetch = originalFetch;
    }
  });
  it("applies global and per-account timeoutSeconds", () => {
    loadConfig.mockReturnValue({
      channels: {
        telegram: { dmPolicy: "open", allowFrom: ["*"], timeoutSeconds: 60 },
      },
    });
    createTelegramBot({ token: "tok" });
    expect(botCtorSpy).toHaveBeenCalledWith(
      "tok",
      expect.objectContaining({
        client: expect.objectContaining({ timeoutSeconds: 60 }),
      }),
    );
    botCtorSpy.mockClear();

    loadConfig.mockReturnValue({
      channels: {
        telegram: {
          dmPolicy: "open",
          allowFrom: ["*"],
          timeoutSeconds: 60,
          accounts: {
            foo: { timeoutSeconds: 61 },
          },
        },
      },
    });
    createTelegramBot({ token: "tok", accountId: "foo" });
    expect(botCtorSpy).toHaveBeenCalledWith(
      "tok",
      expect.objectContaining({
        client: expect.objectContaining({ timeoutSeconds: 61 }),
      }),
    );
  });
  it("sequentializes updates by chat and thread", () => {
    createTelegramBot({ token: "tok" });
    expect(sequentializeSpy).toHaveBeenCalledTimes(1);
    expect(middlewareUseSpy).toHaveBeenCalledWith(sequentializeSpy.mock.results[0]?.value);
    expect(harness.sequentializeKey).toBe(getTelegramSequentialKey);
  });
  it("routes callback_query payloads as messages and answers callbacks", async () => {
    createTelegramBot({ token: "tok" });
    const callbackHandler = onSpy.mock.calls.find((call) => call[0] === "callback_query")?.[1] as (
      ctx: Record<string, unknown>,
    ) => Promise<void>;
    expect(callbackHandler).toBeDefined();

    await callbackHandler({
      callbackQuery: {
        id: "cbq-1",
        data: "cmd:option_a",
        from: { id: 9, first_name: "Ada", username: "ada_bot" },
        message: {
          chat: { id: 1234, type: "private" },
          date: 1736380800,
          message_id: 10,
        },
      },
      me: { username: "deneb_bot" },
      getFile: async () => ({ download: async () => new Uint8Array() }),
    });

    expect(replySpy).toHaveBeenCalledTimes(1);
    const payload = replySpy.mock.calls[0][0];
    expect(payload.Body).toContain("cmd:option_a");
    expect(answerCallbackQuerySpy).toHaveBeenCalledWith("cbq-1");
  });
  it("reloads callback model routing bindings without recreating the bot", async () => {
    const buildModelsProviderDataMock =
      telegramBotDepsForTest.buildModelsProviderData as unknown as ReturnType<typeof vi.fn>;
    let boundAgentId = "agent-a";
    loadConfig.mockImplementation(() => ({
      agents: {
        defaults: {
          model: "openai/gpt-4.1",
        },
        list: [{ id: "agent-a" }, { id: "agent-b" }],
      },
      channels: {
        telegram: { dmPolicy: "open", allowFrom: ["*"] },
      },
      bindings: [
        {
          agentId: boundAgentId,
          match: { channel: "telegram", accountId: "default" },
        },
      ],
    }));

    createTelegramBot({ token: "tok" });
    const callbackHandler = getOnHandler("callback_query") as (
      ctx: Record<string, unknown>,
    ) => Promise<void>;

    const sendModelCallback = async (id: number) => {
      await callbackHandler({
        callbackQuery: {
          id: `cbq-model-${id}`,
          data: "mdl_prov",
          from: { id: 9, first_name: "Ada", username: "ada_bot" },
          message: {
            chat: { id: 1234, type: "private" },
            date: 1736380800 + id,
            message_id: id,
          },
        },
        me: { username: "deneb_bot" },
        getFile: async () => ({ download: async () => new Uint8Array() }),
      });
    };

    buildModelsProviderDataMock.mockClear();
    await sendModelCallback(1);
    expect(buildModelsProviderDataMock).toHaveBeenCalled();
    expect(buildModelsProviderDataMock.mock.calls.at(-1)?.[1]).toBe("agent-a");

    boundAgentId = "agent-b";
    await sendModelCallback(2);
    expect(buildModelsProviderDataMock.mock.calls.at(-1)?.[1]).toBe("agent-b");
  });
  it("wraps inbound message with Telegram envelope", async () => {
    await withEnvAsync({ TZ: "Europe/Vienna" }, async () => {
      createTelegramBot({ token: "tok" });
      expect(onSpy).toHaveBeenCalledWith("message", expect.any(Function));
      const handler = getOnHandler("message") as (ctx: Record<string, unknown>) => Promise<void>;

      const message = {
        chat: { id: 1234, type: "private" },
        text: "hello world",
        date: 1736380800, // 2025-01-09T00:00:00Z
        from: {
          first_name: "Ada",
          last_name: "Lovelace",
          username: "ada_bot",
        },
      };
      await handler({
        message,
        me: { username: "deneb_bot" },
        getFile: async () => ({ download: async () => new Uint8Array() }),
      });

      expect(replySpy).toHaveBeenCalledTimes(1);
      const payload = replySpy.mock.calls[0][0];
      const expectedTimestamp = formatEnvelopeTimestamp(new Date("2025-01-09T00:00:00Z"));
      const timestampPattern = escapeRegExp(expectedTimestamp);
      expect(payload.Body).toMatch(
        new RegExp(
          `^\\[Telegram Ada Lovelace \\(@ada_bot\\) id:1234 (\\+\\d+[smhd] )?${timestampPattern}\\]`,
        ),
      );
      expect(payload.Body).toContain("hello world");
    });
  });
  it("handles pairing DM flows for new and already-pending requests", async () => {
    const cases = [
      {
        name: "new unknown sender",
        messages: ["hello"],
        expectedSendCount: 1,
        pairingUpsertResults: [{ code: "PAIRCODE", created: true }],
      },
      {
        name: "already pending request",
        messages: ["hello", "hello again"],
        expectedSendCount: 1,
        pairingUpsertResults: [
          { code: "PAIRCODE", created: true },
          { code: "PAIRCODE", created: false },
        ],
      },
    ] as const;

    await withIsolatedStateDirAsync(async () => {
      for (const [index, testCase] of cases.entries()) {
        onSpy.mockClear();
        sendMessageSpy.mockClear();
        replySpy.mockClear();
        loadConfig.mockReturnValue({
          channels: { telegram: { dmPolicy: "pairing" } },
        });
        readChannelAllowFromStore.mockResolvedValue([]);
        upsertChannelPairingRequest.mockClear();
        let pairingUpsertCall = 0;
        upsertChannelPairingRequest.mockImplementation(async () => {
          const result =
            testCase.pairingUpsertResults[
              Math.min(pairingUpsertCall, testCase.pairingUpsertResults.length - 1)
            ];
          pairingUpsertCall += 1;
          return result;
        });

        createTelegramBot({ token: "tok" });
        const handler = getOnHandler("message") as (ctx: Record<string, unknown>) => Promise<void>;
        const senderId = Number(`${Date.now()}${index}`.slice(-9));
        for (const text of testCase.messages) {
          await handler({
            message: {
              chat: { id: 1234, type: "private" },
              text,
              date: 1736380800,
              from: { id: senderId, username: "random" },
            },
            me: { username: "deneb_bot" },
            getFile: async () => ({ download: async () => new Uint8Array() }),
          });
        }

        expect(replySpy, testCase.name).not.toHaveBeenCalled();
        expect(sendMessageSpy, testCase.name).toHaveBeenCalledTimes(testCase.expectedSendCount);
        expect(sendMessageSpy.mock.calls[0]?.[0], testCase.name).toBe(1234);
        const pairingText = String(sendMessageSpy.mock.calls[0]?.[1]);
        expect(pairingText, testCase.name).toContain(`Your Telegram user id: ${senderId}`);
        expect(pairingText, testCase.name).toContain("Pairing code:");
        const code = pairingText.match(/Pairing code:\s*([A-Z2-9]{8})/)?.[1];
        expect(code, testCase.name).toBeDefined();
        expect(pairingText, testCase.name).toContain(`deneb pairing approve telegram ${code}`);
        expect(pairingText, testCase.name).not.toContain("<code>");
      }
    });
  });
  it("blocks unauthorized DM media before download and sends pairing reply", async () => {
    await withIsolatedStateDirAsync(async () => {
      loadConfig.mockReturnValue({
        channels: { telegram: { dmPolicy: "pairing" } },
      });
      readChannelAllowFromStore.mockResolvedValue([]);
      upsertChannelPairingRequest.mockResolvedValue({ code: "PAIRME12", created: true });
      sendMessageSpy.mockClear();
      replySpy.mockClear();
      const senderId = Number(`${Date.now()}01`.slice(-9));

      const fetchSpy = vi.spyOn(globalThis, "fetch").mockImplementation(
        async () =>
          new Response(new Uint8Array([0xff, 0xd8, 0xff, 0x00]), {
            status: 200,
            headers: { "content-type": "image/jpeg" },
          }),
      );
      const getFileSpy = vi.fn(async () => ({ file_path: "photos/p1.jpg" }));

      try {
        createTelegramBot({ token: "tok" });
        const handler = getOnHandler("message") as (ctx: Record<string, unknown>) => Promise<void>;

        await handler({
          message: {
            chat: { id: 1234, type: "private" },
            message_id: 410,
            date: 1736380800,
            photo: [{ file_id: "p1" }],
            from: { id: senderId, username: "random" },
          },
          me: { username: "deneb_bot" },
          getFile: getFileSpy,
        });

        expect(getFileSpy).not.toHaveBeenCalled();
        expect(fetchSpy).not.toHaveBeenCalled();
        expect(sendMessageSpy).toHaveBeenCalledTimes(1);
        expect(String(sendMessageSpy.mock.calls[0]?.[1])).toContain("Pairing code:");
        expect(replySpy).not.toHaveBeenCalled();
      } finally {
        fetchSpy.mockRestore();
      }
    });
  });
  it("blocks DM media downloads completely when dmPolicy is disabled", async () => {
    loadConfig.mockReturnValue({
      channels: { telegram: { dmPolicy: "disabled" } },
    });
    sendMessageSpy.mockClear();
    replySpy.mockClear();

    const fetchSpy = vi.spyOn(globalThis, "fetch").mockImplementation(
      async () =>
        new Response(new Uint8Array([0xff, 0xd8, 0xff, 0x00]), {
          status: 200,
          headers: { "content-type": "image/jpeg" },
        }),
    );
    const getFileSpy = vi.fn(async () => ({ file_path: "photos/p1.jpg" }));

    try {
      createTelegramBot({ token: "tok" });
      const handler = getOnHandler("message") as (ctx: Record<string, unknown>) => Promise<void>;

      await handler({
        message: {
          chat: { id: 1234, type: "private" },
          message_id: 411,
          date: 1736380800,
          photo: [{ file_id: "p1" }],
          from: { id: 999, username: "random" },
        },
        me: { username: "deneb_bot" },
        getFile: getFileSpy,
      });

      expect(getFileSpy).not.toHaveBeenCalled();
      expect(fetchSpy).not.toHaveBeenCalled();
      expect(sendMessageSpy).not.toHaveBeenCalled();
      expect(replySpy).not.toHaveBeenCalled();
    } finally {
      fetchSpy.mockRestore();
    }
  });
  it("blocks unauthorized DM media groups before any photo download", async () => {
    await withIsolatedStateDirAsync(async () => {
      loadConfig.mockReturnValue({
        channels: { telegram: { dmPolicy: "pairing" } },
      });
      readChannelAllowFromStore.mockResolvedValue([]);
      upsertChannelPairingRequest.mockResolvedValue({ code: "PAIRME12", created: true });
      sendMessageSpy.mockClear();
      replySpy.mockClear();
      const senderId = Number(`${Date.now()}02`.slice(-9));

      const fetchSpy = vi.spyOn(globalThis, "fetch").mockImplementation(
        async () =>
          new Response(new Uint8Array([0xff, 0xd8, 0xff, 0x00]), {
            status: 200,
            headers: { "content-type": "image/jpeg" },
          }),
      );
      const getFileSpy = vi.fn(async () => ({ file_path: "photos/p1.jpg" }));

      try {
        createTelegramBot({ token: "tok", testTimings: TELEGRAM_TEST_TIMINGS });
        const handler = getOnHandler("message") as (ctx: Record<string, unknown>) => Promise<void>;

        await handler({
          message: {
            chat: { id: 1234, type: "private" },
            message_id: 412,
            media_group_id: "dm-album-1",
            date: 1736380800,
            photo: [{ file_id: "p1" }],
            from: { id: senderId, username: "random" },
          },
          me: { username: "deneb_bot" },
          getFile: getFileSpy,
        });

        expect(getFileSpy).not.toHaveBeenCalled();
        expect(fetchSpy).not.toHaveBeenCalled();
        expect(sendMessageSpy).toHaveBeenCalledTimes(1);
        expect(String(sendMessageSpy.mock.calls[0]?.[1])).toContain("Pairing code:");
        expect(replySpy).not.toHaveBeenCalled();
      } finally {
        fetchSpy.mockRestore();
      }
    });
  });
  it("triggers typing cue via onReplyStart", async () => {
    dispatchReplyWithBufferedBlockDispatcher.mockImplementationOnce(
      async ({ dispatcherOptions }) => {
        await dispatcherOptions.typingCallbacks?.onReplyStart?.();
        return { queuedFinal: false, counts: { block: 0, final: 0, tool: 0 } };
      },
    );
    createTelegramBot({ token: "tok" });
    const handler = getOnHandler("message") as (ctx: Record<string, unknown>) => Promise<void>;
    await handler({
      message: {
        chat: { id: 42, type: "private" },
        from: { id: 999, username: "random" },
        text: "hi",
      },
      me: { username: "deneb_bot" },
      getFile: async () => ({ download: async () => new Uint8Array() }),
    });
    expect(sendChatActionSpy).toHaveBeenCalledWith(42, "typing", undefined);
  });

  it("dedupes duplicate updates for callback_query, message, and channel_post", async () => {
    loadConfig.mockReturnValue({
      channels: {
        telegram: {
          dmPolicy: "open",
          allowFrom: ["*"],
          groupPolicy: "open",
          groups: {
            "-100777111222": {
              enabled: true,
              requireMention: false,
            },
          },
        },
      },
    });

    createTelegramBot({ token: "tok" });
    const callbackHandler = getOnHandler("callback_query") as (
      ctx: Record<string, unknown>,
    ) => Promise<void>;
    const messageHandler = getOnHandler("message") as (
      ctx: Record<string, unknown>,
    ) => Promise<void>;
    const channelPostHandler = getOnHandler("channel_post") as (
      ctx: Record<string, unknown>,
    ) => Promise<void>;

    await callbackHandler({
      update: { update_id: 222 },
      callbackQuery: {
        id: "cb-1",
        data: "ping",
        from: { id: 789, username: "testuser" },
        message: {
          chat: { id: 123, type: "private" },
          date: 1736380800,
          message_id: 9001,
        },
      },
      me: { username: "deneb_bot" },
      getFile: async () => ({}),
    });
    await callbackHandler({
      update: { update_id: 222 },
      callbackQuery: {
        id: "cb-1",
        data: "ping",
        from: { id: 789, username: "testuser" },
        message: {
          chat: { id: 123, type: "private" },
          date: 1736380800,
          message_id: 9001,
        },
      },
      me: { username: "deneb_bot" },
      getFile: async () => ({}),
    });
    expect(replySpy).toHaveBeenCalledTimes(1);

    replySpy.mockClear();

    await messageHandler({
      update: { update_id: 111 },
      message: {
        chat: { id: 123, type: "private" },
        from: { id: 456, username: "testuser" },
        text: "hello",
        date: 1736380800,
        message_id: 42,
      },
      me: { username: "deneb_bot" },
      getFile: async () => ({ download: async () => new Uint8Array() }),
    });
    await messageHandler({
      update: { update_id: 111 },
      message: {
        chat: { id: 123, type: "private" },
        from: { id: 456, username: "testuser" },
        text: "hello",
        date: 1736380800,
        message_id: 42,
      },
      me: { username: "deneb_bot" },
      getFile: async () => ({ download: async () => new Uint8Array() }),
    });
    expect(replySpy).toHaveBeenCalledTimes(1);

    replySpy.mockClear();

    await channelPostHandler({
      channelPost: {
        chat: { id: -100777111222, type: "channel", title: "Wake Channel" },
        from: { id: 98765, is_bot: true, first_name: "wakebot", username: "wake_bot" },
        message_id: 777,
        text: "wake check",
        date: 1736380800,
      },
      me: { username: "deneb_bot" },
      getFile: async () => ({}),
    });
    await channelPostHandler({
      channelPost: {
        chat: { id: -100777111222, type: "channel", title: "Wake Channel" },
        from: { id: 98765, is_bot: true, first_name: "wakebot", username: "wake_bot" },
        message_id: 777,
        text: "wake check",
        date: 1736380800,
      },
      me: { username: "deneb_bot" },
      getFile: async () => ({}),
    });
    expect(replySpy).toHaveBeenCalledTimes(1);
  });

  it("does not persist update offset past pending updates", async () => {
    // For this test we need sequentialize(...) to behave like a normal middleware and call next().
    sequentializeSpy.mockImplementationOnce(
      () => async (_ctx: unknown, next: () => Promise<void>) => {
        await next();
      },
    );

    const onUpdateId = vi.fn();
    loadConfig.mockReturnValue({
      channels: { telegram: { dmPolicy: "open", allowFrom: ["*"] } },
    });

    createTelegramBot({
      token: "tok",
      updateOffset: {
        lastUpdateId: 100,
        onUpdateId,
      },
    });

    type Middleware = (
      ctx: Record<string, unknown>,
      next: () => Promise<void>,
    ) => Promise<void> | void;

    const middlewares = middlewareUseSpy.mock.calls
      .map((call) => call[0])
      .filter((fn): fn is Middleware => typeof fn === "function");

    const runMiddlewareChain = async (
      ctx: Record<string, unknown>,
      finalNext: () => Promise<void>,
    ) => {
      let idx = -1;
      const dispatch = async (i: number): Promise<void> => {
        if (i <= idx) {
          throw new Error("middleware dispatch called multiple times");
        }
        idx = i;
        const fn = middlewares[i];
        if (!fn) {
          await finalNext();
          return;
        }
        await fn(ctx, async () => dispatch(i + 1));
      };
      await dispatch(0);
    };

    let releaseUpdate101: (() => void) | undefined;
    const update101Gate = new Promise<void>((resolve) => {
      releaseUpdate101 = resolve;
    });

    // Start processing update 101 but keep it pending (simulates an update queued behind sequentialize()).
    const p101 = runMiddlewareChain({ update: { update_id: 101 } }, async () => update101Gate);
    // Let update 101 enter the chain and mark itself pending before 102 completes.
    await Promise.resolve();

    // Complete update 102 while 101 is still pending. The persisted watermark must not jump to 102.
    await runMiddlewareChain({ update: { update_id: 102 } }, async () => {});

    const persistedValues = onUpdateId.mock.calls.map((call) => Number(call[0]));
    const maxPersisted = persistedValues.length > 0 ? Math.max(...persistedValues) : -Infinity;
    expect(maxPersisted).toBeLessThan(101);

    releaseUpdate101?.();
    await p101;

    // Once the pending update finishes, the watermark can safely catch up.
    const persistedAfterDrain = onUpdateId.mock.calls.map((call) => Number(call[0]));
    const maxPersistedAfterDrain =
      persistedAfterDrain.length > 0 ? Math.max(...persistedAfterDrain) : -Infinity;
    expect(maxPersistedAfterDrain).toBe(102);
  });
});
