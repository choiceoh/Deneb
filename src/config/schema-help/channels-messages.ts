export const CHANNELS_MESSAGES_HELP: Record<string, string> = {
  messages:
    "Message formatting, acknowledgment, queueing, debounce, and status reaction behavior for inbound/outbound chat flows. Use this section when channel responsiveness or message UX needs adjustment.",
  "messages.messagePrefix":
    "Prefix text prepended to inbound user messages before they are handed to the agent runtime. Use this sparingly for channel context markers and keep it stable across sessions.",
  "messages.responsePrefix":
    "Prefix text prepended to outbound assistant replies before sending to channels. Use for lightweight branding/context tags and avoid long prefixes that reduce content density.",
  "messages.groupChat":
    "Group-message handling controls including mention triggers and history window sizing. Keep mention patterns narrow so group channels do not trigger on every message.",
  "messages.groupChat.mentionPatterns":
    "Safe case-insensitive regex patterns used to detect explicit mentions/trigger phrases in group chats. Use precise patterns to reduce false positives in high-volume channels; invalid or unsafe nested-repetition patterns are ignored.",
  "messages.groupChat.historyLimit":
    "Maximum number of prior group messages loaded as context per turn for group sessions. Use higher values for richer continuity, or lower values for faster and cheaper responses.",
  "messages.queue":
    "Inbound message queue strategy used to buffer bursts before processing turns. Tune this for busy channels where sequential processing or batching behavior matters.",
  "messages.queue.mode":
    'Queue behavior mode: "steer", "followup", "collect", "steer-backlog", "steer+backlog", "queue", or "interrupt". Keep conservative modes unless you intentionally need aggressive interruption/backlog semantics.',
  "messages.queue.byChannel":
    "Per-channel queue mode overrides keyed by provider id (for example telegram). Use this when one channel's traffic pattern needs different queue behavior than global defaults.",
  "messages.queue.debounceMs":
    "Global queue debounce window in milliseconds before processing buffered inbound messages. Use higher values to coalesce rapid bursts, or lower values for reduced response latency.",
  "messages.queue.debounceMsByChannel":
    "Per-channel debounce overrides for queue behavior keyed by provider id. Use this to tune burst handling independently for chat surfaces with different pacing.",
  "messages.queue.cap":
    "Maximum number of queued inbound items retained before drop policy applies. Keep caps bounded in noisy channels so memory usage remains predictable.",
  "messages.queue.drop":
    'Drop strategy when queue cap is exceeded: "old", "new", or "summarize". Use summarize when preserving intent matters, or old/new when deterministic dropping is preferred.',
  "messages.inbound":
    "Direct inbound debounce settings used before queue/turn processing starts. Configure this for provider-specific rapid message bursts from the same sender.",
  "messages.inbound.byChannel":
    "Per-channel inbound debounce overrides keyed by provider id in milliseconds. Use this where some providers send message fragments more aggressively than others.",
  "messages.removeAckAfterReply":
    "Removes the acknowledgment reaction after final reply delivery when enabled. Keep enabled for cleaner UX in channels where persistent ack reactions create clutter.",
  "messages.tts":
    "Text-to-speech policy for reading agent replies aloud on supported voice or audio surfaces. Keep disabled unless voice playback is part of your operator/user workflow.",
  "messages.suppressToolErrors":
    "When true, suppress ⚠️ tool-error warnings from being shown to the user. The agent already sees errors in context and can retry. Default: false.",
  "messages.ackReaction": "Emoji reaction used to acknowledge inbound messages (empty disables).",
  "messages.ackReactionScope":
    'When to send ack reactions ("group-mentions", "group-all", "direct", "all", "off", "none"). "off"/"none" disables ack reactions entirely.',
  "messages.statusReactions":
    "Lifecycle status reactions that update the emoji on the trigger message as the agent progresses (queued → thinking → tool → done/error).",
  "messages.statusReactions.enabled":
    "Enable lifecycle status reactions for Telegram. When enabled, the ack reaction becomes the initial 'queued' state and progresses through thinking, tool, done/error automatically. Default: false.",
  "messages.statusReactions.emojis":
    "Override default status reaction emojis. Keys: thinking, compacting, tool, coding, web, done, error, stallSoft, stallHard. Must be valid Telegram reaction emojis.",
  "messages.statusReactions.timing":
    "Override default timing. Keys: debounceMs (700), stallSoftMs (25000), stallHardMs (60000), doneHoldMs (1500), errorHoldMs (2500).",
  "messages.inbound.debounceMs":
    "Debounce window (ms) for batching rapid inbound messages from the same sender (0 to disable).",
  channels:
    "Channel provider configurations plus shared defaults that control access policies, heartbeat visibility, and per-surface behavior. Keep defaults centralized and override per provider only where required.",
  "channels.telegram":
    "Telegram channel provider configuration including auth tokens, retry behavior, and message rendering controls. Use this section to tune bot behavior for Telegram-specific API semantics.",
  "channels.defaults":
    "Default channel behavior applied across providers when provider-specific settings are not set. Use this to enforce consistent baseline policy before per-provider tuning.",
  "channels.defaults.groupPolicy":
    'Default group policy across channels: "open", "disabled", or "allowlist". Keep "allowlist" for safer production setups unless broad group participation is intentional.',
  "channels.defaults.heartbeat":
    "Default heartbeat visibility settings for status messages emitted by providers/channels. Tune this globally to reduce noisy healthy-state updates while keeping alerts visible.",
  "channels.defaults.heartbeat.showOk":
    "Shows healthy/OK heartbeat status entries when true in channel status outputs. Keep false in noisy environments and enable only when operators need explicit healthy confirmations.",
  "channels.defaults.heartbeat.showAlerts":
    "Shows degraded/error heartbeat alerts when true so operator channels surface problems promptly. Keep enabled in production so broken channel states are visible.",
  "channels.defaults.heartbeat.useIndicator":
    "Enables concise indicator-style heartbeat rendering instead of verbose status text where supported. Use indicator mode for dense dashboards with many active channels.",
  "channels.telegram.configWrites":
    "Allow Telegram to write config in response to channel events/commands (default: true).",
  "channels.telegram.botToken":
    "Telegram bot token used to authenticate Bot API requests for this account/provider config. Use secret/env substitution and rotate tokens if exposure is suspected.",
  "channels.telegram.capabilities.inlineButtons":
    "Enable Telegram inline button components for supported command and interaction surfaces. Disable if your deployment needs plain-text-only compatibility behavior.",
  "channels.telegram.execApprovals":
    "Telegram-native exec approval routing and approver authorization. Enable this only when Telegram should act as an explicit exec-approval client for the selected bot account.",
  "channels.telegram.execApprovals.enabled":
    "Enable Telegram exec approvals for this account. When false or unset, Telegram messages/buttons cannot approve exec requests.",
  "channels.telegram.execApprovals.approvers":
    "Telegram user IDs allowed to approve exec requests for this bot account. Use numeric Telegram user IDs; prompts are only delivered to these approvers when target includes dm.",
  "channels.telegram.execApprovals.agentFilter":
    'Optional allowlist of agent IDs eligible for Telegram exec approvals, for example `["main", "ops-agent"]`. Use this to keep approval prompts scoped to the agents you actually operate from Telegram.',
  "channels.telegram.execApprovals.sessionFilter":
    "Optional session-key filters matched as substring or regex-style patterns before Telegram approval routing is used. Use narrow patterns so Telegram approvals only appear for intended sessions.",
  "channels.telegram.execApprovals.target":
    'Controls where Telegram approval prompts are sent: "dm" sends to approver DMs (default), "channel" sends to the originating Telegram chat/topic, and "both" sends to both. Channel delivery exposes the command text to the chat, so only use it in trusted groups/topics.',
  "channels.telegram.commands.native": 'Override native commands for Telegram (bool or "auto").',
  "channels.telegram.commands.nativeSkills":
    'Override native skill commands for Telegram (bool or "auto").',
  "channels.telegram.dmPolicy":
    'Direct message access control ("pairing" recommended). "open" requires channels.telegram.allowFrom=["*"].',
  "channels.telegram.streaming":
    'Unified Telegram stream preview mode: "off" | "partial" | "block" | "progress" (default: "partial"). "progress" maps to "partial" on Telegram. Legacy boolean/streamMode keys are auto-mapped.',
  "channels.telegram.retry.attempts":
    "Max retry attempts for outbound Telegram API calls (default: 3).",
  "channels.telegram.retry.minDelayMs": "Minimum retry delay in ms for Telegram outbound calls.",
  "channels.telegram.retry.maxDelayMs":
    "Maximum retry delay cap in ms for Telegram outbound calls.",
  "channels.telegram.retry.jitter": "Jitter factor (0-1) applied to Telegram retry delays.",
  "channels.telegram.network.autoSelectFamily":
    "Override Node autoSelectFamily for Telegram (true=enable, false=disable).",
  "channels.telegram.timeoutSeconds":
    "Max seconds before Telegram API requests are aborted (default: 500 per grammY).",
  "channels.telegram.silentErrorReplies":
    "When true, Telegram bot replies marked as errors are sent silently (no notification sound). Default: false.",
  "channels.telegram.threadBindings.enabled":
    "Enable Telegram conversation binding features (/focus, /unfocus, /agents, and /session idle|max-age). Overrides session.threadBindings.enabled when set.",
  "channels.telegram.threadBindings.idleHours":
    "Inactivity window in hours for Telegram bound sessions. Set 0 to disable idle auto-unfocus (default: 24). Overrides session.threadBindings.idleHours when set.",
  "channels.telegram.threadBindings.maxAgeHours":
    "Optional hard max age in hours for Telegram bound sessions. Set 0 to disable hard cap (default: 0). Overrides session.threadBindings.maxAgeHours when set.",
  "channels.telegram.threadBindings.spawnSubagentSessions":
    "Allow subagent spawns with thread=true to auto-bind Telegram current conversations when supported.",
  "channels.telegram.threadBindings.spawnAcpSessions":
    "Allow ACP spawns with thread=true to auto-bind Telegram current conversations when supported.",
};
