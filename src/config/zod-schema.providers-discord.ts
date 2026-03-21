import { z } from "zod";
import { resolveDiscordPreviewStreamMode } from "./discord-preview-streaming.js";
import { ToolPolicySchema } from "./zod-schema.agent-runtime.js";
import {
  ChannelHealthMonitorSchema,
  ChannelHeartbeatVisibilitySchema,
} from "./zod-schema.channels.js";
import {
  BlockStreamingChunkSchema,
  BlockStreamingCoalesceSchema,
  DmConfigSchema,
  DmPolicySchema,
  GroupPolicySchema,
  HexColorSchema,
  MarkdownConfigSchema,
  ProviderCommandsSchema,
  SecretInputSchema,
  ReplyToModeSchema,
  RetryConfigSchema,
  TtsConfigSchema,
  requireAllowlistAllowFrom,
  requireOpenAllowFrom,
} from "./zod-schema.core.js";
import { sensitive } from "./zod-schema.sensitive.js";

const ToolPolicyBySenderSchema = z.record(z.string(), ToolPolicySchema).optional();

const DiscordIdSchema = z
  .union([z.string(), z.number()])
  .refine((value) => typeof value === "string", {
    message: "Discord IDs must be strings (wrap numeric IDs in quotes).",
  });
const DiscordIdListSchema = z.array(DiscordIdSchema);

function normalizeDiscordStreamingConfig(value: { streaming?: unknown; streamMode?: unknown }) {
  value.streaming = resolveDiscordPreviewStreamMode(value);
  delete value.streamMode;
}

export const DiscordDmSchema = z
  .object({
    enabled: z.boolean().optional(),
    policy: DmPolicySchema.optional(),
    allowFrom: DiscordIdListSchema.optional(),
    groupEnabled: z.boolean().optional(),
    groupChannels: DiscordIdListSchema.optional(),
  })
  .strict();

export const DiscordGuildChannelSchema = z
  .object({
    allow: z.boolean().optional(),
    requireMention: z.boolean().optional(),
    ignoreOtherMentions: z.boolean().optional(),
    tools: ToolPolicySchema,
    toolsBySender: ToolPolicyBySenderSchema,
    skills: z.array(z.string()).optional(),
    enabled: z.boolean().optional(),
    users: DiscordIdListSchema.optional(),
    roles: DiscordIdListSchema.optional(),
    systemPrompt: z.string().optional(),
    includeThreadStarter: z.boolean().optional(),
    autoThread: z.boolean().optional(),
    /** Archive duration for auto-created threads in minutes. Discord supports 60, 1440 (1 day), 4320 (3 days), 10080 (1 week). Default: 60. */
    autoArchiveDuration: z
      .union([
        z.enum(["60", "1440", "4320", "10080"]),
        z.literal(60),
        z.literal(1440),
        z.literal(4320),
        z.literal(10080),
      ])
      .optional(),
  })
  .strict();

export const DiscordGuildSchema = z
  .object({
    slug: z.string().optional(),
    requireMention: z.boolean().optional(),
    ignoreOtherMentions: z.boolean().optional(),
    tools: ToolPolicySchema,
    toolsBySender: ToolPolicyBySenderSchema,
    reactionNotifications: z.enum(["off", "own", "all", "allowlist"]).optional(),
    users: DiscordIdListSchema.optional(),
    roles: DiscordIdListSchema.optional(),
    channels: z.record(z.string(), DiscordGuildChannelSchema.optional()).optional(),
  })
  .strict();

const DiscordUiSchema = z
  .object({
    components: z
      .object({
        accentColor: HexColorSchema.optional(),
      })
      .strict()
      .optional(),
  })
  .strict()
  .optional();

const DiscordVoiceAutoJoinSchema = z
  .object({
    guildId: z.string().min(1),
    channelId: z.string().min(1),
  })
  .strict();

const DiscordVoiceSchema = z
  .object({
    enabled: z.boolean().optional(),
    autoJoin: z.array(DiscordVoiceAutoJoinSchema).optional(),
    daveEncryption: z.boolean().optional(),
    decryptionFailureTolerance: z.number().int().min(0).optional(),
    tts: TtsConfigSchema.optional(),
  })
  .strict()
  .optional();

