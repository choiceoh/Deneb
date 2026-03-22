import {
  buildTelegramTopicConversationId,
  normalizeConversationText,
  parseTelegramChatIdFromTarget,
} from "../../../acp/conversation-id.js";
import { resolveConversationIdFromTargets } from "../../../infra/outbound/conversation-id.js";
import type { HandleCommandsParams } from "../commands-types.js";
import { resolveTelegramConversationId } from "../telegram-context.js";

export function resolveAcpCommandChannel(params: HandleCommandsParams): string {
  const raw =
    params.ctx.OriginatingChannel ??
    params.command.channel ??
    params.ctx.Surface ??
    params.ctx.Provider;
  return normalizeConversationText(raw).toLowerCase();
}

export function resolveAcpCommandAccountId(params: HandleCommandsParams): string {
  const accountId = normalizeConversationText(params.ctx.AccountId);
  return accountId || "default";
}

export function resolveAcpCommandThreadId(params: HandleCommandsParams): string | undefined {
  const threadId =
    params.ctx.MessageThreadId != null
      ? normalizeConversationText(String(params.ctx.MessageThreadId))
      : "";
  return threadId || undefined;
}

export function resolveAcpCommandConversationId(params: HandleCommandsParams): string | undefined {
  const channel = resolveAcpCommandChannel(params);
  if (channel === "telegram") {
    const telegramConversationId = resolveTelegramConversationId({
      ctx: {
        MessageThreadId: params.ctx.MessageThreadId,
        OriginatingTo: params.ctx.OriginatingTo,
        To: params.ctx.To,
      },
      command: {
        to: params.command.to,
      },
    });
    if (telegramConversationId) {
      return telegramConversationId;
    }
    const threadId = resolveAcpCommandThreadId(params);
    const parentConversationId = resolveAcpCommandParentConversationId(params);
    if (threadId && parentConversationId) {
      return (
        buildTelegramTopicConversationId({
          chatId: parentConversationId,
          topicId: threadId,
        }) ?? threadId
      );
    }
  }
  return resolveConversationIdFromTargets({
    threadId: params.ctx.MessageThreadId,
    targets: [params.ctx.OriginatingTo, params.command.to, params.ctx.To],
  });
}

export function resolveAcpCommandParentConversationId(
  params: HandleCommandsParams,
): string | undefined {
  const channel = resolveAcpCommandChannel(params);
  if (channel === "telegram") {
    return (
      parseTelegramChatIdFromTarget(params.ctx.OriginatingTo) ??
      parseTelegramChatIdFromTarget(params.command.to) ??
      parseTelegramChatIdFromTarget(params.ctx.To)
    );
  }
  return undefined;
}

export function resolveAcpCommandBindingContext(params: HandleCommandsParams): {
  channel: string;
  accountId: string;
  threadId?: string;
  conversationId?: string;
  parentConversationId?: string;
} {
  const parentConversationId = resolveAcpCommandParentConversationId(params);
  return {
    channel: resolveAcpCommandChannel(params),
    accountId: resolveAcpCommandAccountId(params),
    threadId: resolveAcpCommandThreadId(params),
    conversationId: resolveAcpCommandConversationId(params),
    ...(parentConversationId ? { parentConversationId } : {}),
  };
}
