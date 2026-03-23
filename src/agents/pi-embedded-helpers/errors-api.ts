import { stableStringify } from "../stable-stringify.js";
import { isImageDimensionErrorMessage, isImageSizeError } from "./errors-assistant.js";
import {
  isAuthErrorMessage,
  isAuthPermanentErrorMessage,
  isBillingErrorMessage,
  isOverloadedErrorMessage,
  isPeriodicUsageLimitErrorMessage,
  isRateLimitErrorMessage,
  isTimeoutErrorMessage,
  matchesFormatErrorPattern,
} from "./failover-matches.js";
import type { FailoverReason } from "./types.js";

// Allow provider-wrapped API payloads such as "Ollama API error 400: {...}".
const ERROR_PAYLOAD_PREFIX_RE =
  /^(?:error|(?:[a-z][\w-]*\s+)?api\s*error|apierror|openai\s*error|anthropic\s*error|gateway\s*error)(?:\s+\d{3})?[:\s-]+/i;
const HTML_ERROR_PREFIX_RE = /^\s*(?:<!doctype\s+html\b|<html\b)/i;
const HTTP_STATUS_CODE_PREFIX_RE = /^(?:http\s*)?(\d{3})(?:\s+([\s\S]+))?$/i;
const CLOUDFLARE_HTML_ERROR_CODES = new Set([521, 522, 523, 524, 525, 526, 530]);
const TRANSIENT_HTTP_ERROR_CODES = new Set([499, 500, 502, 503, 504, 521, 522, 523, 524, 529]);

type PaymentRequiredFailoverReason = Extract<FailoverReason, "billing" | "rate_limit">;

const BILLING_402_HINTS = [
  "insufficient credits",
  "insufficient quota",
  "credit balance",
  "insufficient balance",
  "plans & billing",
  "add more credits",
  "top up",
] as const;
const BILLING_402_PLAN_HINTS = [
  "upgrade your plan",
  "upgrade plan",
  "current plan",
  "subscription",
] as const;

