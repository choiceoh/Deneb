// ── Heartbeat detection ─────────────────────────────────────────────────────

const HEARTBEAT_OK_TOKEN = "heartbeat_ok";

/**
 * Detect whether an assistant message is a heartbeat ack.
 *
 * Matches the same pattern as OpenClaw core's heartbeat-events-filter:
 * content starts with "heartbeat_ok" (case-insensitive) and any character
 * immediately after is not alphanumeric or underscore.
 *
 * This catches:
 *   - "HEARTBEAT_OK"
 *   - "  HEARTBEAT_OK  "
 *   - "HEARTBEAT_OK — weekend, no market."
 *   - "Saturday 10:48 AM PT — weekend, no market. HEARTBEAT_OK"
 *
 * But not:
 *   - "HEARTBEAT_OK_EXTENDED" (alphanumeric continuation)
 */
export function isHeartbeatOkContent(content: string): boolean {
  const trimmed = content.trim().toLowerCase();
  if (!trimmed) {
    return false;
  }

  // Check if it starts with the token
  if (trimmed.startsWith(HEARTBEAT_OK_TOKEN)) {
    const suffix = trimmed.slice(HEARTBEAT_OK_TOKEN.length);
    if (suffix.length === 0) {
      return true;
    }
    return !/[a-z0-9_]/.test(suffix[0]);
  }

  // Also check if it ends with the token (chatty prefix + HEARTBEAT_OK)
  if (trimmed.endsWith(HEARTBEAT_OK_TOKEN)) {
    return true;
  }

  return false;
}

// ── Emergency fallback summarization ────────────────────────────────────────

/**
 * Creates a deterministic truncation summarizer used only as an emergency
 * fallback when the model-backed summarizer cannot be created.
 *
 * CompactionEngine already escalates normal -> aggressive -> fallback for
 * convergence. This function simply provides a stable baseline summarize
 * callback to keep compaction operable when runtime setup is unavailable.
 */
export function createEmergencyFallbackSummarize(): (
  text: string,
  aggressive?: boolean,
) => Promise<string> {
  return async (text: string, aggressive?: boolean): Promise<string> => {
    const maxChars = aggressive ? 600 * 4 : 900 * 4;
    if (text.length <= maxChars) {
      return text;
    }
    return text.slice(0, maxChars) + "\n[Truncated for context management]";
  };
}
