import type { ChannelId } from "../channels/plugins/types.js";

export type AutonomousConfig = {
  /** Enable the autonomous loop engine. Default: false. */
  enabled?: boolean;
  /** Which agent runs the autonomous cycles. Defaults to the default agent. */
  agentId?: string;
  /** Primary channel for proactive outbound messages. */
  defaultChannel?: ChannelId;
  /** Default target (e.g. Discord channel ID, Telegram chat ID) for proactive messages. */
  defaultChannelTarget?: string;
  /** Account ID for multi-account channel setups. */
  defaultAccountId?: string;
  /** Interval between autonomous cycles in milliseconds. Default: 300000 (5 min). */
  cycleIntervalMs?: number;
  /** Maximum cycles per hour (safety limit). Default: 12. */
  maxCyclesPerHour?: number;
  /** Initial goals to seed when state is empty. */
  goals?: string[];
  /** Channel IDs to monitor for attention signals. */
  monitorChannels?: string[];
  /** Dry-run mode: cycles run but no external actions are taken. Default: false. */
  dryRun?: boolean;
  /** Model override for autonomous cycle agent turns. */
  model?: string;
  /** Thinking level for autonomous cycles. */
  thinking?: string;
  /** Timeout per cycle in seconds. Default: 120. */
  timeoutSeconds?: number;
};
