import type { ReactionType, ReactionTypeEmoji } from "@grammyjs/types";
import { loadConfig } from "deneb/plugin-sdk/config-runtime";
import { isDiagnosticFlagEnabled } from "deneb/plugin-sdk/infra-runtime";
import { formatErrorMessage, formatUncaughtError } from "deneb/plugin-sdk/infra-runtime";
import { createTelegramRetryRunner } from "deneb/plugin-sdk/infra-runtime";
import type { RetryConfig } from "deneb/plugin-sdk/infra-runtime";
import { createSubsystemLogger } from "deneb/plugin-sdk/runtime-env";
import { redactSensitiveText } from "deneb/plugin-sdk/text-runtime";
import { type ApiClientOptions, Bot, HttpError } from "grammy";
import { type ResolvedTelegramAccount, resolveTelegramAccount } from "./accounts.js";
import { withTelegramApiErrorLogging } from "./api-logging.js";
import { buildTelegramThreadParams, buildTypingThreadParams } from "./bot/helpers.js";
import type { TelegramInlineButtons } from "./button-types.js";
import { resolveTelegramFetch } from "./fetch.js";
import {
  isRecoverableTelegramNetworkError,
  isSafeToRetrySendError,
  isTelegramServerError,
} from "./network-errors.js";
import { makeProxyFetch } from "./proxy.js";
import { maybePersistResolvedTelegramTarget } from "./target-writeback.js";
import {
  normalizeTelegramChatId,
  normalizeTelegramLookupTarget,
  parseTelegramTarget,
} from "./targets.js";

export type TelegramApi = Bot["api"];
export type TelegramApiOverride = Partial<TelegramApi>;

export type TelegramSendOpts = {
  cfg?: ReturnType<typeof loadConfig>;
  token?: string;
  accountId?: string;
  verbose?: boolean;
  mediaUrl?: string;
  mediaLocalRoots?: readonly string[];
  maxBytes?: number;
  api?: TelegramApiOverride;
  retry?: RetryConfig;
  textMode?: "markdown" | "html";
  plainText?: string;
  /** Send audio as voice message (voice bubble) instead of audio file. Defaults to false. */
  asVoice?: boolean;
  /** Send video as video note (voice bubble) instead of regular video. Defaults to false. */
  asVideoNote?: boolean;
  /** Send message silently (no notification). Defaults to false. */
  silent?: boolean;
  /** Message ID to reply to (for threading) */
  replyToMessageId?: number;
  /** Quote text for Telegram reply_parameters. */
  quoteText?: string;
  /** Forum topic thread ID (for forum supergroups) */
  messageThreadId?: number;
  /** Inline keyboard buttons (reply markup). */
  buttons?: TelegramInlineButtons;
  /** Send image as document to avoid Telegram compression. Defaults to false. */
  forceDocument?: boolean;
};

export type TelegramSendResult = {
  messageId: string;
  chatId: string;
};

export type TelegramMessageLike = {
  message_id?: number;
  chat?: { id?: string | number };
};

export type TelegramReactionOpts = {
  cfg?: ReturnType<typeof loadConfig>;
  token?: string;
  accountId?: string;
  api?: TelegramApiOverride;
  remove?: boolean;
  verbose?: boolean;
  retry?: RetryConfig;
};

export type TelegramTypingOpts = {
  cfg?: ReturnType<typeof loadConfig>;
  token?: string;
  accountId?: string;
  verbose?: boolean;
  api?: TelegramApiOverride;
  retry?: RetryConfig;
  messageThreadId?: number;
};

export function resolveTelegramMessageIdOrThrow(
  result: TelegramMessageLike | null | undefined,
  context: string,
): number {
  if (typeof result?.message_id === "number" && Number.isFinite(result.message_id)) {
    return Math.trunc(result.message_id);
  }
  throw new Error(`Telegram ${context} returned no message_id`);
}

export function splitTelegramPlainTextChunks(text: string, limit: number): string[] {
  if (!text) {
    return [];
  }
  const normalizedLimit = Math.max(1, Math.floor(limit));
  const chunks: string[] = [];
  for (let start = 0; start < text.length; start += normalizedLimit) {
    chunks.push(text.slice(start, start + normalizedLimit));
  }
  return chunks;
}

