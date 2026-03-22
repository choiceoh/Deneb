/**
 * Preview streaming mode utilities.
 *
 * Historically shared across Discord/Slack/Telegram. Now only Telegram is used.
 * Kept at the same path to avoid churn in the Telegram schema import.
 */

export type StreamingMode = "off" | "partial" | "block" | "progress";
export type TelegramPreviewStreamMode = "off" | "partial" | "block";

function normalizeStreamingMode(value: unknown): string | null {
  if (typeof value !== "string") {
    return null;
  }
  const normalized = value.trim().toLowerCase();
  return normalized || null;
}

export function parseStreamingMode(value: unknown): StreamingMode | null {
  const normalized = normalizeStreamingMode(value);
  if (
    normalized === "off" ||
    normalized === "partial" ||
    normalized === "block" ||
    normalized === "progress"
  ) {
    return normalized;
  }
  return null;
}

function parsePreviewStreamMode(value: unknown): TelegramPreviewStreamMode | null {
  const parsed = parseStreamingMode(value);
  if (!parsed) {
    return null;
  }
  return parsed === "progress" ? "partial" : parsed;
}

export function resolveTelegramPreviewStreamMode(
  params: {
    streamMode?: unknown;
    streaming?: unknown;
  } = {},
): TelegramPreviewStreamMode {
  const parsedStreaming = parseStreamingMode(params.streaming);
  if (parsedStreaming) {
    if (parsedStreaming === "progress") {
      return "partial";
    }
    return parsedStreaming;
  }

  const legacy = parsePreviewStreamMode(params.streamMode);
  if (legacy) {
    return legacy;
  }
  if (typeof params.streaming === "boolean") {
    return params.streaming ? "partial" : "off";
  }
  return "partial";
}
