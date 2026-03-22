/**
 * Validation and sanitization utilities for the autonomous system.
 * Prevents NaN propagation, invalid timestamps, and bad user input.
 */

/** Maximum safe timestamp: year 2100. */
const MAX_TIMESTAMP_MS = 4_102_444_800_000;
/** Minimum safe timestamp: year 2000. */
const MIN_TIMESTAMP_MS = 946_684_800_000;
/** Maximum setTimeout delay (~24.8 days). */
export const MAX_TIMEOUT_MS = 2_147_483_647;
/** Maximum string length for stored text fields. */
const MAX_TEXT_LENGTH = 10_000;
/** Maximum string length for IDs. */
const MAX_ID_LENGTH = 200;

/**
 * Returns true if the value is a finite, positive number.
 * Rejects NaN, Infinity, negative, and non-number types.
 */
export function isFinitePositive(value: unknown): value is number {
  return typeof value === "number" && Number.isFinite(value) && value > 0;
}

/**
 * Returns true if the value is a finite, non-negative number.
 */
export function isFiniteNonNegative(value: unknown): value is number {
  return typeof value === "number" && Number.isFinite(value) && value >= 0;
}

/**
 * Validate a timestamp (ms since epoch). Returns the value if valid, or the fallback.
 */
export function safeTimestamp(value: unknown, fallback = 0): number {
  if (typeof value !== "number" || !Number.isFinite(value)) {
    return fallback;
  }
  if (value < 0 || value > MAX_TIMESTAMP_MS) {
    return fallback;
  }
  return value;
}

/**
 * Safely parse a date string. Returns the timestamp or undefined if invalid.
 */
export function safeDateParse(value: string | undefined | null): number | undefined {
  if (!value || typeof value !== "string") {
    return undefined;
  }
  const trimmed = value.trim();
  if (trimmed.length === 0) {
    return undefined;
  }
  const ts = new Date(trimmed).getTime();
  if (!Number.isFinite(ts)) {
    return undefined;
  }
  return ts;
}

/**
 * Clamp a delay in milliseconds to a safe range for setTimeout.
 */
export function clampDelay(delayMs: number, minMs = 0, maxMs = MAX_TIMEOUT_MS): number {
  if (!Number.isFinite(delayMs)) {
    return minMs;
  }
  return Math.max(minMs, Math.min(delayMs, maxMs));
}

/**
 * Validate and clamp a "max per hour" rate value.
 * Returns at least 1 to prevent division by zero.
 */
export function safeMaxPerHour(value: unknown, fallback = 12): number {
  if (typeof value !== "number" || !Number.isFinite(value) || value < 1) {
    return fallback;
  }
  return Math.floor(Math.min(value, 1000));
}

/**
 * Sanitize a string for storage. Trims, clamps length, removes null bytes.
 */
export function sanitizeText(value: unknown, maxLength = MAX_TEXT_LENGTH): string {
  if (typeof value !== "string") {
    return "";
  }
  // oxlint-disable-next-line no-control-regex -- intentionally stripping null bytes for safety.
  return value.replace(/\0/g, "").trim().slice(0, maxLength);
}

/**
 * Sanitize an ID string. Trims, clamps length, strips unsafe characters.
 */
export function sanitizeId(value: unknown): string {
  if (typeof value !== "string") {
    return "";
  }
  // Keep only alphanumeric, hyphens, underscores, dots, and colons.
  return value
    .trim()
    .replace(/[^a-zA-Z0-9\-_.:]/g, "")
    .slice(0, MAX_ID_LENGTH);
}

/**
 * Validate that an array contains only strings. Returns a cleaned copy.
 */
export function validateStringArray(value: unknown): string[] {
  if (!Array.isArray(value)) {
    return [];
  }
  return value.filter((item): item is string => typeof item === "string");
}

/**
 * Safe division that returns fallback on divide-by-zero or NaN.
 */
export function safeDivide(numerator: number, denominator: number, fallback = 0): number {
  if (denominator === 0 || !Number.isFinite(denominator) || !Number.isFinite(numerator)) {
    return fallback;
  }
  return numerator / denominator;
}

/**
 * Format a timestamp for display. Returns "never" for 0/invalid.
 */
export function formatTimestamp(ts: number): string {
  if (!isFinitePositive(ts) || ts < MIN_TIMESTAMP_MS) {
    return "never";
  }
  try {
    return new Date(ts).toLocaleString();
  } catch {
    return "invalid";
  }
}

/**
 * Safe ISO string from timestamp. Returns null for invalid values.
 */
export function safeIsoString(ts: number): string | null {
  if (!Number.isFinite(ts) || ts < 0 || ts > MAX_TIMESTAMP_MS) {
    return null;
  }
  try {
    return new Date(ts).toISOString();
  } catch {
    return null;
  }
}