export function splitTelegramPlainTextFallback(
  text: string,
  chunkCount: number,
  limit: number,
): string[] {
  if (!text) {
    return [];
  }
  const normalizedLimit = Math.max(1, Math.floor(limit));
  const fixedChunks = splitTelegramPlainTextChunks(text, normalizedLimit);
  if (chunkCount <= 1 || fixedChunks.length >= chunkCount) {
    return fixedChunks;
  }
  const chunks: string[] = [];
  let offset = 0;
  for (let index = 0; index < chunkCount; index += 1) {
    const remainingChars = text.length - offset;
    const remainingChunks = chunkCount - index;
    const nextChunkLength =
      remainingChunks === 1
        ? remainingChars
        : Math.min(normalizedLimit, Math.ceil(remainingChars / remainingChunks));
    chunks.push(text.slice(offset, offset + nextChunkLength));
    offset += nextChunkLength;
  }
  return chunks;
}

export const PARSE_ERR_RE = /can't parse entities|parse entities|find end of the entity/i;
const THREAD_NOT_FOUND_RE = /400:\s*Bad Request:\s*message thread not found/i;
const MESSAGE_NOT_MODIFIED_RE =
  /400:\s*Bad Request:\s*message is not modified|MESSAGE_NOT_MODIFIED/i;
export const CHAT_NOT_FOUND_RE = /400: Bad Request: chat not found/i;
export const sendLogger = createSubsystemLogger("telegram/send");
const diagLogger = createSubsystemLogger("telegram/diagnostic");
const telegramClientOptionsCache = new Map<string, ApiClientOptions | undefined>();
const MAX_TELEGRAM_CLIENT_OPTIONS_CACHE_SIZE = 64;

export function resetTelegramClientOptionsCacheForTests(): void {
  telegramClientOptionsCache.clear();
}

export function createTelegramHttpLogger(cfg: ReturnType<typeof loadConfig>) {
  const enabled = isDiagnosticFlagEnabled("telegram.http", cfg);
  if (!enabled) {
    return () => {};
  }
  return (label: string, err: unknown) => {
    if (!(err instanceof HttpError)) {
      return;
    }
    const detail = redactSensitiveText(formatUncaughtError(err.error ?? err));
    diagLogger.warn(`telegram http error (${label}): ${detail}`);
  };
}

export function shouldUseTelegramClientOptionsCache(): boolean {
  return !process.env.VITEST && process.env.NODE_ENV !== "test";
}

export function buildTelegramClientOptionsCacheKey(params: {
  account: ResolvedTelegramAccount;
  timeoutSeconds?: number;
}): string {
  const proxyKey = params.account.config.proxy?.trim() ?? "";
  const autoSelectFamily = params.account.config.network?.autoSelectFamily;
  const autoSelectFamilyKey =
    typeof autoSelectFamily === "boolean" ? String(autoSelectFamily) : "default";
  const dnsResultOrderKey = params.account.config.network?.dnsResultOrder ?? "default";
  const timeoutSecondsKey =
    typeof params.timeoutSeconds === "number" ? String(params.timeoutSeconds) : "default";
  return `${params.account.accountId}::${proxyKey}::${autoSelectFamilyKey}::${dnsResultOrderKey}::${timeoutSecondsKey}`;
}

export function setCachedTelegramClientOptions(
  cacheKey: string,
  clientOptions: ApiClientOptions | undefined,
): ApiClientOptions | undefined {
  telegramClientOptionsCache.set(cacheKey, clientOptions);
  if (telegramClientOptionsCache.size > MAX_TELEGRAM_CLIENT_OPTIONS_CACHE_SIZE) {
    const oldestKey = telegramClientOptionsCache.keys().next().value;
    if (oldestKey !== undefined) {
      telegramClientOptionsCache.delete(oldestKey);
    }
  }
  return clientOptions;
}

