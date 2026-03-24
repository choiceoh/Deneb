import type { Message } from "@grammyjs/types";
import type { DmPolicy } from "deneb/plugin-sdk/config-runtime";
import { upsertChannelPairingRequest } from "deneb/plugin-sdk/conversation-runtime";

function createChannelPairingChallengeIssuer(opts: {
  channel: string;
  upsertPairingRequest: (params: {
    channel: string;
    id: string;
    accountId: string;
    meta?: unknown;
  }) => Promise<{ code: string; created: boolean } | void>;
}) {
  return async (params: {
    senderId: string;
    senderIdLine: string;
    meta?: unknown;
    onCreated?: () => void;
    sendPairingReply: (text: string) => Promise<void>;
    onReplyError?: (err: unknown) => void;
  }) => {
    try {
      const result = await opts.upsertPairingRequest({
        channel: opts.channel,
        id: params.senderId,
        accountId: params.senderId,
        meta: params.meta,
      });
      const code = result && "code" in result ? result.code : undefined;
      const created = result && "created" in result ? result.created : true;
      if (code && created) {
        params.onCreated?.();
        const lines = [
          params.senderIdLine,
          `Pairing code: ${code}`,
          `Run: deneb pairing approve ${opts.channel} ${code}`,
        ];
        await params.sendPairingReply(lines.join("\n"));
      }
    } catch (err) {
      (params.onReplyError ?? (() => {}))(err);
    }
  };
}
import { logVerbose } from "deneb/plugin-sdk/runtime-env";
import type { Bot } from "grammy";
import { withTelegramApiErrorLogging } from "./api-logging.js";
import { resolveSenderAllowMatch, type NormalizedAllowFrom } from "./bot-access.js";

type TelegramDmAccessLogger = {
  info: (obj: Record<string, unknown>, msg: string) => void;
};

type TelegramSenderIdentity = {
  username: string;
  userId: string | null;
  candidateId: string;
  firstName?: string;
  lastName?: string;
};

function resolveTelegramSenderIdentity(msg: Message, chatId: number): TelegramSenderIdentity {
  const from = msg.from;
  const userId = from?.id != null ? String(from.id) : null;
  return {
    username: from?.username ?? "",
    userId,
    candidateId: userId ?? String(chatId),
    firstName: from?.first_name,
    lastName: from?.last_name,
  };
}

export async function enforceTelegramDmAccess(params: {
  isGroup: boolean;
  dmPolicy: DmPolicy;
  msg: Message;
  chatId: number;
  effectiveDmAllow: NormalizedAllowFrom;
  accountId: string;
  bot: Bot;
  logger: TelegramDmAccessLogger;
  upsertPairingRequest?: typeof upsertChannelPairingRequest;
}): Promise<boolean> {
  const {
    isGroup,
    dmPolicy,
    msg,
    chatId,
    effectiveDmAllow,
    accountId,
    bot,
    logger,
    upsertPairingRequest,
  } = params;
  if (isGroup) {
    return true;
  }
  if (dmPolicy === "disabled") {
    return false;
  }
  if (dmPolicy === "open") {
    return true;
  }

  const sender = resolveTelegramSenderIdentity(msg, chatId);
  const allowMatch = resolveSenderAllowMatch({
    allow: effectiveDmAllow,
    senderId: sender.candidateId,
    senderUsername: sender.username,
  });
  const allowMatchMeta = `matchKey=${allowMatch.matchKey ?? "none"} matchSource=${
    allowMatch.matchSource ?? "none"
  }`;
  const allowed =
    effectiveDmAllow.hasWildcard || (effectiveDmAllow.hasEntries && allowMatch.allowed);
  if (allowed) {
    return true;
  }

  if (dmPolicy === "pairing") {
    try {
      const telegramUserId = sender.userId ?? sender.candidateId;
      await createChannelPairingChallengeIssuer({
        channel: "telegram",
        upsertPairingRequest: async ({ id, meta }) =>
          (await (upsertPairingRequest ?? upsertChannelPairingRequest)({
            channel: "telegram",
            id,
            accountId,
            meta,
          })) as { code: string; created: boolean } | void,
      })({
        senderId: telegramUserId,
        senderIdLine: `Your Telegram user id: ${telegramUserId}`,
        meta: {
          username: sender.username || undefined,
          firstName: sender.firstName,
          lastName: sender.lastName,
        },
        onCreated: () => {
          logger.info(
            {
              chatId: String(chatId),
              senderUserId: sender.userId ?? undefined,
              username: sender.username || undefined,
              firstName: sender.firstName,
              lastName: sender.lastName,
              matchKey: allowMatch.matchKey ?? "none",
              matchSource: allowMatch.matchSource ?? "none",
            },
            "telegram pairing request",
          );
        },
        sendPairingReply: async (text) => {
          await withTelegramApiErrorLogging({
            operation: "sendMessage",
            fn: () => bot.api.sendMessage(chatId, text),
          });
        },
        onReplyError: (err) => {
          logVerbose(`telegram pairing reply failed for chat ${chatId}: ${String(err)}`);
        },
      });
    } catch (err) {
      logVerbose(`telegram pairing reply failed for chat ${chatId}: ${String(err)}`);
    }
    return false;
  }

  logVerbose(
    `Blocked unauthorized telegram sender ${sender.candidateId} (dmPolicy=${dmPolicy}, ${allowMatchMeta})`,
  );
  return false;
}
