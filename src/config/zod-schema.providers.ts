/**
 * zod-schema.providers.ts — Deneb: Telegram-only channel schema.
 */

import { z } from "zod";
import { ChannelHeartbeatVisibilitySchema } from "./zod-schema.channels.js";
import { GroupPolicySchema } from "./zod-schema.core.js";
import { TelegramConfigSchema } from "./zod-schema.providers-core.js";

export * from "./zod-schema.providers-core.js";
export { ChannelHeartbeatVisibilitySchema } from "./zod-schema.channels.js";

const ChannelModelByChannelSchema = z
  .record(z.string(), z.record(z.string(), z.string()))
  .optional();

export const ChannelsSchema = z
  .object({
    defaults: z
      .object({
        groupPolicy: GroupPolicySchema.optional(),
        heartbeat: ChannelHeartbeatVisibilitySchema,
      })
      .strict()
      .optional(),
    modelByChannel: ChannelModelByChannelSchema,
    telegram: TelegramConfigSchema.optional(),
  })
  .passthrough() // Allow extension channel configs
  .optional();