export const DiscordAccountSchema = z
  .object({
    name: z.string().optional(),
    capabilities: z.array(z.string()).optional(),
    markdown: MarkdownConfigSchema,
    enabled: z.boolean().optional(),
    commands: ProviderCommandsSchema,
    configWrites: z.boolean().optional(),
    token: SecretInputSchema.optional().register(sensitive),
    proxy: z.string().optional(),
    allowBots: z.union([z.boolean(), z.literal("mentions")]).optional(),
    dangerouslyAllowNameMatching: z.boolean().optional(),
    groupPolicy: GroupPolicySchema.optional().default("allowlist"),
    historyLimit: z.number().int().min(0).optional(),
    dmHistoryLimit: z.number().int().min(0).optional(),
    dms: z.record(z.string(), DmConfigSchema.optional()).optional(),
    textChunkLimit: z.number().int().positive().optional(),
    chunkMode: z.enum(["length", "newline"]).optional(),
    blockStreaming: z.boolean().optional(),
    blockStreamingCoalesce: BlockStreamingCoalesceSchema.optional(),
    // Canonical streaming mode. Legacy aliases (`streamMode`, boolean `streaming`) are auto-mapped.
    streaming: z.union([z.boolean(), z.enum(["off", "partial", "block", "progress"])]).optional(),
    streamMode: z.enum(["partial", "block", "off"]).optional(),
    draftChunk: BlockStreamingChunkSchema.optional(),
    maxLinesPerMessage: z.number().int().positive().optional(),
    mediaMaxMb: z.number().positive().optional(),
    retry: RetryConfigSchema,
    actions: z
      .object({
        reactions: z.boolean().optional(),
        stickers: z.boolean().optional(),
        emojiUploads: z.boolean().optional(),
        stickerUploads: z.boolean().optional(),
        polls: z.boolean().optional(),
        permissions: z.boolean().optional(),
        messages: z.boolean().optional(),
        threads: z.boolean().optional(),
        pins: z.boolean().optional(),
        search: z.boolean().optional(),
        memberInfo: z.boolean().optional(),
        roleInfo: z.boolean().optional(),
        roles: z.boolean().optional(),
        channelInfo: z.boolean().optional(),
        voiceStatus: z.boolean().optional(),
        events: z.boolean().optional(),
        moderation: z.boolean().optional(),
        channels: z.boolean().optional(),
        presence: z.boolean().optional(),
      })
      .strict()
      .optional(),
    replyToMode: ReplyToModeSchema.optional(),
    // Aliases for channels.discord.dm.policy / channels.discord.dm.allowFrom. Prefer these for
    // inheritance in multi-account setups (shallow merge works; nested dm object doesn't).
    dmPolicy: DmPolicySchema.optional(),
    allowFrom: DiscordIdListSchema.optional(),
    defaultTo: z.string().optional(),
    dm: DiscordDmSchema.optional(),
    guilds: z.record(z.string(), DiscordGuildSchema.optional()).optional(),
    heartbeat: ChannelHeartbeatVisibilitySchema,
    healthMonitor: ChannelHealthMonitorSchema,
    execApprovals: z
      .object({
        enabled: z.boolean().optional(),
        approvers: DiscordIdListSchema.optional(),
        agentFilter: z.array(z.string()).optional(),
        sessionFilter: z.array(z.string()).optional(),
        cleanupAfterResolve: z.boolean().optional(),
        target: z.enum(["dm", "channel", "both"]).optional(),
      })
      .strict()
      .optional(),
    agentComponents: z
      .object({
        enabled: z.boolean().optional(),
      })
      .strict()
      .optional(),
    ui: DiscordUiSchema,
    slashCommand: z
      .object({
        ephemeral: z.boolean().optional(),
      })
      .strict()
      .optional(),
    threadBindings: z
      .object({
        enabled: z.boolean().optional(),
        idleHours: z.number().nonnegative().optional(),
        maxAgeHours: z.number().nonnegative().optional(),
        spawnSubagentSessions: z.boolean().optional(),
        spawnAcpSessions: z.boolean().optional(),
      })
      .strict()
      .optional(),
    intents: z
      .object({
        presence: z.boolean().optional(),
        guildMembers: z.boolean().optional(),
      })
      .strict()
      .optional(),
    voice: DiscordVoiceSchema,
    pluralkit: z
      .object({
        enabled: z.boolean().optional(),
        token: SecretInputSchema.optional().register(sensitive),
      })
      .strict()
      .optional(),
    responsePrefix: z.string().optional(),
    ackReaction: z.string().optional(),
    ackReactionScope: z
      .enum(["group-mentions", "group-all", "direct", "all", "off", "none"])
      .optional(),
    activity: z.string().optional(),
    status: z.enum(["online", "dnd", "idle", "invisible"]).optional(),
    autoPresence: z
      .object({
        enabled: z.boolean().optional(),
        intervalMs: z.number().int().positive().optional(),
        minUpdateIntervalMs: z.number().int().positive().optional(),
        healthyText: z.string().optional(),
        degradedText: z.string().optional(),
        exhaustedText: z.string().optional(),
      })
      .strict()
      .optional(),
    activityType: z
      .union([z.literal(0), z.literal(1), z.literal(2), z.literal(3), z.literal(4), z.literal(5)])
      .optional(),
    activityUrl: z.string().url().optional(),
    inboundWorker: z
      .object({
        runTimeoutMs: z.number().int().nonnegative().optional(),
      })
      .strict()
      .optional(),
    eventQueue: z
      .object({
        listenerTimeout: z.number().int().positive().optional(),
        maxQueueSize: z.number().int().positive().optional(),
        maxConcurrency: z.number().int().positive().optional(),
      })
      .strict()
      .optional(),
  })
  .strict()
  .superRefine((value, ctx) => {
    normalizeDiscordStreamingConfig(value);

    const activityText = typeof value.activity === "string" ? value.activity.trim() : "";
    const hasActivity = Boolean(activityText);
    const hasActivityType = value.activityType !== undefined;
    const activityUrl = typeof value.activityUrl === "string" ? value.activityUrl.trim() : "";
    const hasActivityUrl = Boolean(activityUrl);

    if ((hasActivityType || hasActivityUrl) && !hasActivity) {
      ctx.addIssue({
        code: z.ZodIssueCode.custom,
        message: "channels.discord.activity is required when activityType or activityUrl is set",
        path: ["activity"],
      });
    }

    if (value.activityType === 1 && !hasActivityUrl) {
      ctx.addIssue({
        code: z.ZodIssueCode.custom,
        message: "channels.discord.activityUrl is required when activityType is 1 (Streaming)",
        path: ["activityUrl"],
      });
    }

    if (hasActivityUrl && value.activityType !== 1) {
      ctx.addIssue({
        code: z.ZodIssueCode.custom,
        message: "channels.discord.activityType must be 1 (Streaming) when activityUrl is set",
        path: ["activityType"],
      });
    }

    const autoPresenceInterval = value.autoPresence?.intervalMs;
    const autoPresenceMinUpdate = value.autoPresence?.minUpdateIntervalMs;
    if (
      typeof autoPresenceInterval === "number" &&
      typeof autoPresenceMinUpdate === "number" &&
      autoPresenceMinUpdate > autoPresenceInterval
    ) {
      ctx.addIssue({
        code: z.ZodIssueCode.custom,
        message:
          "channels.discord.autoPresence.minUpdateIntervalMs must be less than or equal to channels.discord.autoPresence.intervalMs",
        path: ["autoPresence", "minUpdateIntervalMs"],
      });
    }

    // DM allowlist validation is enforced at DiscordConfigSchema so account entries
    // can inherit top-level allowFrom via runtime shallow merge.
  });