export function resolveTelegramClientOptions(
  account: ResolvedTelegramAccount,
): ApiClientOptions | undefined {
  const DEFAULT_TELEGRAM_TIMEOUT_SECONDS = 30;
  const timeoutSeconds =
    typeof account.config.timeoutSeconds === "number" &&
    Number.isFinite(account.config.timeoutSeconds)
      ? Math.max(1, Math.floor(account.config.timeoutSeconds))
      : DEFAULT_TELEGRAM_TIMEOUT_SECONDS;

  const cacheEnabled = shouldUseTelegramClientOptionsCache();
  const cacheKey = cacheEnabled
    ? buildTelegramClientOptionsCacheKey({
        account,
        timeoutSeconds,
      })
    : null;
  if (cacheKey && telegramClientOptionsCache.has(cacheKey)) {
    return telegramClientOptionsCache.get(cacheKey);
  }

  const proxyUrl = account.config.proxy?.trim();
  const proxyFetch = proxyUrl ? makeProxyFetch(proxyUrl) : undefined;
  const fetchImpl = resolveTelegramFetch(proxyFetch, {
    network: account.config.network,
  });
  const clientOptions =
    fetchImpl || timeoutSeconds
      ? {
          ...(fetchImpl ? { fetch: fetchImpl as unknown as ApiClientOptions["fetch"] } : {}),
          ...(timeoutSeconds ? { timeoutSeconds } : {}),
        }
      : undefined;
  if (cacheKey) {
    return setCachedTelegramClientOptions(cacheKey, clientOptions);
  }
  return clientOptions;
}

export function resolveToken(
  explicit: string | undefined,
  params: { accountId: string; token: string },
) {
  if (explicit?.trim()) {
    return explicit.trim();
  }
  if (!params.token) {
    throw new Error(
      `Telegram bot token missing for account "${params.accountId}" (set channels.telegram.accounts.${params.accountId}.botToken/tokenFile or TELEGRAM_BOT_TOKEN for default).`,
    );
  }
  return params.token.trim();
}

export async function resolveChatId(
  to: string,
  params: { api: TelegramApiOverride; verbose?: boolean },
): Promise<string> {
  const numericChatId = normalizeTelegramChatId(to);
  if (numericChatId) {
    return numericChatId;
  }
  const lookupTarget = normalizeTelegramLookupTarget(to);
  const getChat = params.api.getChat;
  if (!lookupTarget || typeof getChat !== "function") {
    throw new Error("Telegram recipient must be a numeric chat ID");
  }
  try {
    const chat = await getChat.call(params.api, lookupTarget);
    const resolved = normalizeTelegramChatId(String(chat?.id ?? ""));
    if (!resolved) {
      throw new Error(`resolved chat id is not numeric (${String(chat?.id ?? "")})`);
    }
    if (params.verbose) {
      sendLogger.warn(`telegram recipient ${lookupTarget} resolved to numeric chat id ${resolved}`);
    }
    return resolved;
  } catch (err) {
    const detail = formatErrorMessage(err);
    throw new Error(
      `Telegram recipient ${lookupTarget} could not be resolved to a numeric chat ID (${detail})`,
      { cause: err },
    );
  }
}

export async function resolveAndPersistChatId(params: {
  cfg: ReturnType<typeof loadConfig>;
  api: TelegramApiOverride;
  lookupTarget: string;
  persistTarget: string;
  verbose?: boolean;
}): Promise<string> {
  const chatId = await resolveChatId(params.lookupTarget, {
    api: params.api,
    verbose: params.verbose,
  });
  await maybePersistResolvedTelegramTarget({
    cfg: params.cfg,
    rawTarget: params.persistTarget,
    resolvedChatId: chatId,
    verbose: params.verbose,
  });
  return chatId;
}

export function normalizeMessageId(raw: string | number): number {
  if (typeof raw === "number" && Number.isFinite(raw)) {
    return Math.trunc(raw);
  }
  if (typeof raw === "string") {
    const value = raw.trim();
    if (!value) {
      throw new Error("Message id is required for Telegram actions");
    }
    const parsed = Number.parseInt(value, 10);
    if (Number.isFinite(parsed)) {
      return parsed;
    }
  }
  throw new Error("Message id is required for Telegram actions");
}

