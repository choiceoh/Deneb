import type { ReplyToMode } from "deneb/plugin-sdk/config-runtime";
import type { TelegramAccountConfig } from "deneb/plugin-sdk/config-runtime";
import { danger, logVerbose } from "deneb/plugin-sdk/runtime-env";
import type { RuntimeEnv } from "deneb/plugin-sdk/runtime-env";
import type { TelegramBotDeps } from "./bot-deps.js";
import {
  buildTelegramMessageContext,
  type BuildTelegramMessageContextParams,
  type TelegramMediaRef,
} from "./bot-message-context.js";
import { dispatchTelegramMessage } from "./bot-message-dispatch.js";
import type { TelegramBotOptions } from "./bot.js";
import type { TelegramContext, TelegramStreamMode } from "./bot/types.js";

/** Dependencies injected once when creating the message processor. */
type TelegramMessageProcessorDeps = Omit<
  BuildTelegramMessageContextParams,
  "primaryCtx" | "allMedia" | "storeAllowFrom" | "options"
> & {
  telegramCfg: TelegramAccountConfig;
  runtime: RuntimeEnv;
  replyToMode: ReplyToMode;
  streamMode: TelegramStreamMode;
  textLimit: number;
  telegramDeps: TelegramBotDeps;
  opts: Pick<TelegramBotOptions, "token">;
};

export const createTelegramMessageProcessor = (deps: TelegramMessageProcessorDeps) => {
  const {
    bot,
    cfg,
    account,
    telegramCfg,
    historyLimit,
    groupHistories,
    dmPolicy,
    allowFrom,
    groupAllowFrom,
    ackReactionScope,
    logger,
    resolveGroupActivation,
    resolveGroupRequireMention,
    resolveTelegramGroupConfig,
    loadFreshConfig,
    sendChatActionHandler,
    runtime,
    replyToMode,
    streamMode,
    textLimit,
    telegramDeps,
    opts,
  } = deps;

  return async (
    primaryCtx: TelegramContext,
    allMedia: TelegramMediaRef[],
    storeAllowFrom: string[],
    options?: { messageIdOverride?: string; forceWasMentioned?: boolean },
    replyMedia?: TelegramMediaRef[],
  ) => {
    const context = await buildTelegramMessageContext({
      primaryCtx,
      allMedia,
      replyMedia,
      storeAllowFrom,
      options,
      bot,
      cfg,
      account,
      historyLimit,
      groupHistories,
      dmPolicy,
      allowFrom,
      groupAllowFrom,
      ackReactionScope,
      logger,
      resolveGroupActivation,
      resolveGroupRequireMention,
      resolveTelegramGroupConfig,
      sendChatActionHandler,
      loadFreshConfig,
      upsertPairingRequest: telegramDeps.upsertChannelPairingRequest,
    });
    if (!context) {
      const chatId = primaryCtx.message?.chat?.id;
      const text = (primaryCtx.message?.text ?? primaryCtx.message?.caption ?? "").slice(0, 80);
      logVerbose(
        `telegram: processMessage dropped (no context) chatId=${chatId ?? "unknown"} text="${text}"`,
      );
      return;
    }
    try {
      await dispatchTelegramMessage({
        context,
        bot,
        cfg,
        runtime,
        replyToMode,
        streamMode,
        textLimit,
        telegramCfg,
        telegramDeps,
        opts,
      });
    } catch (err) {
      runtime.error?.(danger(`telegram message processing failed: ${String(err)}`));
      try {
        await bot.api.sendMessage(
          context.chatId,
          "Something went wrong while processing your request. Please try again.",
          context.threadSpec?.id != null ? { message_thread_id: context.threadSpec.id } : undefined,
        );
      } catch {
        // Best-effort fallback; delivery may fail if the bot was blocked or the chat is invalid.
      }
    }
  };
};
