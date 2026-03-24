// Outbound adapter stubs for removed extensions.
// Kept as lightweight stubs so test files that import them still compile.

import type { ChannelOutboundAdapter } from "../src/channels/plugins/types.js";
import { resolveOutboundSendDep } from "../src/infra/outbound/send-deps.js";

type SendFn = (to: string, text: string, opts?: unknown) => Promise<{ messageId: string }>;

function createDepBasedStub(
  channelId: "discord" | "imessage" | "signal" | "slack" | "whatsapp",
): ChannelOutboundAdapter {
  return {
    deliveryMode: "direct",
    sendText: async (ctx) => {
      const send = resolveOutboundSendDep<SendFn>(ctx.deps, channelId);
      if (!send) {
        throw new Error(`No send dep for ${channelId}`);
      }
      const result = await send(ctx.to, ctx.text);
      return { channel: channelId, ...result };
    },
    sendMedia: async (ctx) => {
      const send = resolveOutboundSendDep<SendFn>(ctx.deps, channelId);
      if (!send) {
        throw new Error(`No send dep for ${channelId}`);
      }
      const result = await send(ctx.to, ctx.text, { mediaUrl: ctx.mediaUrl });
      return { channel: channelId, ...result };
    },
  };
}

export const discordOutbound: ChannelOutboundAdapter = createDepBasedStub("discord");
export const imessageOutbound: ChannelOutboundAdapter = createDepBasedStub("imessage");
export const signalOutbound: ChannelOutboundAdapter = createDepBasedStub("signal");
export const slackOutbound: ChannelOutboundAdapter = createDepBasedStub("slack");
export { telegramOutbound } from "../extensions/telegram/src/outbound-adapter.js";

// WhatsApp stub includes resolveTarget for phone number normalization since
// the original extension is removed but tests still exercise target resolution.
function normalizeWhatsAppTarget(raw: string): string | null {
  const trimmed = raw.trim().replace(/^whatsapp:/i, "");
  if (!trimmed) {
    return null;
  }
  // Group JIDs pass through with lowercase.
  if (trimmed.includes("@")) {
    return trimmed.toLowerCase();
  }
  // Strip non-digit/+ characters and normalize phone numbers.
  const digits = trimmed.replace(/[^\d+]/g, "");
  if (!digits || digits.replace(/\+/g, "").length < 4) {
    return null;
  }
  return digits.startsWith("+") ? digits : `+${digits}`;
}

function simpleChunker(text: string, limit: number): string[] {
  if (!text || limit <= 0) {
    return text ? [text] : [];
  }
  const chunks: string[] = [];
  for (let i = 0; i < text.length; i += limit) {
    chunks.push(text.slice(i, i + limit));
  }
  return chunks;
}

export const whatsappOutbound: ChannelOutboundAdapter = {
  ...createDepBasedStub("whatsapp"),
  chunker: simpleChunker,
  resolveTarget: ({ to, allowFrom, mode }) => {
    const normalized = to ? normalizeWhatsAppTarget(to) : null;
    if (normalized) {
      return { ok: true, to: normalized };
    }
    if (mode === "implicit" && allowFrom?.length) {
      for (const entry of allowFrom) {
        const fallback = normalizeWhatsAppTarget(entry);
        if (fallback) {
          return { ok: true, to: fallback };
        }
      }
    }
    return {
      ok: false,
      error: new Error("WhatsApp target is required. Specify a phone number or group JID."),
    };
  },
};
