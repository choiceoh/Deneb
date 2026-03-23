import type { InlineKeyboardButton, InlineKeyboardMarkup } from "@grammyjs/types";
import { resolveMarkdownTableMode } from "deneb/plugin-sdk/config-runtime";
import { recordChannelActivity } from "deneb/plugin-sdk/infra-runtime";
import { formatErrorMessage } from "deneb/plugin-sdk/infra-runtime";
import type { MediaKind } from "deneb/plugin-sdk/media-runtime";
import { buildOutboundMediaLoadOptions } from "deneb/plugin-sdk/media-runtime";
import { isGifMedia, kindFromMime } from "deneb/plugin-sdk/media-runtime";
import { logVerbose } from "deneb/plugin-sdk/runtime-env";
import { loadWebMedia } from "deneb/plugin-sdk/web-media";
import { InputFile } from "grammy";
import type { TelegramInlineButtons } from "./button-types.js";
import { splitTelegramCaption } from "./caption.js";
import { renderTelegramHtmlText, splitTelegramHtmlChunks } from "./format.js";
import {
  type TelegramApi,
  type TelegramMessageLike,
  type TelegramSendOpts,
  type TelegramSendResult,
  buildTelegramThreadReplyParams,
  createRequestWithChatNotFound,
  createTelegramNonIdempotentRequestWithDiag,
  resolveTelegramApiContext,
  resolveTelegramMessageIdOrThrow,
  resolveAndPersistChatId,
  splitTelegramPlainTextChunks,
  splitTelegramPlainTextFallback,
  withTelegramHtmlParseFallback,
  withTelegramThreadFallback,
  parseTelegramTarget,
} from "./send-infra.js";
import { recordSentMessage } from "./sent-message-cache.js";
import { resolveTelegramVoiceSend } from "./voice.js";

export function buildInlineKeyboard(
  buttons?: TelegramSendOpts["buttons"],
): InlineKeyboardMarkup | undefined {
  if (!buttons?.length) {
    return undefined;
  }
  const rows = buttons
    .map((row) =>
      row
        .filter((button) => button?.text && button?.callback_data)
        .map(
          (button): InlineKeyboardButton => ({
            text: button.text,
            callback_data: button.callback_data,
            ...(button.style ? { style: button.style } : {}),
          }),
        ),
    )
    .filter((row) => row.length > 0);
  if (rows.length === 0) {
    return undefined;
  }
  return { inline_keyboard: rows };
}

function inferFilename(kind: MediaKind) {
  switch (kind) {
    case "image":
      return "image.jpg";
    case "video":
      return "video.mp4";
    case "audio":
      return "audio.ogg";
    default:
      return "file.bin";
  }
}

