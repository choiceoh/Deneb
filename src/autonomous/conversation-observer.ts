import { createSubsystemLogger } from "../logging/subsystem.js";
import { addObservation, loadAutonomousState, saveAutonomousState } from "./state-store.js";
import type { ObservationRelevance } from "./types.js";
import { sanitizeText } from "./validation.js";

const log = createSubsystemLogger("autonomous");

/** Minimum interval between observations to avoid flooding state. */
const MIN_OBSERVATION_INTERVAL_MS = 30_000; // 30 seconds
/** Max length for observation content. */
const MAX_OBSERVATION_LENGTH = 1000;

let lastObservationAt = 0;

/**
 * Record an inbound conversation as an observation in the autonomous state.
 * Called after the main agent processes a message, so the autonomous cycle
 * can later derive goals and context from real conversations.
 *
 * Rate-limited to prevent flooding.
 */
export async function recordConversationObservation(params: {
  channel: string;
  senderId?: string;
  inboundMessage: string;
  outboundReply?: string;
  storePath?: string;
}): Promise<void> {
  const { channel, senderId, inboundMessage, outboundReply, storePath } = params;

  // Rate limit: skip if too recent.
  const now = Date.now();
  if (now - lastObservationAt < MIN_OBSERVATION_INTERVAL_MS) {
    return;
  }

  // Skip empty or trivial messages.
  const trimmed = inboundMessage.trim();
  if (trimmed.length === 0) {
    return;
  }

  try {
    const state = await loadAutonomousState(storePath);

    const source = senderId ? `conversation:${channel}:${senderId}` : `conversation:${channel}`;

    // Build a concise summary of the exchange.
    const inboundSnippet = truncate(sanitizeText(trimmed), 400);
    let content = `User said: "${inboundSnippet}"`;
    if (outboundReply) {
      const replySnippet = truncate(sanitizeText(outboundReply.trim()), 400);
      content += ` → Agent replied: "${replySnippet}"`;
    }
    content = truncate(content, MAX_OBSERVATION_LENGTH);

    // Higher relevance for longer/substantive messages (likely tasks or topics).
    const relevance: ObservationRelevance = trimmed.length > 100 ? "high" : "medium";

    addObservation(state, source, content, relevance);
    await saveAutonomousState(state, storePath);
    lastObservationAt = now;
  } catch (err) {
    // Non-fatal: never let observation recording break the main flow.
    log.debug(
      `failed to record conversation observation: ${err instanceof Error ? err.message : String(err)}`,
    );
  }
}

function truncate(text: string, maxLen: number): string {
  if (text.length <= maxLen) {
    return text;
  }
  return text.slice(0, maxLen - 3) + "...";
}