export function isTelegramThreadNotFoundError(err: unknown): boolean {
  return THREAD_NOT_FOUND_RE.test(formatErrorMessage(err));
}

export function isTelegramMessageNotModifiedError(err: unknown): boolean {
  return MESSAGE_NOT_MODIFIED_RE.test(formatErrorMessage(err));
}

export function hasMessageThreadIdParam(params?: Record<string, unknown>): boolean {
  if (!params) {
    return false;
  }
  const value = params.message_thread_id;
  if (typeof value === "number") {
    return Number.isFinite(value);
  }
  if (typeof value === "string") {
    return value.trim().length > 0;
  }
  return false;
}

export function removeMessageThreadIdParam(
  params?: Record<string, unknown>,
): Record<string, unknown> | undefined {
  if (!params || !hasMessageThreadIdParam(params)) {
    return params;
  }
  const next = { ...params };
  delete next.message_thread_id;
  return Object.keys(next).length > 0 ? next : undefined;
}

export function isTelegramHtmlParseError(err: unknown): boolean {
  return PARSE_ERR_RE.test(formatErrorMessage(err));
}

export function buildTelegramThreadReplyParams(params: {
  targetMessageThreadId?: number;
  messageThreadId?: number;
  chatType?: "direct" | "group" | "unknown";
  replyToMessageId?: number;
  quoteText?: string;
}): Record<string, unknown> {
  const messageThreadId =
    params.messageThreadId != null ? params.messageThreadId : params.targetMessageThreadId;
  const threadScope = params.chatType === "direct" ? ("dm" as const) : ("forum" as const);
  // Never blanket-strip DM message_thread_id by chat-id sign.
  // Telegram supports DM topics; stripping silently misroutes topic replies.
  // Keep thread id and rely on thread-not-found retry fallback for plain DMs.
  const threadSpec =
    messageThreadId != null ? { id: messageThreadId, scope: threadScope } : undefined;
  const threadIdParams = buildTelegramThreadParams(threadSpec);
  const threadParams: Record<string, unknown> = threadIdParams ? { ...threadIdParams } : {};

  if (params.replyToMessageId != null) {
    const replyToMessageId = Math.trunc(params.replyToMessageId);
    if (params.quoteText?.trim()) {
      threadParams.reply_parameters = {
        message_id: replyToMessageId,
        quote: params.quoteText.trim(),
      };
    } else {
      threadParams.reply_to_message_id = replyToMessageId;
    }
  }
  return threadParams;
}

export async function withTelegramHtmlParseFallback<T>(params: {
  label: string;
  verbose?: boolean;
  requestHtml: (label: string) => Promise<T>;
  requestPlain: (label: string) => Promise<T>;
}): Promise<T> {
  try {
    return await params.requestHtml(params.label);
  } catch (err) {
    if (!isTelegramHtmlParseError(err)) {
      throw err;
    }
    if (params.verbose) {
      sendLogger.warn(
        `telegram ${params.label} failed with HTML parse error, retrying as plain text: ${formatErrorMessage(
          err,
        )}`,
      );
    }
    return await params.requestPlain(`${params.label}-plain`);
  }
}

export type TelegramApiContext = {
  cfg: ReturnType<typeof loadConfig>;
  account: ResolvedTelegramAccount;
  api: TelegramApi;
};

export function resolveTelegramApiContext(opts: {
  token?: string;
  accountId?: string;
  api?: TelegramApiOverride;
  cfg?: ReturnType<typeof loadConfig>;
}): TelegramApiContext {
  const cfg = opts.cfg ?? loadConfig();
  const account = resolveTelegramAccount({
    cfg,
    accountId: opts.accountId,
  });
  const token = resolveToken(opts.token, account);
  const client = resolveTelegramClientOptions(account);
  const api = (opts.api ?? new Bot(token, client ? { client } : undefined).api) as TelegramApi;
  return { cfg, account, api };
}

export type TelegramRequestWithDiag = <T>(
  fn: () => Promise<T>,
  label?: string,
  options?: { shouldLog?: (err: unknown) => boolean },
) => Promise<T>;

