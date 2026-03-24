import { hasOutboundReplyContent } from "deneb/plugin-sdk/reply-payload";
import { DEFAULT_HEARTBEAT_ACK_MAX_CHARS } from "../../auto-reply/heartbeat.js";
import type { ReplyPayload } from "../../auto-reply/types.js";
import { truncateUtf16Safe } from "../../utils.js";
import { shouldSkipHeartbeatOnlyDelivery } from "../heartbeat-policy.js";

type DeliveryPayload = Pick<
  ReplyPayload,
  "text" | "mediaUrl" | "mediaUrls" | "interactive" | "channelData" | "isError"
>;

const SUMMARY_MAX_CHARS = 2000;

/**
 * Iterate payloads in reverse, first skipping error entries then falling back
 * to include them. Returns the first truthy result from `extract`.
 */
function pickLastFromPayloads<T extends { isError?: boolean }, R>(
  payloads: T[],
  extract: (item: T) => R | undefined,
): R | undefined {
  for (let i = payloads.length - 1; i >= 0; i--) {
    if (payloads[i]?.isError) {
      continue;
    }
    const result = extract(payloads[i]);
    if (result !== undefined) {
      return result;
    }
  }
  for (let i = payloads.length - 1; i >= 0; i--) {
    const result = extract(payloads[i]);
    if (result !== undefined) {
      return result;
    }
  }
  return undefined;
}

export function pickSummaryFromOutput(text: string | undefined) {
  const clean = (text ?? "").trim();
  if (!clean) {
    return undefined;
  }
  return clean.length > SUMMARY_MAX_CHARS
    ? `${truncateUtf16Safe(clean, SUMMARY_MAX_CHARS)}…`
    : clean;
}

export function pickSummaryFromPayloads(
  payloads: Array<{ text?: string | undefined; isError?: boolean }>,
) {
  return pickLastFromPayloads(payloads, (p) => pickSummaryFromOutput(p?.text));
}

export function pickLastNonEmptyTextFromPayloads(
  payloads: Array<{ text?: string | undefined; isError?: boolean }>,
) {
  return pickLastFromPayloads(payloads, (p) => {
    const clean = (p?.text ?? "").trim();
    return clean || undefined;
  });
}

export function pickLastDeliverablePayload(payloads: DeliveryPayload[]) {
  const isDeliverable = (p: DeliveryPayload) => {
    const hasInteractive = (p?.interactive?.blocks?.length ?? 0) > 0;
    const hasChannelData = Object.keys(p?.channelData ?? {}).length > 0;
    return hasOutboundReplyContent(p, { trimText: true }) || hasInteractive || hasChannelData;
  };
  return pickLastFromPayloads(payloads, (p) => (isDeliverable(p) ? p : undefined));
}

/**
 * Check if delivery should be skipped because the agent signaled no user-visible update.
 * Domain-specific alias for `shouldSkipHeartbeatOnlyDelivery`.
 */
export function isHeartbeatOnlyResponse(payloads: DeliveryPayload[], ackMaxChars: number) {
  return shouldSkipHeartbeatOnlyDelivery(payloads, ackMaxChars);
}

export function resolveHeartbeatAckMaxChars(agentCfg?: { heartbeat?: { ackMaxChars?: number } }) {
  const raw = agentCfg?.heartbeat?.ackMaxChars ?? DEFAULT_HEARTBEAT_ACK_MAX_CHARS;
  return Math.max(0, raw);
}
