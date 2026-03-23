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

describe("createTelegramBot — topics, commands & media", () => {
  beforeAll(() => {
    process.env.TZ = "UTC";
  });
  afterAll(() => {
    process.env.TZ = ORIGINAL_TZ;
  });

  function resetHarnessSpies() {
    onSpy.mockClear();
    replySpy.mockClear();
    sendMessageSpy.mockClear();
    setMessageReactionSpy.mockClear();
    setMyCommandsSpy.mockClear();
  }
  function getMessageHandler() {
    createTelegramBot({ token: "tok" });
    return getOnHandler("message") as (ctx: Record<string, unknown>) => Promise<void>;
  }
  async function dispatchMessage(params: {
    message: Record<string, unknown>;
    me?: Record<string, unknown>;
  }) {
    const handler = getMessageHandler();
    await handler({
      message: params.message,
      me: params.me ?? { username: "deneb_bot" },
      getFile: async () => ({ download: async () => new Uint8Array() }),
    });
  }

  it("reacts to mention-gated group messages when ackReaction is enabled", async () => {
    resetHarnessSpies();

    loadConfig.mockReturnValue({
      messages: {
        ackReaction: "👀",
        ackReactionScope: "group-mentions",
        groupChat: { mentionPatterns: ["\\bbert\\b"] },
      },
      channels: {
        telegram: {
          groupPolicy: "open",
          groups: { "*": { requireMention: true } },
        },
      },
    });

    await dispatchMessage({
      message: {
        chat: { id: 7, type: "group", title: "Test Group" },
        text: "bert hello",
        date: 1736380800,
        message_id: 123,
        from: { id: 9, first_name: "Ada" },
      },
    });

    expect(setMessageReactionSpy).toHaveBeenCalledWith(7, 123, [{ type: "emoji", emoji: "👀" }]);
  });
  it("clears native commands when disabled", () => {
    resetHarnessSpies();
    loadConfig.mockReturnValue({
      commands: { native: false },
    });

    createTelegramBot({ token: "tok" });

    expect(setMyCommandsSpy).toHaveBeenCalledWith([]);
  });
  it("handles requireMention when mentions do and do not resolve", async () => {
    const cases = [
      {
        name: "mention pattern configured but no match",
        config: { messages: { groupChat: { mentionPatterns: ["\\bbert\\b"] } } },
        me: { username: "deneb_bot" },
        expectedReplyCount: 0,
        expectedWasMentioned: undefined,
      },
      {
        name: "mention detection unavailable",
        config: { messages: { groupChat: { mentionPatterns: [] } } },
        me: {},
        expectedReplyCount: 1,
        expectedWasMentioned: false,
      },
    ] as const;

    for (const [index, testCase] of cases.entries()) {
      resetHarnessSpies();
      loadConfig.mockReturnValue({
        ...testCase.config,
        channels: {
          telegram: {
            groupPolicy: "open",
            groups: { "*": { requireMention: true } },
          },
        },
      });

      await dispatchMessage({
        message: {
          chat: { id: 7, type: "group", title: "Test Group" },
          text: "hello everyone",
          date: 1_736_380_800 + index,
          message_id: 2 + index,
          from: { id: 9, first_name: "Ada" },
        },
        me: testCase.me,
      });

      expect(replySpy.mock.calls.length, testCase.name).toBe(testCase.expectedReplyCount);
      if (testCase.expectedWasMentioned != null) {
        const payload = replySpy.mock.calls[0][0];
        expect(payload.WasMentioned, testCase.name).toBe(testCase.expectedWasMentioned);
      }
    }
  });
  it("includes reply-to context when a Telegram reply is received", async () => {
    resetHarnessSpies();

    await dispatchMessage({
      message: {
        chat: { id: 7, type: "private" },
        text: "Sure, see below",
        date: 1736380800,
        reply_to_message: {
          message_id: 9001,
          text: "Can you summarize this?",
          from: { first_name: "Ada" },
        },
      },
    });

    expect(replySpy).toHaveBeenCalledTimes(1);
    const payload = replySpy.mock.calls[0][0];
    expect(payload.Body).toContain("[Replying to Ada id:9001]");
    expect(payload.Body).toContain("Can you summarize this?");
    expect(payload.ReplyToId).toBe("9001");
    expect(payload.ReplyToBody).toBe("Can you summarize this?");
    expect(payload.ReplyToSender).toBe("Ada");
  });

  it("blocks group messages for restrictive group config edge cases", async () => {
    const blockedCases = [
      {
        name: "allowlist policy with no groupAllowFrom",
        config: {
          channels: {
            telegram: {
              groupPolicy: "allowlist",
              groups: { "*": { requireMention: false } },
            },
          },
        },
        message: {
          chat: { id: -100123456789, type: "group", title: "Test Group" },
          from: { id: 123456789, username: "testuser" },
          text: "hello",
          date: 1736380800,
        },
      },
      {
        name: "groups map without wildcard",
        config: {
          channels: {
            telegram: {
              groups: {
                "123": { requireMention: false },
              },
            },
          },
        },
        message: {
          chat: { id: 456, type: "group", title: "Ops" },
          text: "@deneb_bot hello",
          date: 1736380800,
        },
      },
    ] as const;

    for (const testCase of blockedCases) {
      resetHarnessSpies();
      loadConfig.mockReturnValue(testCase.config);
      await dispatchMessage({ message: testCase.message });
      expect(replySpy.mock.calls.length, testCase.name).toBe(0);
    }
  });
  it("blocks group sender not in groupAllowFrom even when sender is paired in DM store", async () => {
    resetHarnessSpies();
    loadConfig.mockReturnValue({
      channels: {
        telegram: {
          groupPolicy: "allowlist",
          groupAllowFrom: ["222222222"],
          groups: { "*": { requireMention: false } },
        },
      },
    });
    readChannelAllowFromStore.mockResolvedValueOnce(["123456789"]);

    await dispatchMessage({
      message: {
        chat: { id: -100123456789, type: "group", title: "Test Group" },
        from: { id: 123456789, username: "testuser" },
        text: "hello",
        date: 1736380800,
      },
    });

    expect(replySpy).not.toHaveBeenCalled();
  });
  it("allows control commands with TG-prefixed groupAllowFrom entries", async () => {
    loadConfig.mockReturnValue({
      channels: {
        telegram: {
          groupPolicy: "allowlist",
          groupAllowFrom: ["  TG:123456789  "],
          groups: { "*": { requireMention: true } },
        },
      },
    });

    createTelegramBot({ token: "tok" });
    const handler = getOnHandler("message") as (ctx: Record<string, unknown>) => Promise<void>;

    await handler({
      message: {
        chat: { id: -100123456789, type: "group", title: "Test Group" },
        from: { id: 123456789, username: "testuser" },
        text: "/status",
        date: 1736380800,
      },
      me: { username: "deneb_bot" },
      getFile: async () => ({ download: async () => new Uint8Array() }),
    });

    expect(replySpy).toHaveBeenCalledTimes(1);
  });
  it("handles forum topic metadata and typing thread fallbacks", async () => {
    const forumCases = [
      {
        name: "topic-scoped forum message",
        threadId: 99,
        expectedTypingThreadId: 99,
        assertTopicMetadata: true,
      },
      {
        name: "General topic forum message",
        threadId: undefined,
        expectedTypingThreadId: 1,
        assertTopicMetadata: false,
      },
    ] as const;

    for (const testCase of forumCases) {
      resetHarnessSpies();
      sendChatActionSpy.mockClear();
      let dispatchCall:
        | {
            ctx: {
              SessionKey?: unknown;
              From?: unknown;
              MessageThreadId?: unknown;
              IsForum?: unknown;
            };
          }
        | undefined;
      dispatchReplyWithBufferedBlockDispatcher.mockImplementationOnce(async (params) => {
        dispatchCall = params as typeof dispatchCall;
        await params.dispatcherOptions.typingCallbacks?.onReplyStart?.();
        return { queuedFinal: false, counts: { block: 0, final: 0, tool: 0 } };
      });
      loadConfig.mockReturnValue({
        channels: {
          telegram: {
            groupPolicy: "open",
            groups: { "*": { requireMention: false } },
          },
        },
      });

      const handler = getMessageHandler();
      await handler(makeForumGroupMessageCtx({ threadId: testCase.threadId }));

      const payload = dispatchCall?.ctx;
      expect(payload).toBeDefined();
      if (!payload) {
        continue;
      }
      if (testCase.assertTopicMetadata) {
        expect(payload.SessionKey).toContain("telegram:group:-1001234567890:topic:99");
        expect(payload.From).toBe("telegram:group:-1001234567890:topic:99");
        expect(payload.MessageThreadId).toBe(99);
        expect(payload.IsForum).toBe(true);
      }
      expect(sendChatActionSpy).toHaveBeenCalledWith(-1001234567890, "typing", {
        message_thread_id: testCase.expectedTypingThreadId,
      });
    }
  });
  it("threads forum replies only when a topic id exists", async () => {
    const threadCases = [
      { name: "General topic reply", threadId: undefined, expectedMessageThreadId: undefined },
      { name: "topic reply", threadId: 99, expectedMessageThreadId: 99 },
    ] as const;

    for (const testCase of threadCases) {
      resetHarnessSpies();
      replySpy.mockResolvedValue({ text: "response" });
      loadConfig.mockReturnValue({
        channels: {
          telegram: {
            groupPolicy: "open",
            groups: { "*": { requireMention: false } },
          },
        },
      });

      const handler = getMessageHandler();
      await handler(makeForumGroupMessageCtx({ threadId: testCase.threadId }));

      expect(sendMessageSpy.mock.calls.length, testCase.name).toBe(1);
      const sendParams = sendMessageSpy.mock.calls[0]?.[2] as { message_thread_id?: number };
      if (testCase.expectedMessageThreadId == null) {
        expect(sendParams?.message_thread_id, testCase.name).toBeUndefined();
      } else {
        expect(sendParams?.message_thread_id, testCase.name).toBe(testCase.expectedMessageThreadId);
      }
    }
  });

  const allowFromEdgeCases: Array<{
    name: string;
    config: Record<string, unknown>;
    message: Record<string, unknown>;
    expectedReplyCount: number;
  }> = [
    {
      name: "allows direct messages regardless of groupPolicy",
      config: {
        channels: {
          telegram: {
            groupPolicy: "disabled",
            allowFrom: ["123456789"],
          },
        },
      },
      message: {
        chat: { id: 123456789, type: "private" },
        from: { id: 123456789, username: "testuser" },
        text: "hello",
        date: 1736380800,
      },
      expectedReplyCount: 1,
    },
    {
      name: "allows direct messages with tg/Telegram-prefixed allowFrom entries",
      config: {
        channels: {
          telegram: {
            allowFrom: ["  TG:123456789  "],
          },
        },
      },
      message: {
        chat: { id: 123456789, type: "private" },
        from: { id: 123456789, username: "testuser" },
        text: "hello",
        date: 1736380800,
      },
      expectedReplyCount: 1,
    },
    {
      name: "matches direct message allowFrom against sender user id when chat id differs",
      config: {
        channels: {
          telegram: {
            allowFrom: ["123456789"],
          },
        },
      },
      message: {
        chat: { id: 777777777, type: "private" },
        from: { id: 123456789, username: "testuser" },
        text: "hello",
        date: 1736380800,
      },
      expectedReplyCount: 1,
    },
    {
      name: "falls back to direct message chat id when sender user id is missing",
      config: {
        channels: {
          telegram: {
            allowFrom: ["123456789"],
          },
        },
      },
      message: {
        chat: { id: 123456789, type: "private" },
        text: "hello",
        date: 1736380800,
      },
      expectedReplyCount: 1,
    },
    {
      name: "allows group messages with wildcard in allowFrom when groupPolicy is 'allowlist'",
      config: {
        channels: {
          telegram: {
            groupPolicy: "allowlist",
            allowFrom: ["*"],
            groups: { "*": { requireMention: false } },
          },
        },
      },
      message: {
        chat: { id: -100123456789, type: "group", title: "Test Group" },
        from: { id: 999999, username: "random" },
        text: "hello",
        date: 1736380800,
      },
      expectedReplyCount: 1,
    },
    {
      name: "blocks group messages with no sender ID when groupPolicy is 'allowlist'",
      config: {
        channels: {
          telegram: {
            groupPolicy: "allowlist",
            allowFrom: ["123456789"],
          },
        },
      },
      message: {
        chat: { id: -100123456789, type: "group", title: "Test Group" },
        text: "hello",
        date: 1736380800,
      },
      expectedReplyCount: 0,
    },
  ];

  it("applies allowFrom edge cases", async () => {
    for (const [index, testCase] of allowFromEdgeCases.entries()) {
      resetHarnessSpies();
      loadConfig.mockReturnValue(testCase.config);
      await dispatchMessage({
        message: {
          ...testCase.message,
          message_id: 2_000 + index,
          date: 1_736_380_900 + index,
        },
      });
      expect(replySpy.mock.calls.length, testCase.name).toBe(testCase.expectedReplyCount);
    }
  });
  it("sends replies without native reply threading", async () => {
    replySpy.mockResolvedValue({ text: "a".repeat(4500) });

    createTelegramBot({ token: "tok" });
    const handler = getOnHandler("message") as (ctx: Record<string, unknown>) => Promise<void>;
    await handler({
      message: {
        chat: { id: 5, type: "private" },
        text: "hi",
        date: 1736380800,
        message_id: 101,
      },
      me: { username: "deneb_bot" },
      getFile: async () => ({ download: async () => new Uint8Array() }),
    });

    expect(sendMessageSpy.mock.calls.length).toBeGreaterThan(1);
    for (const call of sendMessageSpy.mock.calls) {
      expect(
        (call[2] as { reply_to_message_id?: number } | undefined)?.reply_to_message_id,
      ).toBeUndefined();
    }
  });
  it("prefixes final replies with responsePrefix", async () => {
    replySpy.mockResolvedValue({ text: "final reply" });
    loadConfig.mockReturnValue({
      channels: {
        telegram: { dmPolicy: "open", allowFrom: ["*"] },
      },
      messages: { responsePrefix: "PFX" },
    });

    createTelegramBot({ token: "tok" });
    const handler = getOnHandler("message") as (ctx: Record<string, unknown>) => Promise<void>;
    await handler({
      message: {
        chat: { id: 5, type: "private" },
        text: "hi",
        date: 1736380800,
      },
      me: { username: "deneb_bot" },
      getFile: async () => ({ download: async () => new Uint8Array() }),
    });

    expect(sendMessageSpy).toHaveBeenCalledTimes(1);
    expect(sendMessageSpy.mock.calls[0][1]).toBe("PFX final reply");
  });
  it("honors threaded replies for replyToMode=first/all", async () => {
    for (const [mode, messageId] of [
      ["first", 101],
      ["all", 102],
    ] as const) {
      onSpy.mockClear();
      sendMessageSpy.mockClear();
      replySpy.mockClear();
      replySpy.mockResolvedValue({
        text: "a".repeat(4500),
        replyToId: String(messageId),
      });

      createTelegramBot({ token: "tok", replyToMode: mode });
      const handler = getOnHandler("message") as (ctx: Record<string, unknown>) => Promise<void>;
      await handler({
        message: {
          chat: { id: 5, type: "private" },
          text: "hi",
          date: 1736380800,
          message_id: messageId,
        },
        me: { username: "deneb_bot" },
        getFile: async () => ({ download: async () => new Uint8Array() }),
      });

      expect(sendMessageSpy.mock.calls.length).toBeGreaterThan(1);
      for (const [index, call] of sendMessageSpy.mock.calls.entries()) {
        const actual = (call[2] as { reply_to_message_id?: number } | undefined)
          ?.reply_to_message_id;
        if (mode === "all" || index === 0) {
          expect(actual).toBe(messageId);
        } else {
          expect(actual).toBeUndefined();
        }
      }
    }
  });
  it("honors routed group activation from session store", async () => {
    const storeDir = fs.mkdtempSync(path.join(os.tmpdir(), "deneb-telegram-"));
    const storePath = path.join(storeDir, "sessions.json");
    fs.writeFileSync(
      storePath,
      JSON.stringify({
        "agent:ops:telegram:group:123": { groupActivation: "always" },
      }),
      "utf-8",
    );
    const config = {
      channels: {
        telegram: {
          groupPolicy: "open",
          groups: { "*": { requireMention: true } },
        },
      },
      bindings: [
        {
          agentId: "ops",
          match: {
            channel: "telegram",
            peer: { kind: "group", id: "123" },
          },
        },
      ],
      session: { store: storePath },
    };
    loadConfig.mockReturnValue(config);

    await withConfigPathAsync(config, async () => {
      createTelegramBot({ token: "tok" });
      const handler = getOnHandler("message") as (ctx: Record<string, unknown>) => Promise<void>;

      await handler({
        message: {
          chat: { id: 123, type: "group", title: "Routing" },
          from: { id: 999, username: "ops" },
          text: "hello",
          date: 1736380800,
        },
        me: { username: "deneb_bot" },
        getFile: async () => ({ download: async () => new Uint8Array() }),
      });

      expect(replySpy).toHaveBeenCalledTimes(1);
    });
  });

  it("applies topic skill filters and system prompts", async () => {
    let dispatchCall:
      | {
          ctx: {
            GroupSystemPrompt?: unknown;
          };
          replyOptions?: {
            skillFilter?: unknown;
          };
        }
      | undefined;
    dispatchReplyWithBufferedBlockDispatcher.mockImplementationOnce(async (params) => {
      dispatchCall = params as typeof dispatchCall;
      return { queuedFinal: false, counts: { block: 0, final: 0, tool: 0 } };
    });
    loadConfig.mockReturnValue({
      channels: {
        telegram: {
          groupPolicy: "open",
          groups: {
            "-1001234567890": {
              requireMention: false,
              systemPrompt: "Group prompt",
              skills: ["group-skill"],
              topics: {
                "99": {
                  skills: [],
                  systemPrompt: "Topic prompt",
                },
              },
            },
          },
        },
      },
    });

    createTelegramBot({ token: "tok" });
    const handler = getOnHandler("message") as (ctx: Record<string, unknown>) => Promise<void>;

    await handler(makeForumGroupMessageCtx({ threadId: 99 }));

    const payload = dispatchCall?.ctx;
    expect(payload).toBeDefined();
    if (!payload) {
      return;
    }
    expect(payload.GroupSystemPrompt).toBe("Group prompt\n\nTopic prompt");
    expect(dispatchCall?.replyOptions?.skillFilter).toEqual([]);
  });
  it("threads native command replies inside topics", async () => {
    commandSpy.mockClear();
    replySpy.mockResolvedValue({ text: "response" });

    loadConfig.mockReturnValue({
      commands: { native: true },
      channels: {
        telegram: {
          dmPolicy: "open",
          allowFrom: ["*"],
          groups: { "*": { requireMention: false } },
        },
      },
    });

    createTelegramBot({ token: "tok" });
    expect(commandSpy).toHaveBeenCalled();
    const handler = commandSpy.mock.calls[0][1] as (ctx: Record<string, unknown>) => Promise<void>;

    await handler({
      ...makeForumGroupMessageCtx({ threadId: 99, text: "/status" }),
      match: "",
    });

    expect(sendMessageSpy).toHaveBeenCalledWith(
      "-1001234567890",
      expect.any(String),
      expect.objectContaining({ message_thread_id: 99 }),
    );
  });
  it("reloads native command routing bindings between invocations without recreating the bot", async () => {
    commandSpy.mockClear();
    replySpy.mockClear();

    let boundAgentId = "agent-a";
    loadConfig.mockImplementation(() => ({
      commands: { native: true },
      channels: {
        telegram: {
          dmPolicy: "open",
          allowFrom: ["*"],
        },
      },
      agents: {
        list: [{ id: "agent-a" }, { id: "agent-b" }],
      },
      bindings: [
        {
          agentId: boundAgentId,
          match: { channel: "telegram", accountId: "default" },
        },
      ],
    }));

    createTelegramBot({ token: "tok" });
    const statusHandler = commandSpy.mock.calls.find((call) => call[0] === "status")?.[1] as
      | ((ctx: Record<string, unknown>) => Promise<void>)
      | undefined;
    if (!statusHandler) {
      throw new Error("status command handler missing");
    }

    const invokeStatus = async (messageId: number) => {
      await statusHandler({
        message: {
          chat: { id: 1234, type: "private" },
          from: { id: 9, username: "ada_bot" },
          text: "/status",
          date: 1736380800 + messageId,
          message_id: messageId,
        },
        match: "",
      });
    };

    await invokeStatus(401);
    expect(replySpy).toHaveBeenCalledTimes(1);
    expect(replySpy.mock.calls[0]?.[0].SessionKey).toContain("agent:agent-a:");

    boundAgentId = "agent-b";
    await invokeStatus(402);
    expect(replySpy).toHaveBeenCalledTimes(2);
    expect(replySpy.mock.calls[1]?.[0].SessionKey).toContain("agent:agent-b:");
  });
  it("skips tool summaries for native slash commands", async () => {
    commandSpy.mockClear();
    replySpy.mockImplementation(async (_ctx: MsgContext, opts?: GetReplyOptions) => {
      await opts?.onToolResult?.({ text: "tool update" });
      return { text: "final reply" };
    });

    loadConfig.mockReturnValue({
      commands: { native: true },
      channels: {
        telegram: {
          dmPolicy: "open",
          allowFrom: ["*"],
        },
      },
    });

    createTelegramBot({ token: "tok" });
    const verboseHandler = commandSpy.mock.calls.find((call) => call[0] === "verbose")?.[1] as
      | ((ctx: Record<string, unknown>) => Promise<void>)
      | undefined;
    if (!verboseHandler) {
      throw new Error("verbose command handler missing");
    }

    await verboseHandler({
      message: {
        chat: { id: 12345, type: "private" },
        from: { id: 12345, username: "testuser" },
        text: "/verbose on",
        date: 1736380800,
        message_id: 42,
      },
      match: "on",
    });

    expect(sendMessageSpy).toHaveBeenCalledTimes(1);
    expect(sendMessageSpy.mock.calls[0]?.[1]).toContain("final reply");
  });
  it("buffers channel_post media groups and processes them together", async () => {
    loadConfig.mockReturnValue({
      channels: {
        telegram: {
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

    const fetchSpy = vi.spyOn(globalThis, "fetch").mockImplementation(
      async () =>
        new Response(new Uint8Array([0x89, 0x50, 0x4e, 0x47]), {
          status: 200,
          headers: { "content-type": "image/png" },
        }),
    );

    const setTimeoutSpy = vi.spyOn(globalThis, "setTimeout");
    try {
      createTelegramBot({ token: "tok", testTimings: TELEGRAM_TEST_TIMINGS });
      const handler = getOnHandler("channel_post") as (
        ctx: Record<string, unknown>,
      ) => Promise<void>;

      const first = handler({
        channelPost: {
          chat: { id: -100777111222, type: "channel", title: "Wake Channel" },
          message_id: 201,
          caption: "album caption",
          date: 1736380800,
          media_group_id: "channel-album-1",
          photo: [{ file_id: "p1" }],
        },
        me: { username: "deneb_bot" },
        getFile: async () => ({ file_path: "photos/p1.jpg" }),
      });

      const second = handler({
        channelPost: {
          chat: { id: -100777111222, type: "channel", title: "Wake Channel" },
          message_id: 202,
          date: 1736380801,
          media_group_id: "channel-album-1",
          photo: [{ file_id: "p2" }],
        },
        me: { username: "deneb_bot" },
        getFile: async () => ({ file_path: "photos/p2.jpg" }),
      });

      await Promise.all([first, second]);
      expect(replySpy).not.toHaveBeenCalled();

      const flushTimerCallIndex = setTimeoutSpy.mock.calls.findLastIndex(
        (call) => call[1] === TELEGRAM_TEST_TIMINGS.mediaGroupFlushMs,
      );
      const flushTimer =
        flushTimerCallIndex >= 0
          ? (setTimeoutSpy.mock.calls[flushTimerCallIndex]?.[0] as (() => unknown) | undefined)
          : undefined;
      // Cancel the real timer so it cannot fire a second time after we manually invoke it.
      if (flushTimerCallIndex >= 0) {
        clearTimeout(
          setTimeoutSpy.mock.results[flushTimerCallIndex]?.value as ReturnType<typeof setTimeout>,
        );
      }
      expect(flushTimer).toBeTypeOf("function");
      await flushTimer?.();

      expect(replySpy).toHaveBeenCalledTimes(1);
      const payload = replySpy.mock.calls[0]?.[0] as { Body?: string };
      expect(payload.Body).toContain("album caption");
    } finally {
      setTimeoutSpy.mockRestore();
      fetchSpy.mockRestore();
    }
  });
  it("coalesces channel_post near-limit text fragments into one message", async () => {
    loadConfig.mockReturnValue({
      channels: {
        telegram: {
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

    useFrozenTime("2026-02-20T00:00:00.000Z");
    try {
      createTelegramBot({ token: "tok", testTimings: TELEGRAM_TEST_TIMINGS });
      const handler = getOnHandler("channel_post") as (
        ctx: Record<string, unknown>,
      ) => Promise<void>;

      const part1 = "A".repeat(4050);
      const part2 = "B".repeat(50);

      await handler({
        channelPost: {
          chat: { id: -100777111222, type: "channel", title: "Wake Channel" },
          message_id: 301,
          date: 1736380800,
          text: part1,
        },
        me: { username: "deneb_bot" },
        getFile: async () => ({}),
      });

      await handler({
        channelPost: {
          chat: { id: -100777111222, type: "channel", title: "Wake Channel" },
          message_id: 302,
          date: 1736380801,
          text: part2,
        },
        me: { username: "deneb_bot" },
        getFile: async () => ({}),
      });

      expect(replySpy).not.toHaveBeenCalled();
      await vi.advanceTimersByTimeAsync(TELEGRAM_TEST_TIMINGS.textFragmentGapMs + 100);

      expect(replySpy).toHaveBeenCalledTimes(1);
      const payload = replySpy.mock.calls[0]?.[0] as { RawBody?: string };
      expect(payload.RawBody).toContain(part1.slice(0, 32));
      expect(payload.RawBody).toContain(part2.slice(0, 32));
    } finally {
      useRealTime();
    }
  });
  it("drops oversized channel_post media instead of dispatching a placeholder message", async () => {
    loadConfig.mockReturnValue({
      channels: {
        telegram: {
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

    const fetchSpy = vi.spyOn(globalThis, "fetch").mockImplementation(
      async () =>
        new Response(new Uint8Array([0xff, 0xd8, 0xff, 0x00]), {
          status: 200,
          headers: { "content-type": "image/jpeg" },
        }),
    );

    createTelegramBot({ token: "tok", mediaMaxMb: 0 });
    const handler = getOnHandler("channel_post") as (ctx: Record<string, unknown>) => Promise<void>;

    await handler({
      channelPost: {
        chat: { id: -100777111222, type: "channel", title: "Wake Channel" },
        message_id: 401,
        date: 1736380800,
        photo: [{ file_id: "oversized" }],
      },
      me: { username: "deneb_bot" },
      getFile: async () => ({ file_path: "photos/oversized.jpg" }),
    });

    expect(replySpy).not.toHaveBeenCalled();
    fetchSpy.mockRestore();
  });
  it("notifies users when media download fails for direct messages", async () => {
    loadConfig.mockReturnValue({
      channels: {
        telegram: { dmPolicy: "open", allowFrom: ["*"] },
      },
    });
    sendMessageSpy.mockClear();
    replySpy.mockClear();
    const fetchSpy = vi
      .spyOn(globalThis, "fetch")
      .mockImplementation(async () =>
        Promise.reject(new Error("MediaFetchError: Failed to fetch media")),
      );

    try {
      createTelegramBot({ token: "tok" });
      const handler = getOnHandler("message") as (ctx: Record<string, unknown>) => Promise<void>;

      await handler({
        message: {
          chat: { id: 1234, type: "private" },
          message_id: 411,
          date: 1736380800,
          photo: [{ file_id: "p1" }],
          from: { id: 55, is_bot: false, first_name: "u" },
        },
        me: { username: "deneb_bot" },
        getFile: async () => ({ file_path: "photos/p1.jpg" }),
      });

      expect(sendMessageSpy).toHaveBeenCalledWith(
        1234,
        "⚠️ Failed to download media. Please try again.",
        { reply_to_message_id: 411 },
      );
      expect(replySpy).not.toHaveBeenCalled();
    } finally {
      fetchSpy.mockRestore();
    }
  });
  it("processes remaining media group photos when one photo download fails", async () => {
    onSpy.mockReset();
    replySpy.mockReset();

    loadConfig.mockReturnValue({
      channels: {
        telegram: {
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

    let fetchCallIndex = 0;
    const fetchSpy = vi.spyOn(globalThis, "fetch").mockImplementation(async () => {
      fetchCallIndex++;
      if (fetchCallIndex === 2) {
        throw new Error("MediaFetchError: Failed to fetch media");
      }
      return new Response(new Uint8Array([0x89, 0x50, 0x4e, 0x47]), {
        status: 200,
        headers: { "content-type": "image/png" },
      });
    });

    const setTimeoutSpy = vi.spyOn(globalThis, "setTimeout");
    try {
      createTelegramBot({ token: "tok", testTimings: TELEGRAM_TEST_TIMINGS });
      const handler = getOnHandler("channel_post") as (
        ctx: Record<string, unknown>,
      ) => Promise<void>;

      const first = handler({
        channelPost: {
          chat: { id: -100777111222, type: "channel", title: "Wake Channel" },
          message_id: 401,
          caption: "partial album",
          date: 1736380800,
          media_group_id: "partial-album-1",
          photo: [{ file_id: "p1" }],
        },
        me: { username: "deneb_bot" },
        getFile: async () => ({ file_path: "photos/p1.jpg" }),
      });

      const second = handler({
        channelPost: {
          chat: { id: -100777111222, type: "channel", title: "Wake Channel" },
          message_id: 402,
          date: 1736380801,
          media_group_id: "partial-album-1",
          photo: [{ file_id: "p2" }],
        },
        me: { username: "deneb_bot" },
        getFile: async () => ({ file_path: "photos/p2.jpg" }),
      });

      await Promise.all([first, second]);
      expect(replySpy).not.toHaveBeenCalled();

      const flushTimerCallIndex = setTimeoutSpy.mock.calls.findLastIndex(
        (call) => call[1] === TELEGRAM_TEST_TIMINGS.mediaGroupFlushMs,
      );
      const flushTimer =
        flushTimerCallIndex >= 0
          ? (setTimeoutSpy.mock.calls[flushTimerCallIndex]?.[0] as (() => unknown) | undefined)
          : undefined;
      // Cancel the real timer so it cannot fire a second time after we manually invoke it.
      if (flushTimerCallIndex >= 0) {
        clearTimeout(
          setTimeoutSpy.mock.results[flushTimerCallIndex]?.value as ReturnType<typeof setTimeout>,
        );
      }
      expect(flushTimer).toBeTypeOf("function");
      await flushTimer?.();

      expect(replySpy).toHaveBeenCalledTimes(1);
      const payload = replySpy.mock.calls[0]?.[0] as { Body?: string };
      expect(payload.Body).toContain("partial album");
    } finally {
      setTimeoutSpy.mockRestore();
      fetchSpy.mockRestore();
    }
  });
  it("drops the media group when a non-recoverable media error occurs", async () => {
    onSpy.mockReset();
    replySpy.mockReset();

    loadConfig.mockReturnValue({
      channels: {
        telegram: {
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

    const fetchSpy = vi.spyOn(globalThis, "fetch").mockImplementation(
      async () =>
        new Response(new Uint8Array([0x89, 0x50, 0x4e, 0x47]), {
          status: 200,
          headers: { "content-type": "image/png" },
        }),
    );

    const setTimeoutSpy = vi.spyOn(globalThis, "setTimeout");
    try {
      createTelegramBot({ token: "tok", testTimings: TELEGRAM_TEST_TIMINGS });
      const handler = getOnHandler("channel_post") as (
        ctx: Record<string, unknown>,
      ) => Promise<void>;

      const first = handler({
        channelPost: {
          chat: { id: -100777111222, type: "channel", title: "Wake Channel" },
          message_id: 501,
          caption: "fatal album",
          date: 1736380800,
          media_group_id: "fatal-album-1",
          photo: [{ file_id: "p1" }],
        },
        me: { username: "deneb_bot" },
        getFile: async () => ({ file_path: "photos/p1.jpg" }),
      });

      const second = handler({
        channelPost: {
          chat: { id: -100777111222, type: "channel", title: "Wake Channel" },
          message_id: 502,
          date: 1736380801,
          media_group_id: "fatal-album-1",
          photo: [{ file_id: "p2" }],
        },
        me: { username: "deneb_bot" },
        getFile: async () => ({}),
      });

      await Promise.all([first, second]);
      expect(replySpy).not.toHaveBeenCalled();

      const flushTimerCallIndex = setTimeoutSpy.mock.calls.findLastIndex(
        (call) => call[1] === TELEGRAM_TEST_TIMINGS.mediaGroupFlushMs,
      );
      const flushTimer =
        flushTimerCallIndex >= 0
          ? (setTimeoutSpy.mock.calls[flushTimerCallIndex]?.[0] as (() => unknown) | undefined)
          : undefined;
      // Cancel the real timer so it cannot fire a second time after we manually invoke it.
      if (flushTimerCallIndex >= 0) {
        clearTimeout(
          setTimeoutSpy.mock.results[flushTimerCallIndex]?.value as ReturnType<typeof setTimeout>,
        );
      }
      expect(flushTimer).toBeTypeOf("function");
      await flushTimer?.();

      expect(replySpy).not.toHaveBeenCalled();
    } finally {
      setTimeoutSpy.mockRestore();
      fetchSpy.mockRestore();
    }
  });
  it("dedupes duplicate message updates by update_id", async () => {
    onSpy.mockReset();
    replySpy.mockReset();

    loadConfig.mockReturnValue({
      channels: {
        telegram: { dmPolicy: "open", allowFrom: ["*"] },
      },
    });

    createTelegramBot({ token: "tok" });
    const handler = getOnHandler("message") as (ctx: Record<string, unknown>) => Promise<void>;

    const ctx = {
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
    };

    await handler(ctx);
    await handler(ctx);

    expect(replySpy).toHaveBeenCalledTimes(1);
  });
});