const PERIODIC_402_HINTS = ["daily", "weekly", "monthly"] as const;
const RETRYABLE_402_RETRY_HINTS = ["try again", "retry", "temporary", "cooldown"] as const;
const RETRYABLE_402_LIMIT_HINTS = ["usage limit", "rate limit", "organization usage"] as const;
const RETRYABLE_402_SCOPED_HINTS = ["organization", "workspace"] as const;
const RETRYABLE_402_SCOPED_RESULT_HINTS = [
  "billing period",
  "exceeded",
  "reached",
  "exhausted",
] as const;
const RAW_402_MARKER_RE =
  /["']?(?:status|code)["']?\s*[:=]\s*402\b|\bhttp\s*402\b|\berror(?:\s+code)?\s*[:=]?\s*402\b|\b(?:got|returned|received)\s+(?:a\s+)?402\b|^\s*402\s+payment required\b|^\s*402\s+.*used up your points\b/i;
const LEADING_402_WRAPPER_RE =
  /^(?:error[:\s-]+)?(?:(?:http\s*)?402(?:\s+payment required)?|payment required)(?:[:\s-]+|$)/i;

type ErrorPayload = Record<string, unknown>;

function includesAnyHint(text: string, hints: readonly string[]): boolean {
  return hints.some((hint) => text.includes(hint));
}

function hasExplicit402BillingSignal(text: string): boolean {
  return (
    includesAnyHint(text, BILLING_402_HINTS) ||
    (includesAnyHint(text, BILLING_402_PLAN_HINTS) && text.includes("limit")) ||
    text.includes("billing hard limit") ||
    text.includes("hard limit reached") ||
    (text.includes("maximum allowed") && text.includes("limit"))
  );
}

function hasQuotaRefreshWindowSignal(text: string): boolean {
  return (
    text.includes("subscription quota limit") &&
    (text.includes("automatic quota refresh") || text.includes("rolling time window"))
  );
}

function hasRetryable402TransientSignal(text: string): boolean {
  const hasPeriodicHint = includesAnyHint(text, PERIODIC_402_HINTS);
  const hasSpendLimit = text.includes("spend limit") || text.includes("spending limit");
  const hasScopedHint = includesAnyHint(text, RETRYABLE_402_SCOPED_HINTS);
  return (
    (includesAnyHint(text, RETRYABLE_402_RETRY_HINTS) &&
      includesAnyHint(text, RETRYABLE_402_LIMIT_HINTS)) ||
    (hasPeriodicHint && (text.includes("usage limit") || hasSpendLimit)) ||
    (hasPeriodicHint && text.includes("limit") && text.includes("reset")) ||
    (hasScopedHint &&
      text.includes("limit") &&
      (hasSpendLimit || includesAnyHint(text, RETRYABLE_402_SCOPED_RESULT_HINTS)))
  );
}

function normalize402Message(raw: string): string {
  return raw.trim().toLowerCase().replace(LEADING_402_WRAPPER_RE, "").trim();
}

function classify402Message(message: string): PaymentRequiredFailoverReason {
  const normalized = normalize402Message(message);
  if (!normalized) {
    return "billing";
  }

  if (hasQuotaRefreshWindowSignal(normalized)) {
    return "rate_limit";
  }

  if (hasExplicit402BillingSignal(normalized)) {
    return "billing";
  }

  if (isRateLimitErrorMessage(normalized)) {
    return "rate_limit";
  }

  if (hasRetryable402TransientSignal(normalized)) {
    return "rate_limit";
  }

  return "billing";
}

function classifyFailoverReasonFrom402Text(raw: string): PaymentRequiredFailoverReason | null {
  if (!RAW_402_MARKER_RE.test(raw)) {
    return null;
  }
  return classify402Message(raw);
}

export function extractLeadingHttpStatus(raw: string): { code: number; rest: string } | null {
  const match = raw.match(HTTP_STATUS_CODE_PREFIX_RE);
  if (!match) {
    return null;
  }
  const code = Number(match[1]);
  if (!Number.isFinite(code)) {
    return null;
  }
  return { code, rest: (match[2] ?? "").trim() };
}

export function isCloudflareOrHtmlErrorPage(raw: string): boolean {
  const trimmed = raw.trim();
  if (!trimmed) {
    return false;
  }

  const status = extractLeadingHttpStatus(trimmed);
  if (!status || status.code < 500) {
    return false;
  }

  if (CLOUDFLARE_HTML_ERROR_CODES.has(status.code)) {
    return true;
  }

  return (
    status.code < 600 && HTML_ERROR_PREFIX_RE.test(status.rest) && /<\/html>/i.test(status.rest)
  );
}

export function isTransientHttpError(raw: string): boolean {
  const trimmed = raw.trim();
  if (!trimmed) {
    return false;
  }
  const status = extractLeadingHttpStatus(trimmed);
  if (!status) {
    return false;
  }
  return TRANSIENT_HTTP_ERROR_CODES.has(status.code);
}

export function classifyFailoverReasonFromHttpStatus(
  status: number | undefined,
  message?: string,
): FailoverReason | null {
  if (typeof status !== "number" || !Number.isFinite(status)) {
    return null;
  }

  if (status === 402) {
    return message ? classify402Message(message) : "billing";
  }
  if (status === 429) {
    return "rate_limit";
  }
  if (status === 401 || status === 403) {
    if (message && isAuthPermanentErrorMessage(message)) {
      return "auth_permanent";
    }
    return "auth";
  }
  if (status === 408) {
    return "timeout";
  }
  if (status === 503) {
    if (message && isOverloadedErrorMessage(message)) {
      return "overloaded";
    }
    return "timeout";
  }
  if (status === 499) {
    if (message && isOverloadedErrorMessage(message)) {
      return "overloaded";
    }
    return "timeout";
  }
  if (status === 502 || status === 504) {
    return "timeout";
  }
  if (status === 529) {
    return "overloaded";
  }
  if (status === 400 || status === 422) {
    // Some providers return quota/balance errors under HTTP 400, so do not
    // let the generic format fallback mask an explicit billing signal.
    if (message && isBillingErrorMessage(message)) {
      return "billing";
    }
    return "format";
  }
  return null;
}

function isErrorPayloadObject(payload: unknown): payload is ErrorPayload {
  if (!payload || typeof payload !== "object" || Array.isArray(payload)) {
    return false;
  }
  const record = payload as ErrorPayload;
  if (record.type === "error") {
    return true;
  }
  if (typeof record.request_id === "string" || typeof record.requestId === "string") {
    return true;
  }
  if ("error" in record) {
    const err = record.error;
    if (err && typeof err === "object" && !Array.isArray(err)) {
      const errRecord = err as ErrorPayload;
      if (
        typeof errRecord.message === "string" ||
        typeof errRecord.type === "string" ||
        typeof errRecord.code === "string"
      ) {
        return true;
      }
    }
  }
  return false;
}

function parseApiErrorPayload(raw: string): ErrorPayload | null {
  if (!raw) {
    return null;
  }
  const trimmed = raw.trim();
  if (!trimmed) {
    return null;
  }
  const candidates = [trimmed];
  if (ERROR_PAYLOAD_PREFIX_RE.test(trimmed)) {
    candidates.push(trimmed.replace(ERROR_PAYLOAD_PREFIX_RE, "").trim());
  }
  for (const candidate of candidates) {
    if (!candidate.startsWith("{") || !candidate.endsWith("}")) {
      continue;
    }
    try {
      const parsed = JSON.parse(candidate) as unknown;
      if (isErrorPayloadObject(parsed)) {
        return parsed;
      }
    } catch {
      // ignore parse errors
    }
  }
  return null;
}

export function getApiErrorPayloadFingerprint(raw?: string): string | null {
  if (!raw) {
    return null;
  }
  const payload = parseApiErrorPayload(raw);
  if (!payload) {
    return null;
  }
  return stableStringify(payload);
}

export function isRawApiErrorPayload(raw?: string): boolean {
  return getApiErrorPayloadFingerprint(raw) !== null;
}

export type ApiErrorInfo = {
  httpCode?: string;
  type?: string;
  message?: string;
  requestId?: string;
};

export function parseApiErrorInfo(raw?: string): ApiErrorInfo | null {
  if (!raw) {
    return null;
  }
  const trimmed = raw.trim();
  if (!trimmed) {
    return null;
  }

  let httpCode: string | undefined;
  let candidate = trimmed;

  const httpPrefixMatch = candidate.match(/^(\d{3})\s+(.+)$/s);
  if (httpPrefixMatch) {
    httpCode = httpPrefixMatch[1];
    candidate = httpPrefixMatch[2].trim();
  }

  const payload = parseApiErrorPayload(candidate);
  if (!payload) {
    return null;
  }

  const requestId =
    typeof payload.request_id === "string"
      ? payload.request_id
      : typeof payload.requestId === "string"
        ? payload.requestId
        : undefined;

  const topType = typeof payload.type === "string" ? payload.type : undefined;
  const topMessage = typeof payload.message === "string" ? payload.message : undefined;

  let errType: string | undefined;
  let errMessage: string | undefined;
  if (payload.error && typeof payload.error === "object" && !Array.isArray(payload.error)) {
    const err = payload.error as Record<string, unknown>;
    if (typeof err.type === "string") {
      errType = err.type;
    }
    if (typeof err.code === "string" && !errType) {
      errType = err.code;
    }
    if (typeof err.message === "string") {
      errMessage = err.message;
    }
  }

  return {
    httpCode,
    type: errType ?? topType,
    message: errMessage ?? topMessage,
    requestId,
  };
}

function isCliSessionExpiredErrorMessage(raw: string): boolean {
  if (!raw) {
    return false;
  }
  const lower = raw.toLowerCase();
  return (
    lower.includes("session not found") ||
    lower.includes("session does not exist") ||
    lower.includes("session expired") ||
    lower.includes("session invalid") ||
    lower.includes("conversation not found") ||
    lower.includes("conversation does not exist") ||
    lower.includes("conversation expired") ||
    lower.includes("conversation invalid") ||
    lower.includes("no such session") ||
    lower.includes("invalid session") ||
    lower.includes("session id not found") ||
    lower.includes("conversation id not found")
  );
}

function isJsonApiInternalServerError(raw: string): boolean {
  if (!raw) {
    return false;
  }
  const value = raw.toLowerCase();
  // Anthropic often wraps transient 500s in JSON payloads like:
  // {"type":"error","error":{"type":"api_error","message":"Internal server error"}}
  return value.includes('"type":"api_error"') && value.includes("internal server error");
}

export function isModelNotFoundErrorMessage(raw: string): boolean {
  if (!raw) {
    return false;
  }
  const lower = raw.toLowerCase();

  // Direct pattern matches from Deneb internals and common providers.
  if (
    lower.includes("unknown model") ||
    lower.includes("model not found") ||
    lower.includes("model_not_found") ||
    lower.includes("not_found_error") ||
    (lower.includes("does not exist") && lower.includes("model")) ||
    (lower.includes("invalid model") && !lower.includes("invalid model reference"))
  ) {
    return true;
  }

  // Google Gemini: "models/X is not found for api version"
  if (/models\/[^\s]+ is not found/i.test(raw)) {
    return true;
  }

  // JSON error payloads: {"status": "NOT_FOUND"} or {"code": 404} combined with not-found text.
  if (/\b404\b/.test(raw) && /not[-_ ]?found/i.test(raw)) {
    return true;
  }

  return false;
}

export function isCloudCodeAssistFormatError(raw: string): boolean {
  return !isImageDimensionErrorMessage(raw) && matchesFormatErrorPattern(raw);
}

export function classifyFailoverReason(raw: string): FailoverReason | null {
  if (isImageDimensionErrorMessage(raw)) {
    return null;
  }
  if (isImageSizeError(raw)) {
    return null;
  }
  if (isCliSessionExpiredErrorMessage(raw)) {
    return "session_expired";
  }
  if (isModelNotFoundErrorMessage(raw)) {
    return "model_not_found";
  }
  const reasonFrom402Text = classifyFailoverReasonFrom402Text(raw);
  if (reasonFrom402Text) {
    return reasonFrom402Text;
  }
  if (isPeriodicUsageLimitErrorMessage(raw)) {
    return isBillingErrorMessage(raw) ? "billing" : "rate_limit";
  }
  if (isRateLimitErrorMessage(raw)) {
    return "rate_limit";
  }
  if (isOverloadedErrorMessage(raw)) {
    return "overloaded";
  }
  if (isTransientHttpError(raw)) {
    // 529 is always overloaded, even without explicit overload keywords in the body.
    const status = extractLeadingHttpStatus(raw.trim());
    if (status?.code === 529) {
      return "overloaded";
    }
    // Treat remaining transient 5xx provider failures as retryable transport issues.
    return "timeout";
  }
  if (isJsonApiInternalServerError(raw)) {
    return "timeout";
  }
  if (isCloudCodeAssistFormatError(raw)) {
    return "format";
  }
  if (isBillingErrorMessage(raw)) {
    return "billing";
  }
  if (isTimeoutErrorMessage(raw)) {
    return "timeout";
  }
  if (isAuthPermanentErrorMessage(raw)) {
    return "auth_permanent";
  }
  if (isAuthErrorMessage(raw)) {
    return "auth";
  }
  return null;
}

export function isFailoverErrorMessage(raw: string): boolean {
  return classifyFailoverReason(raw) !== null;
}

export function isFailoverAssistantError(
  msg: import("@mariozechner/pi-ai").AssistantMessage | undefined,
): boolean {
  if (!msg || msg.stopReason !== "error") {
    return false;
  }
  return isFailoverErrorMessage(msg.errorMessage ?? "");
}

export type { FailoverReason };
