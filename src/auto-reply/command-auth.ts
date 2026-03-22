// Stub: command authorization removed for solo-dev simplification.
import type { ChannelId } from "../channels/plugins/types.js";
import type { DenebConfig } from "../config/config.js";
import type { MsgContext } from "./templating.js";

export type CommandAuthorization = {
  providerId?: ChannelId;
  ownerList: string[];
  senderId?: string;
  senderIsOwner: boolean;
  isAuthorizedSender: boolean;
  from?: string;
  to?: string;
};

export function resolveCommandAuthorization(params: {
  ctx: MsgContext;
  cfg: DenebConfig;
  commandAuthorized: boolean;
}): CommandAuthorization {
  const from = (params.ctx.From ?? "").trim();
  const to = (params.ctx.To ?? "").trim();
  return {
    providerId: undefined,
    ownerList: [],
    senderId: undefined,
    senderIsOwner: true,
    isAuthorizedSender: true,
    from: from || undefined,
    to: to || undefined,
  };
}
