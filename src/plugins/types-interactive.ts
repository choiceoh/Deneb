import type { ChannelStructuredComponents } from "../channels/plugins/types.js";
import type {
  PluginConversationBinding,
  PluginConversationBindingRequestParams,
  PluginConversationBindingRequestResult,
} from "./types-commands.js";

export type PluginInteractiveChannel = "telegram" | "discord" | "slack";

export type PluginInteractiveButtons = Array<
  Array<{ text: string; callback_data: string; style?: "danger" | "success" | "primary" }>
>;

export type PluginInteractiveTelegramHandlerResult = {
  handled?: boolean;
} | void;

export type PluginInteractiveTelegramHandlerContext = {
  channel: "telegram";
  accountId: string;
  callbackId: string;
  conversationId: string;
  parentConversationId?: string;
  senderId?: string;
  senderUsername?: string;
  threadId?: number;
  isGroup: boolean;
  isForum: boolean;
  auth: {
    isAuthorizedSender: boolean;
  };
  callback: {
    data: string;
    namespace: string;
    payload: string;
    messageId: number;
    chatId: string;
    messageText?: string;
  };
  respond: {
    reply: (params: { text: string; buttons?: PluginInteractiveButtons }) => Promise<void>;
    editMessage: (params: { text: string; buttons?: PluginInteractiveButtons }) => Promise<void>;
    editButtons: (params: { buttons: PluginInteractiveButtons }) => Promise<void>;
    clearButtons: () => Promise<void>;
    deleteMessage: () => Promise<void>;
  };
  requestConversationBinding: (
    params?: PluginConversationBindingRequestParams,
  ) => Promise<PluginConversationBindingRequestResult>;
  detachConversationBinding: () => Promise<{ removed: boolean }>;
  getCurrentConversationBinding: () => Promise<PluginConversationBinding | null>;
};

export type PluginInteractiveDiscordHandlerResult = {
  handled?: boolean;
} | void;

export type PluginInteractiveDiscordHandlerContext = {
  channel: "discord";
  accountId: string;
  interactionId: string;
  conversationId: string;
  parentConversationId?: string;
  guildId?: string;
  senderId?: string;
  senderUsername?: string;
  auth: {
    isAuthorizedSender: boolean;
  };
  interaction: {
    kind: "button" | "select" | "modal";
    data: string;
    namespace: string;
    payload: string;
    messageId?: string;
    values?: string[];
    fields?: Array<{ id: string; name: string; values: string[] }>;
  };
  respond: {
    acknowledge: () => Promise<void>;
    reply: (params: { text: string; ephemeral?: boolean }) => Promise<void>;
    followUp: (params: { text: string; ephemeral?: boolean }) => Promise<void>;
    editMessage: (params: {
      text?: string;
      components?: ChannelStructuredComponents;
    }) => Promise<void>;
    clearComponents: (params?: { text?: string }) => Promise<void>;
  };
  requestConversationBinding: (
    params?: PluginConversationBindingRequestParams,
  ) => Promise<PluginConversationBindingRequestResult>;
  detachConversationBinding: () => Promise<{ removed: boolean }>;
  getCurrentConversationBinding: () => Promise<PluginConversationBinding | null>;
};

export type PluginInteractiveSlackHandlerResult = {
  handled?: boolean;
} | void;

export type PluginInteractiveSlackHandlerContext = {
  channel: "slack";
  accountId: string;
  interactionId: string;
  conversationId: string;
  parentConversationId?: string;
  senderId?: string;
  senderUsername?: string;
  threadId?: string;
  auth: {
    isAuthorizedSender: boolean;
  };
  interaction: {
    kind: "button" | "select";
    data: string;
    namespace: string;
    payload: string;
    actionId: string;
    blockId?: string;
    messageTs?: string;
    threadTs?: string;
    value?: string;
    selectedValues?: string[];
    selectedLabels?: string[];
    triggerId?: string;
    responseUrl?: string;
  };
  respond: {
    acknowledge: () => Promise<void>;
    reply: (params: { text: string; responseType?: "ephemeral" | "in_channel" }) => Promise<void>;
    followUp: (params: {
      text: string;
      responseType?: "ephemeral" | "in_channel";
    }) => Promise<void>;
    editMessage: (params: { text?: string; blocks?: unknown[] }) => Promise<void>;
  };
  requestConversationBinding: (
    params?: PluginConversationBindingRequestParams,
  ) => Promise<PluginConversationBindingRequestResult>;
  detachConversationBinding: () => Promise<{ removed: boolean }>;
  getCurrentConversationBinding: () => Promise<PluginConversationBinding | null>;
};

export type PluginInteractiveTelegramHandlerRegistration = {
  channel: "telegram";
  namespace: string;
  handler: (
    ctx: PluginInteractiveTelegramHandlerContext,
  ) => Promise<PluginInteractiveTelegramHandlerResult> | PluginInteractiveTelegramHandlerResult;
};

export type PluginInteractiveDiscordHandlerRegistration = {
  channel: "discord";
  namespace: string;
  handler: (
    ctx: PluginInteractiveDiscordHandlerContext,
  ) => Promise<PluginInteractiveDiscordHandlerResult> | PluginInteractiveDiscordHandlerResult;
};

export type PluginInteractiveSlackHandlerRegistration = {
  channel: "slack";
  namespace: string;
  handler: (
    ctx: PluginInteractiveSlackHandlerContext,
  ) => Promise<PluginInteractiveSlackHandlerResult> | PluginInteractiveSlackHandlerResult;
};

export type PluginInteractiveHandlerRegistration =
  | PluginInteractiveTelegramHandlerRegistration
  | PluginInteractiveDiscordHandlerRegistration
  | PluginInteractiveSlackHandlerRegistration;
