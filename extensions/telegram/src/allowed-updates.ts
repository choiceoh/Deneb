import { API_CONSTANTS } from "grammy";

type TelegramUpdateType = (typeof API_CONSTANTS.ALL_UPDATE_TYPES)[number];

/**
 * Explicit allowlist of update types with registered handlers.
 * Keep in sync with bot-handlers.runtime.ts handler registrations.
 */
const HANDLED_UPDATE_TYPES: readonly TelegramUpdateType[] = [
  "message",
  "edited_message",
  "channel_post",
  "edited_channel_post",
  "callback_query",
  "message_reaction",
  "my_chat_member",
  // message:migrate_to_chat_id is a sub-filter of "message", not a separate update type
];

export function resolveTelegramAllowedUpdates(): ReadonlyArray<TelegramUpdateType> {
  return HANDLED_UPDATE_TYPES;
}