export const DiscordConfigSchema = DiscordAccountSchema.extend({
  accounts: z.record(z.string(), DiscordAccountSchema.optional()).optional(),
  defaultAccount: z.string().optional(),
}).superRefine((value, ctx) => {
  const dmPolicy = value.dmPolicy ?? value.dm?.policy ?? "pairing";
  const allowFrom = value.allowFrom ?? value.dm?.allowFrom;
  const allowFromPath =
    value.allowFrom !== undefined ? (["allowFrom"] as const) : (["dm", "allowFrom"] as const);
  requireOpenAllowFrom({
    policy: dmPolicy,
    allowFrom,
    ctx,
    path: [...allowFromPath],
    message:
      'channels.discord.dmPolicy="open" requires channels.discord.allowFrom (or channels.discord.dm.allowFrom) to include "*"',
  });
  requireAllowlistAllowFrom({
    policy: dmPolicy,
    allowFrom,
    ctx,
    path: [...allowFromPath],
    message:
      'channels.discord.dmPolicy="allowlist" requires channels.discord.allowFrom (or channels.discord.dm.allowFrom) to contain at least one sender ID',
  });

  if (!value.accounts) {
    return;
  }
  for (const [accountId, account] of Object.entries(value.accounts)) {
    if (!account) {
      continue;
    }
    const effectivePolicy =
      account.dmPolicy ?? account.dm?.policy ?? value.dmPolicy ?? value.dm?.policy ?? "pairing";
    const effectiveAllowFrom =
      account.allowFrom ?? account.dm?.allowFrom ?? value.allowFrom ?? value.dm?.allowFrom;
    requireOpenAllowFrom({
      policy: effectivePolicy,
      allowFrom: effectiveAllowFrom,
      ctx,
      path: ["accounts", accountId, "allowFrom"],
      message:
        'channels.discord.accounts.*.dmPolicy="open" requires channels.discord.accounts.*.allowFrom (or channels.discord.allowFrom) to include "*"',
    });
    requireAllowlistAllowFrom({
      policy: effectivePolicy,
      allowFrom: effectiveAllowFrom,
      ctx,
      path: ["accounts", accountId, "allowFrom"],
      message:
        'channels.discord.accounts.*.dmPolicy="allowlist" requires channels.discord.accounts.*.allowFrom (or channels.discord.allowFrom) to contain at least one sender ID',
    });
  }
});
