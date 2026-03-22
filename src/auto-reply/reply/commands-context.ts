import type { DenebConfig } from "../../config/config.js";
import { normalizeCommandBody } from "../commands-registry.js";
import type { MsgContext } from "../templating.js";
import type { CommandContext } from "./commands-types.js";
import { stripMentions } from "./mentions.js";

export function buildCommandContext(params: {
  ctx: MsgContext;
  cfg: DenebConfig;
  agentId?: string;
  sessionKey?: string;
  isGroup: boolean;
  triggerBodyNormalized: string;
  commandAuthorized: boolean;
}): CommandContext {
  const { ctx, cfg, agentId, sessionKey, isGroup, triggerBodyNormalized } = params;
  const from = (ctx.From ?? "").trim() || undefined;
  const to = (ctx.To ?? "").trim() || undefined;
  const surface = (ctx.Surface ?? ctx.Provider ?? "").trim().toLowerCase();
  const channel = (ctx.Provider ?? surface).trim().toLowerCase();
  const abortKey = sessionKey ?? from ?? to;
  const rawBodyNormalized = triggerBodyNormalized;
  const commandBodyNormalized = normalizeCommandBody(
    isGroup ? stripMentions(rawBodyNormalized, ctx, cfg, agentId) : rawBodyNormalized,
    { botUsername: ctx.BotUsername },
  );

  return {
    surface,
    channel,
    channelId: undefined,
    ownerList: [],
    senderIsOwner: true,
    isAuthorizedSender: true,
    senderId: undefined,
    abortKey,
    rawBodyNormalized,
    commandBodyNormalized,
    from,
    to,
  };
}
