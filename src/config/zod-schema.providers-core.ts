/**
 * zod-schema.providers-core.ts — Deneb: Telegram-only provider schemas.
 *
 * Non-Telegram channel schemas have been removed. Only the Telegram
 * re-export and shared helpers remain.
 */

export {
  TelegramTopicSchema,
  TelegramGroupSchema,
  TelegramDirectSchema,
  TelegramAccountSchemaBase,
  TelegramAccountSchema,
  TelegramConfigSchema,
} from "./zod-schema.providers-telegram.js";
