/**
 * Classify a chat error message into a structured errorKind.
 *
 * Used by ChatEventSchema's errorKind field so ACP clients and UI consumers
 * can distinguish transient backend errors from deliberate refusals.
 */

export type ChatErrorKind = "timeout" | "rate_limit" | "overloaded" | "server_error";

const RATE_LIMIT_PATTERNS = [/rate.?limit/i, /too many requests/i, /429/, /quota.*exceeded/i];

const TIMEOUT_PATTERNS = [
  /timeout/i,
  /timed?\s*out/i,
  /ETIMEDOUT/,
  /ECONNABORTED/,
  /deadline.*exceeded/i,
];

const OVERLOADED_PATTERNS = [/overloaded/i, /503/, /service.*unavailable/i, /capacity/i];

function matchesAny(msg: string, patterns: RegExp[]): boolean {
  return patterns.some((p) => p.test(msg));
}

export function classifyChatErrorKind(errorMessage?: string): ChatErrorKind | undefined {
  if (!errorMessage) {
    return undefined;
  }
  if (matchesAny(errorMessage, RATE_LIMIT_PATTERNS)) {
    return "rate_limit";
  }
  if (matchesAny(errorMessage, TIMEOUT_PATTERNS)) {
    return "timeout";
  }
  if (matchesAny(errorMessage, OVERLOADED_PATTERNS)) {
    return "overloaded";
  }
  return "server_error";
}