export function createTelegramRequestWithDiag(params: {
  cfg: ReturnType<typeof loadConfig>;
  account: ResolvedTelegramAccount;
  retry?: RetryConfig;
  verbose?: boolean;
  shouldRetry?: (err: unknown) => boolean;
  /** When true, the shouldRetry predicate is used exclusively without the TELEGRAM_RETRY_RE fallback. */
  strictShouldRetry?: boolean;
  useApiErrorLogging?: boolean;
}): TelegramRequestWithDiag {
  const request = createTelegramRetryRunner({
    retry: params.retry,
    configRetry: params.account.config.retry,
    verbose: params.verbose,
    ...(params.shouldRetry ? { shouldRetry: params.shouldRetry } : {}),
    ...(params.strictShouldRetry ? { strictShouldRetry: true } : {}),
  });
  const logHttpError = createTelegramHttpLogger(params.cfg);
  return <T>(
    fn: () => Promise<T>,
    label?: string,
    options?: { shouldLog?: (err: unknown) => boolean },
  ) => {
    const runRequest = () => request(fn, label);
    const call =
      params.useApiErrorLogging === false
        ? runRequest()
        : withTelegramApiErrorLogging({
            operation: label ?? "request",
            fn: runRequest,
            ...(options?.shouldLog ? { shouldLog: options.shouldLog } : {}),
          });
    return call.catch((err) => {
      logHttpError(label ?? "request", err);
      throw err;
    });
  };
}

export function wrapTelegramChatNotFoundError(
  err: unknown,
  params: { chatId: string; input: string },
) {
  if (!CHAT_NOT_FOUND_RE.test(formatErrorMessage(err))) {
    return err;
  }
  return new Error(
    [
      `Telegram send failed: chat not found (chat_id=${params.chatId}).`,
      "Likely: bot not started in DM, bot removed from group/channel, group migrated (new -100… id), or wrong bot token.",
      `Input was: ${JSON.stringify(params.input)}.`,
    ].join(" "),
  );
}

export async function withTelegramThreadFallback<T>(
  params: Record<string, unknown> | undefined,
  label: string,
  verbose: boolean | undefined,
  attempt: (
    effectiveParams: Record<string, unknown> | undefined,
    effectiveLabel: string,
  ) => Promise<T>,
): Promise<T> {
  try {
    return await attempt(params, label);
  } catch (err) {
    // Do not widen this fallback to cover "chat not found".
    // chat-not-found is routing/auth/membership/token; stripping thread IDs hides root cause.
    if (!hasMessageThreadIdParam(params) || !isTelegramThreadNotFoundError(err)) {
      throw err;
    }
    if (verbose) {
      sendLogger.warn(
        `telegram ${label} failed with message_thread_id, retrying without thread: ${formatErrorMessage(err)}`,
      );
    }
    const retriedParams = removeMessageThreadIdParam(params);
    return await attempt(retriedParams, `${label}-threadless`);
  }
}

export function createRequestWithChatNotFound(params: {
  requestWithDiag: TelegramRequestWithDiag;
  chatId: string;
  input: string;
}) {
  return async <T>(fn: () => Promise<T>, label: string) =>
    params.requestWithDiag(fn, label).catch((err) => {
      throw wrapTelegramChatNotFoundError(err, {
        chatId: params.chatId,
        input: params.input,
      });
    });
}

export function createTelegramNonIdempotentRequestWithDiag(params: {
  cfg: ReturnType<typeof loadConfig>;
  account: ResolvedTelegramAccount;
  retry?: RetryConfig;
  verbose?: boolean;
  useApiErrorLogging?: boolean;
}): TelegramRequestWithDiag {
  return createTelegramRequestWithDiag({
    cfg: params.cfg,
    account: params.account,
    retry: params.retry,
    verbose: params.verbose,
    useApiErrorLogging: params.useApiErrorLogging,
    shouldRetry: (err) => isSafeToRetrySendError(err),
    strictShouldRetry: true,
  });
}

// Re-export helpers needed by split files
export {
  buildTypingThreadParams,
  isRecoverableTelegramNetworkError,
  isTelegramServerError,
  parseTelegramTarget,
};
export type { ReactionType, ReactionTypeEmoji };
