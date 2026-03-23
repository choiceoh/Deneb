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

describe("createTelegramBot — routing & mention gating", () => {
  beforeAll(() => {
    process.env.TZ = "UTC";
  });
  afterAll(() => {
    process.env.TZ = ORIGINAL_TZ;
  });

  it("allows distinct callback_query ids without update_id", async () => {
    loadConfig.mockReturnValue({
      channels: {
        telegram: { dmPolicy: "open", allowFrom: ["*"] },
      },
    });

    createTelegramBot({ token: "tok" });
    const handler = getOnHandler("callback_query") as (
      ctx: Record<string, unknown>,
    ) => Promise<void>;

    await handler({
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

    await handler({
      callbackQuery: {
        id: "cb-2",
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

    expect(replySpy).toHaveBeenCalledTimes(2);
  });

  const groupPolicyCases: Array<{
    name: string;
    config: Record<string, unknown>;
    message: Record<string, unknown>;
    expectedReplyCount: number;
  }> = [
    {
      name: "blocks all group messages when groupPolicy is 'disabled'",
      config: {
        channels: {
          telegram: {
            groupPolicy: "disabled",
            allowFrom: ["123456789"],
          },
        },
      },
      message: {
        chat: { id: -100123456789, type: "group", title: "Test Group" },
        from: { id: 123456789, username: "testuser" },
        text: "@deneb_bot hello",
        date: 1736380800,
      },
      expectedReplyCount: 0,
    },
    {
      name: "blocks group messages from senders not in allowFrom when groupPolicy is 'allowlist'",
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
        from: { id: 999999, username: "notallowed" },
        text: "@deneb_bot hello",
        date: 1736380800,
      },
      expectedReplyCount: 0,
    },
    {
      name: "allows group messages from senders in allowFrom (by ID) when groupPolicy is 'allowlist'",
      config: {
        channels: {
          telegram: {
            groupPolicy: "allowlist",
            allowFrom: ["123456789"],
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
      expectedReplyCount: 1,
    },
    {
      name: "blocks group messages when allowFrom is configured with @username entries (numeric IDs required)",
      config: {
        channels: {
          telegram: {
            groupPolicy: "allowlist",
            allowFrom: ["@testuser"],
            groups: { "*": { requireMention: false } },
          },
        },
      },
      message: {
        chat: { id: -100123456789, type: "group", title: "Test Group" },
        from: { id: 12345, username: "testuser" },
        text: "hello",
        date: 1736380800,
      },
      expectedReplyCount: 0,
    },
    {
      name: "allows group messages from tg:-prefixed allowFrom entries case-insensitively",
      config: {
        channels: {
          telegram: {
            groupPolicy: "allowlist",
            allowFrom: ["TG:77112533"],
            groups: { "*": { requireMention: false } },
          },
        },
      },
      message: {
        chat: { id: -100123456789, type: "group", title: "Test Group" },
        from: { id: 77112533, username: "mneves" },
        text: "hello",
        date: 1736380800,
      },
      expectedReplyCount: 1,
    },
    {
      name: "blocks group messages when per-group allowFrom override is explicitly empty",
      config: {
        channels: {
          telegram: {
            groupPolicy: "open",
            groups: {
              "-100123456789": {
                allowFrom: [],
                requireMention: false,
              },
            },
          },
        },
      },
      message: {
        chat: { id: -100123456789, type: "group", title: "Test Group" },
        from: { id: 999999, username: "random" },
        text: "hello",
        date: 1736380800,
      },
      expectedReplyCount: 0,
    },
    {
      name: "allows all group messages when groupPolicy is 'open'",
      config: {
        channels: {
          telegram: {
            groupPolicy: "open",
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
  ];

  it("applies groupPolicy cases", async () => {
    for (const [index, testCase] of groupPolicyCases.entries()) {
      resetHarnessSpies();
      loadConfig.mockReturnValue(testCase.config);
      await dispatchMessage({
        message: {
          ...testCase.message,
          message_id: 1_000 + index,
          date: 1_736_380_800 + index,
        },
      });
      expect(replySpy.mock.calls.length, testCase.name).toBe(testCase.expectedReplyCount);
    }
  });

  it("routes DMs by telegram accountId binding", async () => {
    const config = {
      channels: {
        telegram: {
          allowFrom: ["*"],
          accounts: {
            opie: {
              botToken: "tok-opie",
              dmPolicy: "open",
              allowFrom: ["*"],
            },
          },
        },
      },
      bindings: [
        {
          agentId: "opie",
          match: { channel: "telegram", accountId: "opie" },
        },
      ],
    };
    loadConfig.mockReturnValue(config);

    await withConfigPathAsync(config, async () => {
      createTelegramBot({ token: "tok", accountId: "opie" });
      const handler = getOnHandler("message") as (ctx: Record<string, unknown>) => Promise<void>;

      await handler({
        message: {
          chat: { id: 123, type: "private" },
          from: { id: 999, username: "testuser" },
          text: "hello",
          date: 1736380800,
          message_id: 42,
        },
        me: { username: "deneb_bot" },
        getFile: async () => ({ download: async () => new Uint8Array() }),
      });

      expect(replySpy).toHaveBeenCalledTimes(1);
      const payload = replySpy.mock.calls[0][0];
      expect(payload.AccountId).toBe("opie");
      expect(payload.SessionKey).toBe("agent:opie:main");
    });
  });

  it("reloads DM routing bindings between messages without recreating the bot", async () => {
    let boundAgentId = "agent-a";
    const configForAgent = (agentId: string) => ({
      channels: {
        telegram: {
          accounts: {
            opie: {
              botToken: "tok-opie",
              dmPolicy: "open",
            },
          },
        },
      },
      agents: {
        list: [{ id: "agent-a" }, { id: "agent-b" }],
      },
      bindings: [
        {
          agentId,
          match: { channel: "telegram", accountId: "opie" },
        },
      ],
    });
    loadConfig.mockImplementation(() => configForAgent(boundAgentId));

    createTelegramBot({ token: "tok", accountId: "opie" });
    const handler = getOnHandler("message") as (ctx: Record<string, unknown>) => Promise<void>;

    const sendDm = async (messageId: number, text: string) => {
      await handler({
        message: {
          chat: { id: 123, type: "private" },
          from: { id: 999, username: "testuser" },
          text,
          date: 1736380800 + messageId,
          message_id: messageId,
        },
        me: { username: "deneb_bot" },
        getFile: async () => ({ download: async () => new Uint8Array() }),
      });
    };

    await sendDm(42, "hello one");
    expect(replySpy).toHaveBeenCalledTimes(1);
    expect(replySpy.mock.calls[0]?.[0].AccountId).toBe("opie");
    expect(replySpy.mock.calls[0]?.[0].SessionKey).toContain("agent:agent-a:");

    boundAgentId = "agent-b";
    await sendDm(43, "hello two");
    expect(replySpy).toHaveBeenCalledTimes(2);
    expect(replySpy.mock.calls[1]?.[0].AccountId).toBe("opie");
    expect(replySpy.mock.calls[1]?.[0].SessionKey).toContain("agent:agent-b:");
  });

  it("reloads topic agent overrides between messages without recreating the bot", async () => {
    let topicAgentId = "topic-a";
    loadConfig.mockImplementation(() => ({
      channels: {
        telegram: {
          groupPolicy: "open",
          groups: {
            "-1001234567890": {
              requireMention: false,
              topics: {
                "99": {
                  agentId: topicAgentId,
                },
              },
            },
          },
        },
      },
      agents: {
        list: [{ id: "topic-a" }, { id: "topic-b" }],
      },
    }));

    createTelegramBot({ token: "tok" });
    const handler = getOnHandler("message") as (ctx: Record<string, unknown>) => Promise<void>;

    const sendTopicMessage = async (messageId: number) => {
      await handler({
        message: {
          chat: { id: -1001234567890, type: "supergroup", title: "Forum Group", is_forum: true },
          from: { id: 12345, username: "testuser" },
          text: "hello",
          date: 1736380800 + messageId,
          message_id: messageId,
          message_thread_id: 99,
        },
        me: { username: "deneb_bot" },
        getFile: async () => ({ download: async () => new Uint8Array() }),
      });
    };

    await sendTopicMessage(301);
    expect(replySpy).toHaveBeenCalledTimes(1);
    expect(replySpy.mock.calls[0]?.[0].SessionKey).toContain("agent:topic-a:");

    topicAgentId = "topic-b";
    await sendTopicMessage(302);
    expect(replySpy).toHaveBeenCalledTimes(2);
    expect(replySpy.mock.calls[1]?.[0].SessionKey).toContain("agent:topic-b:");
  });

  it("routes non-default account DMs to the per-account fallback session without explicit bindings", async () => {
    loadConfig.mockReturnValue({
      channels: {
        telegram: {
          accounts: {
            opie: {
              botToken: "tok-opie",
              dmPolicy: "open",
            },
          },
        },
      },
    });

    createTelegramBot({ token: "tok", accountId: "opie" });
    const handler = getOnHandler("message") as (ctx: Record<string, unknown>) => Promise<void>;

    await handler({
      message: {
        chat: { id: 123, type: "private" },
        from: { id: 999, username: "testuser" },
        text: "hello",
        date: 1736380800,
        message_id: 42,
      },
      me: { username: "deneb_bot" },
      getFile: async () => ({ download: async () => new Uint8Array() }),
    });

    expect(replySpy).toHaveBeenCalledTimes(1);
    const payload = replySpy.mock.calls[0]?.[0];
    expect(payload.AccountId).toBe("opie");
    expect(payload.SessionKey).toContain("agent:main:telegram:opie:");
  });

  it("applies group mention overrides and fallback behavior", async () => {
    const cases: Array<{
      config: Record<string, unknown>;
      message: Record<string, unknown>;
      me?: Record<string, unknown>;
    }> = [
      {
        config: {
          channels: {
            telegram: {
              groupPolicy: "open",
              groups: {
                "*": { requireMention: true },
                "123": { requireMention: false },
              },
            },
          },
        },
        message: {
          chat: { id: 123, type: "group", title: "Dev Chat" },
          text: "hello",
          date: 1736380800,
        },
      },
      {
        config: {
          channels: {
            telegram: {
              groupPolicy: "open",
              groups: {
                "*": { requireMention: true },
                "-1001234567890": {
                  requireMention: true,
                  topics: {
                    "99": { requireMention: false },
                  },
                },
              },
            },
          },
        },
        message: {
          chat: {
            id: -1001234567890,
            type: "supergroup",
            title: "Forum Group",
            is_forum: true,
          },
          text: "hello",
          date: 1736380800,
          message_thread_id: 99,
        },
      },
      {
        config: {
          channels: {
            telegram: {
              groupPolicy: "open",
              groups: { "*": { requireMention: false } },
            },
          },
        },
        message: {
          chat: { id: 456, type: "group", title: "Ops" },
          text: "hello",
          date: 1736380800,
        },
      },
      {
        config: {
          channels: {
            telegram: {
              groupPolicy: "open",
              groups: { "*": { requireMention: true } },
            },
          },
        },
        message: {
          chat: { id: 789, type: "group", title: "No Me" },
          text: "hello",
          date: 1736380800,
        },
        me: {},
      },
    ];

    for (const testCase of cases) {
      resetHarnessSpies();
      loadConfig.mockReturnValue(testCase.config);
      await dispatchMessage({
        message: testCase.message,
        me: testCase.me,
      });
      expect(replySpy).toHaveBeenCalledTimes(1);
    }
  });

  it("routes forum topics to parent or topic-specific bindings", async () => {
    const cases: Array<{
      config: Record<string, unknown>;
      expectedSessionKeyFragment: string;
      text: string;
    }> = [
      {
        config: {
          channels: {
            telegram: {
              groupPolicy: "open",
              groups: { "*": { requireMention: false } },
            },
          },
          agents: {
            list: [{ id: "forum-agent" }],
          },
          bindings: [
            {
              agentId: "forum-agent",
              match: {
                channel: "telegram",
                peer: { kind: "group", id: "-1001234567890" },
              },
            },
          ],
        },
        expectedSessionKeyFragment: "agent:forum-agent:",
        text: "hello from topic",
      },
      {
        config: {
          channels: {
            telegram: {
              groupPolicy: "open",
              groups: { "*": { requireMention: false } },
            },
          },
          agents: {
            list: [{ id: "topic-agent" }, { id: "group-agent" }],
          },
          bindings: [
            {
              agentId: "topic-agent",
              match: {
                channel: "telegram",
                peer: { kind: "group", id: "-1001234567890:topic:99" },
              },
            },
            {
              agentId: "group-agent",
              match: {
                channel: "telegram",
                peer: { kind: "group", id: "-1001234567890" },
              },
            },
          ],
        },
        expectedSessionKeyFragment: "agent:topic-agent:",
        text: "hello from topic 99",
      },
    ];

    for (const testCase of cases) {
      await withConfigPathAsync(testCase.config, async () => {
        resetHarnessSpies();
        loadConfig.mockReturnValue(testCase.config);
        await dispatchMessage({
          message: {
            chat: {
              id: -1001234567890,
              type: "supergroup",
              title: "Forum Group",
              is_forum: true,
            },
            from: { id: 999, username: "testuser" },
            text: testCase.text,
            date: 1736380800,
            message_id: 42,
            message_thread_id: 99,
          },
        });
        expect(replySpy).toHaveBeenCalledTimes(1);
        const payload = replySpy.mock.calls[0][0];
        expect(payload.SessionKey).toContain(testCase.expectedSessionKeyFragment);
      });
    }
  });

  it("sends GIF replies as animations", async () => {
    replySpy.mockResolvedValueOnce({
      text: "caption",
      mediaUrl: "https://example.com/fun",
    });
    const fetchSpy = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      new Response(Buffer.from("GIF89a"), {
        status: 200,
        headers: {
          "content-type": "image/gif",
        },
      }),
    );
    try {
      createTelegramBot({ token: "tok" });
      const handler = getOnHandler("message") as (ctx: Record<string, unknown>) => Promise<void>;

      await handler({
        message: {
          chat: { id: 1234, type: "private" },
          text: "hello world",
          date: 1736380800,
          message_id: 5,
          from: { first_name: "Ada" },
        },
        me: { username: "deneb_bot" },
        getFile: async () => ({ download: async () => new Uint8Array() }),
      });

      expect(sendAnimationSpy).toHaveBeenCalledTimes(1);
      expect(sendAnimationSpy).toHaveBeenCalledWith("1234", expect.anything(), {
        caption: "caption",
        parse_mode: "HTML",
        reply_to_message_id: undefined,
      });
      expect(sendPhotoSpy).not.toHaveBeenCalled();
    } finally {
      fetchSpy.mockRestore();
    }
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

  it("accepts mentionPatterns matches with and without unrelated mentions", async () => {
    const cases = [
      {
        name: "plain mention pattern text",
        message: {
          chat: { id: 7, type: "group", title: "Test Group" },
          text: "bert: introduce yourself",
          date: 1736380800,
          message_id: 1,
          from: { id: 9, first_name: "Ada" },
        },
        assertEnvelope: true,
      },
      {
        name: "mention pattern plus another @mention",
        message: {
          chat: { id: 7, type: "group", title: "Test Group" },
          text: "bert: hello @alice",
          entities: [{ type: "mention", offset: 12, length: 6 }],
          date: 1736380801,
          message_id: 3,
          from: { id: 9, first_name: "Ada" },
        },
        assertEnvelope: false,
      },
    ] as const;

    for (const testCase of cases) {
      resetHarnessSpies();
      loadConfig.mockReturnValue({
        agents: {
          defaults: {
            envelopeTimezone: "utc",
          },
        },
        identity: { name: "Bert" },
        messages: { groupChat: { mentionPatterns: ["\\bbert\\b"] } },
        channels: {
          telegram: {
            groupPolicy: "open",
            groups: { "*": { requireMention: true } },
          },
        },
      });

      await dispatchMessage({
        message: testCase.message,
      });

      expect(replySpy.mock.calls.length, testCase.name).toBe(1);
      const payload = replySpy.mock.calls[0][0];
      expect(payload.WasMentioned, testCase.name).toBe(true);
      if (testCase.assertEnvelope) {
        expect(payload.SenderName).toBe("Ada");
        expect(payload.SenderId).toBe("9");
        const expectedTimestamp = formatEnvelopeTimestamp(new Date("2025-01-09T00:00:00Z"));
        const timestampPattern = escapeRegExp(expectedTimestamp);
        expect(payload.Body).toMatch(
          new RegExp(`^\\[Telegram Test Group id:7 (\\+\\d+[smhd] )?${timestampPattern}\\]`),
        );
      }
    }
  });
  it("keeps group envelope headers stable (sender identity is separate)", async () => {
    resetHarnessSpies();

    loadConfig.mockReturnValue({
      agents: {
        defaults: {
          envelopeTimezone: "utc",
        },
      },
      channels: {
        telegram: {
          groupPolicy: "open",
          groups: { "*": { requireMention: false } },
        },
      },
    });

    await dispatchMessage({
      message: {
        chat: { id: 42, type: "group", title: "Ops" },
        text: "hello",
        date: 1736380800,
        message_id: 2,
        from: {
          id: 99,
          first_name: "Ada",
          last_name: "Lovelace",
          username: "ada",
        },
      },
    });

    expect(replySpy).toHaveBeenCalledTimes(1);
    const payload = replySpy.mock.calls[0][0];
    expect(payload.SenderName).toBe("Ada Lovelace");
    expect(payload.SenderId).toBe("99");
    expect(payload.SenderUsername).toBe("ada");
    const expectedTimestamp = formatEnvelopeTimestamp(new Date("2025-01-09T00:00:00Z"));
    const timestampPattern = escapeRegExp(expectedTimestamp);
    expect(payload.Body).toMatch(
      new RegExp(`^\\[Telegram Ops id:42 (\\+\\d+[smhd] )?${timestampPattern}\\]`),
    );
  });
});