export async function sendMessageTelegram(
  to: string,
  text: string,
  opts: TelegramSendOpts = {},
): Promise<TelegramSendResult> {
  const { cfg, account, api } = resolveTelegramApiContext(opts);
  const target = parseTelegramTarget(to);
  const chatId = await resolveAndPersistChatId({
    cfg,
    api,
    lookupTarget: target.chatId,
    persistTarget: to,
    verbose: opts.verbose,
  });
  const mediaUrl = opts.mediaUrl?.trim();
  const mediaMaxBytes =
    opts.maxBytes ??
    (typeof account.config.mediaMaxMb === "number" ? account.config.mediaMaxMb : 100) * 1024 * 1024;
  const replyMarkup = buildInlineKeyboard(opts.buttons);

  const threadParams = buildTelegramThreadReplyParams({
    targetMessageThreadId: target.messageThreadId,
    messageThreadId: opts.messageThreadId,
    chatType: target.chatType,
    replyToMessageId: opts.replyToMessageId,
    quoteText: opts.quoteText,
  });
  const hasThreadParams = Object.keys(threadParams).length > 0;
  const requestWithDiag = createTelegramNonIdempotentRequestWithDiag({
    cfg,
    account,
    retry: opts.retry,
    verbose: opts.verbose,
  });
  const requestWithChatNotFound = createRequestWithChatNotFound({
    requestWithDiag,
    chatId,
    input: to,
  });

  const textMode = opts.textMode ?? "markdown";
  const tableMode = resolveMarkdownTableMode({
    cfg,
    channel: "telegram",
    accountId: account.accountId,
  });
  const renderHtmlText = (value: string) => renderTelegramHtmlText(value, { textMode, tableMode });

  // Resolve link preview setting from config (default: enabled).
  const linkPreviewEnabled = account.config.linkPreview ?? true;
  const linkPreviewOptions = linkPreviewEnabled ? undefined : { is_disabled: true };

  type TelegramTextChunk = {
    plainText: string;
    htmlText?: string;
  };

  const sendTelegramTextChunk = async (
    chunk: TelegramTextChunk,
    params?: Record<string, unknown>,
  ) => {
    return await withTelegramThreadFallback(
      params,
      "message",
      opts.verbose,
      async (effectiveParams, label) => {
        const baseParams = effectiveParams ? { ...effectiveParams } : {};
        if (linkPreviewOptions) {
          baseParams.link_preview_options = linkPreviewOptions;
        }
        const plainParams = {
          ...baseParams,
          ...(opts.silent === true ? { disable_notification: true } : {}),
        };
        const hasPlainParams = Object.keys(plainParams).length > 0;
        const requestPlain = (retryLabel: string) =>
          requestWithChatNotFound(
            () =>
              hasPlainParams
                ? api.sendMessage(
                    chatId,
                    chunk.plainText,
                    plainParams as Parameters<typeof api.sendMessage>[2],
                  )
                : api.sendMessage(chatId, chunk.plainText),
            retryLabel,
          );
        if (!chunk.htmlText) {
          return await requestPlain(label);
        }
        const htmlText = chunk.htmlText;
        const htmlParams = {
          parse_mode: "HTML" as const,
          ...plainParams,
        };
        return await withTelegramHtmlParseFallback({
          label,
          verbose: opts.verbose,
          requestHtml: (retryLabel) =>
            requestWithChatNotFound(
              () =>
                api.sendMessage(
                  chatId,
                  htmlText,
                  htmlParams as Parameters<typeof api.sendMessage>[2],
                ),
              retryLabel,
            ),
          requestPlain,
        });
      },
    );
  };

  const buildTextParams = (isLastChunk: boolean) =>
    hasThreadParams || (isLastChunk && replyMarkup)
      ? {
          ...threadParams,
          ...(isLastChunk && replyMarkup ? { reply_markup: replyMarkup } : {}),
        }
      : undefined;

  const sendTelegramTextChunks = async (
    chunks: TelegramTextChunk[],
    context: string,
  ): Promise<{ messageId: string; chatId: string }> => {
    let lastMessageId = "";
    let lastChatId = chatId;
    for (let index = 0; index < chunks.length; index += 1) {
      const chunk = chunks[index];
      if (!chunk) {
        continue;
      }
      const res = await sendTelegramTextChunk(chunk, buildTextParams(index === chunks.length - 1));
      const messageId = resolveTelegramMessageIdOrThrow(res, context);
      recordSentMessage(chatId, messageId);
      lastMessageId = String(messageId);
      lastChatId = String(res?.chat?.id ?? chatId);
    }
    return { messageId: lastMessageId, chatId: lastChatId };
  };

  const buildChunkedTextPlan = (rawText: string, context: string): TelegramTextChunk[] => {
    const fallbackText = opts.plainText ?? rawText;
    let htmlChunks: string[];
    try {
      htmlChunks = splitTelegramHtmlChunks(rawText, 4000);
    } catch (error) {
      logVerbose(
        `telegram ${context} failed HTML chunk planning, retrying as plain text: ${formatErrorMessage(
          error,
        )}`,
      );
      return splitTelegramPlainTextChunks(fallbackText, 4000).map((plainText) => ({ plainText }));
    }
    const fixedPlainTextChunks = splitTelegramPlainTextChunks(fallbackText, 4000);
    if (fixedPlainTextChunks.length > htmlChunks.length) {
      logVerbose(
        `telegram ${context} plain-text fallback needs more chunks than HTML; sending plain text`,
      );
      return fixedPlainTextChunks.map((plainText) => ({ plainText }));
    }
    const plainTextChunks = splitTelegramPlainTextFallback(fallbackText, htmlChunks.length, 4000);
    return htmlChunks.map((htmlText, index) => ({
      htmlText,
      plainText: plainTextChunks[index] ?? htmlText,
    }));
  };

  const sendChunkedText = async (rawText: string, context: string) =>
    await sendTelegramTextChunks(buildChunkedTextPlan(rawText, context), context);

  if (mediaUrl) {
    const media = await loadWebMedia(
      mediaUrl,
      buildOutboundMediaLoadOptions({
        maxBytes: mediaMaxBytes,
        mediaLocalRoots: opts.mediaLocalRoots,
        optimizeImages: opts.forceDocument ? false : undefined,
      }),
    );
    const kind = kindFromMime(media.contentType ?? undefined);
    const isGif = isGifMedia({
      contentType: media.contentType,
      fileName: media.fileName,
    });
    const isVideoNote = kind === "video" && opts.asVideoNote === true;
    const fileName =
      media.fileName ?? (isGif ? "animation.gif" : inferFilename(kind ?? "document")) ?? "file";
    const file = new InputFile(media.buffer, fileName);
    let caption: string | undefined;
    let followUpText: string | undefined;

    if (isVideoNote) {
      caption = undefined;
      followUpText = text.trim() ? text : undefined;
    } else {
      const split = splitTelegramCaption(text);
      caption = split.caption;
      followUpText = split.followUpText;
    }
    const htmlCaption = caption ? renderHtmlText(caption) : undefined;
    // If text exceeds Telegram's caption limit, send media without caption
    // then send text as a separate follow-up message.
    const needsSeparateText = Boolean(followUpText);
    // When splitting, put reply_markup only on the follow-up text (the "main" content),
    // not on the media message.
    const baseMediaParams = {
      ...(hasThreadParams ? threadParams : {}),
      ...(!needsSeparateText && replyMarkup ? { reply_markup: replyMarkup } : {}),
    };
    const mediaParams = {
      ...(htmlCaption ? { caption: htmlCaption, parse_mode: "HTML" as const } : {}),
      ...baseMediaParams,
      ...(opts.silent === true ? { disable_notification: true } : {}),
    };
    const sendMedia = async (
      label: string,
      sender: (
        effectiveParams: Record<string, unknown> | undefined,
      ) => Promise<TelegramMessageLike>,
    ) =>
      await withTelegramThreadFallback(
        mediaParams,
        label,
        opts.verbose,
        async (effectiveParams, retryLabel) =>
          requestWithChatNotFound(() => sender(effectiveParams), retryLabel),
      );

    const mediaSender = (() => {
      if (isGif && !opts.forceDocument) {
        return {
          label: "animation",
          sender: (effectiveParams: Record<string, unknown> | undefined) =>
            api.sendAnimation(
              chatId,
              file,
              effectiveParams as Parameters<typeof api.sendAnimation>[2],
            ) as Promise<TelegramMessageLike>,
        };
      }
      if (kind === "image" && !opts.forceDocument) {
        return {
          label: "photo",
          sender: (effectiveParams: Record<string, unknown> | undefined) =>
            api.sendPhoto(
              chatId,
              file,
              effectiveParams as Parameters<typeof api.sendPhoto>[2],
            ) as Promise<TelegramMessageLike>,
        };
      }
      if (kind === "video") {
        if (isVideoNote) {
          return {
            label: "video_note",
            sender: (effectiveParams: Record<string, unknown> | undefined) =>
              api.sendVideoNote(
                chatId,
                file,
                effectiveParams as Parameters<typeof api.sendVideoNote>[2],
              ) as Promise<TelegramMessageLike>,
          };
        }
        return {
          label: "video",
          sender: (effectiveParams: Record<string, unknown> | undefined) =>
            api.sendVideo(
              chatId,
              file,
              effectiveParams as Parameters<typeof api.sendVideo>[2],
            ) as Promise<TelegramMessageLike>,
        };
      }
      if (kind === "audio") {
        const { useVoice } = resolveTelegramVoiceSend({
          wantsVoice: opts.asVoice === true, // default false (backward compatible)
          contentType: media.contentType,
          fileName,
          logFallback: logVerbose,
        });
        if (useVoice) {
          return {
            label: "voice",
            sender: (effectiveParams: Record<string, unknown> | undefined) =>
              api.sendVoice(
                chatId,
                file,
                effectiveParams as Parameters<typeof api.sendVoice>[2],
              ) as Promise<TelegramMessageLike>,
          };
        }
        return {
          label: "audio",
          sender: (effectiveParams: Record<string, unknown> | undefined) =>
            api.sendAudio(
              chatId,
              file,
              effectiveParams as Parameters<typeof api.sendAudio>[2],
            ) as Promise<TelegramMessageLike>,
        };
      }
      return {
        label: "document",
        sender: (effectiveParams: Record<string, unknown> | undefined) =>
          api.sendDocument(
            chatId,
            file,
            // Only force Telegram to keep the uploaded media type when callers explicitly
            // opt into document delivery for image/GIF uploads.
            (opts.forceDocument
              ? { ...effectiveParams, disable_content_type_detection: true }
              : effectiveParams) as Parameters<typeof api.sendDocument>[2],
          ) as Promise<TelegramMessageLike>,
      };
    })();

    const result = await sendMedia(mediaSender.label, mediaSender.sender);
    const mediaMessageId = resolveTelegramMessageIdOrThrow(result, "media send");
    const resolvedChatId = String(result?.chat?.id ?? chatId);
    recordSentMessage(chatId, mediaMessageId);
    recordChannelActivity({
      channel: "telegram",
      accountId: account.accountId,
      direction: "outbound",
    });

    // If text was too long for a caption, send it as a separate follow-up message.
    // Use HTML conversion so markdown renders like captions.
    if (needsSeparateText && followUpText) {
      if (textMode === "html") {
        const textResult = await sendChunkedText(followUpText, "text follow-up send");
        return { messageId: textResult.messageId, chatId: resolvedChatId };
      }
      const textResult = await sendTelegramTextChunks(
        [{ plainText: followUpText, htmlText: renderHtmlText(followUpText) }],
        "text follow-up send",
      );
      return { messageId: textResult.messageId, chatId: resolvedChatId };
    }

    return { messageId: String(mediaMessageId), chatId: resolvedChatId };
  }

  if (!text || !text.trim()) {
    throw new Error("Message must be non-empty for Telegram sends");
  }
  let textResult: { messageId: string; chatId: string };
  if (textMode === "html") {
    textResult = await sendChunkedText(text, "text send");
  } else {
    textResult = await sendTelegramTextChunks(
      [{ plainText: opts.plainText ?? text, htmlText: renderHtmlText(text) }],
      "text send",
    );
  }
  recordChannelActivity({
    channel: "telegram",
    accountId: account.accountId,
    direction: "outbound",
  });
  return textResult;
}
