import { buildChannelConfigSchema, TelegramConfigSchema } from "deneb/plugin-sdk/telegram-core";

export const TelegramChannelConfigSchema = buildChannelConfigSchema(TelegramConfigSchema);
