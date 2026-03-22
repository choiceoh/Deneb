import type { ReplyPayload } from "../auto-reply/types.js";
import type { ChannelId } from "../channels/plugins/types.js";
import type { DenebConfig } from "../config/config.js";
import type { GatewayRequestHandler } from "../gateway/server-methods/types.js";

export type DenebPluginGatewayMethod = {
  method: string;
  handler: GatewayRequestHandler;
};

// =============================================================================
// Plugin Commands
// =============================================================================

/**
 * Context passed to plugin command handlers.
 */
export type PluginCommandContext = {
  /** The sender's identifier (e.g., Telegram user ID) */
  senderId?: string;
  /** The channel/surface (e.g., "telegram", "discord") */
  channel: string;
  /** Provider channel id (e.g., "telegram") */
  channelId?: ChannelId;
  /** Whether the sender is on the allowlist */
  isAuthorizedSender: boolean;
  /** Raw command arguments after the command name */
  args?: string;
  /** The full normalized command body */
  commandBody: string;
  /** Current Deneb configuration */
  config: DenebConfig;
  /** Raw "From" value (channel-scoped id) */
  from?: string;
  /** Raw "To" value (channel-scoped id) */
  to?: string;
  /** Account id for multi-account channels */
  accountId?: string;
  /** Thread/topic id if available */
  messageThreadId?: number;
  requestConversationBinding: (
    params?: PluginConversationBindingRequestParams,
  ) => Promise<PluginConversationBindingRequestResult>;
  detachConversationBinding: () => Promise<{ removed: boolean }>;
  getCurrentConversationBinding: () => Promise<PluginConversationBinding | null>;
};

export type PluginConversationBindingRequestParams = {
  summary?: string;
  detachHint?: string;
};

export type PluginConversationBindingResolutionDecision = "allow-once" | "allow-always" | "deny";

export type PluginConversationBinding = {
  bindingId: string;
  pluginId: string;
  pluginName?: string;
  pluginRoot: string;
  channel: string;
  accountId: string;
  conversationId: string;
  parentConversationId?: string;
  threadId?: string | number;
  boundAt: number;
  summary?: string;
  detachHint?: string;
};

export type PluginConversationBindingRequestResult =
  | {
      status: "bound";
      binding: PluginConversationBinding;
    }
  | {
      status: "pending";
      approvalId: string;
      reply: ReplyPayload;
    }
  | {
      status: "error";
      message: string;
    };

export type PluginConversationBindingResolvedEvent = {
  status: "approved" | "denied";
  binding?: PluginConversationBinding;
  decision: PluginConversationBindingResolutionDecision;
  request: {
    summary?: string;
    detachHint?: string;
    requestedBySenderId?: string;
    conversation: {
      channel: string;
      accountId: string;
      conversationId: string;
      parentConversationId?: string;
      threadId?: string | number;
    };
  };
};

/**
 * Result returned by a plugin command handler.
 */
export type PluginCommandResult = ReplyPayload;

/**
 * Handler function for plugin commands.
 */
export type PluginCommandHandler = (
  ctx: PluginCommandContext,
) => PluginCommandResult | Promise<PluginCommandResult>;

/**
 * Definition for a plugin-registered command.
 */
export type DenebPluginCommandDefinition = {
  /** Command name without leading slash (e.g., "tts") */
  name: string;
  /**
   * Optional native-command aliases for slash/menu surfaces.
   * `default` applies to all native providers unless a provider-specific
   * override exists (for example `{ default: "talkvoice", discord: "voice2" }`).
   */
  nativeNames?: Partial<Record<string, string>> & { default?: string };
  /** Description shown in /help and command menus */
  description: string;
  /** Whether this command accepts arguments */
  acceptsArgs?: boolean;
  /** Whether only authorized senders can use this command (default: true) */
  requireAuth?: boolean;
  /** The handler function */
  handler: PluginCommandHandler;
};
