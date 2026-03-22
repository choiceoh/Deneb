import { homedir } from "os";
import { join } from "path";

export type LcmConfig = {
  enabled: boolean;
  databasePath: string;
  contextThreshold: number;
  freshTailCount: number;
  leafMinFanout: number;
  condensedMinFanout: number;
  condensedMinFanoutHard: number;
  incrementalMaxDepth: number;
  leafChunkTokens: number;
  leafTargetTokens: number;
  condensedTargetTokens: number;
  maxExpandTokens: number;
  largeFileTokenThreshold: number;
  /** Provider override for large-file text summarization. */
  largeFileSummaryProvider: string;
  /** Model override for large-file text summarization. */
  largeFileSummaryModel: string;
  /** Model override for conversation summarization. */
  summaryModel: string;
  /** Provider override for conversation summarization. */
  summaryProvider: string;
  autocompactDisabled: boolean;
  /** IANA timezone for timestamps in summaries (from TZ env or system default) */
  timezone: string;
  /** When true, retroactively delete HEARTBEAT_OK turn cycles from LCM storage. */
  pruneHeartbeatOk: boolean;
  /** Compression observer configuration for proactive background summarization. */
  observer: {
    enabled: boolean;
    targetRatio: number;
    messageInterval: number;
    model: string;
    provider: string;
    maxStalenessMs: number;
  };
};

/** Safely coerce an unknown value to a finite number, or return undefined. */
function toNumber(value: unknown): number | undefined {
  if (typeof value === "number" && Number.isFinite(value)) {
    return value;
  }
  if (typeof value === "string") {
    const n = Number(value);
    if (Number.isFinite(n)) {
      return n;
    }
  }
  return undefined;
}

/** Safely coerce an unknown value to a boolean, or return undefined. */
function toBool(value: unknown): boolean | undefined {
  if (typeof value === "boolean") {
    return value;
  }
  if (value === "true") {
    return true;
  }
  if (value === "false") {
    return false;
  }
  return undefined;
}

/** Clamp a number to [min, max] range. */
function clampNumber(value: number, min: number, max: number): number {
  return Math.max(min, Math.min(max, value));
}

/** Safely coerce an unknown value to a trimmed non-empty string, or return undefined. */
function toStr(value: unknown): string | undefined {
  if (typeof value === "string") {
    const trimmed = value.trim();
    return trimmed.length > 0 ? trimmed : undefined;
  }
  return undefined;
}

/**
 * Resolve LCM configuration with three-tier precedence:
 *   1. Environment variables (highest)
 *   2. Config object (from plugins.entries.lcm.config)
 *   3. Hardcoded defaults (lowest)
 */
export function resolveLcmConfig(
  env: NodeJS.ProcessEnv = process.env,
  pluginConfig?: Record<string, unknown>,
): LcmConfig {
  const pc = pluginConfig ?? {};

  return {
    enabled:
      env.LCM_ENABLED !== undefined ? env.LCM_ENABLED !== "false" : (toBool(pc.enabled) ?? true),
    databasePath:
      env.LCM_DATABASE_PATH ??
      toStr(pc.dbPath) ??
      toStr(pc.databasePath) ??
      join(homedir(), ".deneb", "lcm.db"),
    contextThreshold: clampNumber(
      toNumber(env.LCM_CONTEXT_THRESHOLD) ?? toNumber(pc.contextThreshold) ?? 0.75,
      0.1,
      1.0,
    ),
    freshTailCount: toNumber(env.LCM_FRESH_TAIL_COUNT) ?? toNumber(pc.freshTailCount) ?? 32,
    leafMinFanout: toNumber(env.LCM_LEAF_MIN_FANOUT) ?? toNumber(pc.leafMinFanout) ?? 8,
    condensedMinFanout:
      toNumber(env.LCM_CONDENSED_MIN_FANOUT) ?? toNumber(pc.condensedMinFanout) ?? 4,
    condensedMinFanoutHard:
      toNumber(env.LCM_CONDENSED_MIN_FANOUT_HARD) ?? toNumber(pc.condensedMinFanoutHard) ?? 2,
    incrementalMaxDepth:
      toNumber(env.LCM_INCREMENTAL_MAX_DEPTH) ?? toNumber(pc.incrementalMaxDepth) ?? 0,
    leafChunkTokens: toNumber(env.LCM_LEAF_CHUNK_TOKENS) ?? toNumber(pc.leafChunkTokens) ?? 20000,
    leafTargetTokens: toNumber(env.LCM_LEAF_TARGET_TOKENS) ?? toNumber(pc.leafTargetTokens) ?? 1200,
    condensedTargetTokens:
      toNumber(env.LCM_CONDENSED_TARGET_TOKENS) ?? toNumber(pc.condensedTargetTokens) ?? 2000,
    maxExpandTokens: toNumber(env.LCM_MAX_EXPAND_TOKENS) ?? toNumber(pc.maxExpandTokens) ?? 4000,
    largeFileTokenThreshold:
      toNumber(env.LCM_LARGE_FILE_TOKEN_THRESHOLD) ??
      toNumber(pc.largeFileThresholdTokens) ??
      toNumber(pc.largeFileTokenThreshold) ??
      25000,
    largeFileSummaryProvider:
      env.LCM_LARGE_FILE_SUMMARY_PROVIDER?.trim() ?? toStr(pc.largeFileSummaryProvider) ?? "",
    largeFileSummaryModel:
      env.LCM_LARGE_FILE_SUMMARY_MODEL?.trim() ?? toStr(pc.largeFileSummaryModel) ?? "",
    summaryModel: env.LCM_SUMMARY_MODEL?.trim() ?? toStr(pc.summaryModel) ?? "",
    summaryProvider: env.LCM_SUMMARY_PROVIDER?.trim() ?? toStr(pc.summaryProvider) ?? "",
    autocompactDisabled:
      env.LCM_AUTOCOMPACT_DISABLED !== undefined
        ? env.LCM_AUTOCOMPACT_DISABLED === "true"
        : (toBool(pc.autocompactDisabled) ?? false),
    timezone: env.TZ ?? toStr(pc.timezone) ?? Intl.DateTimeFormat().resolvedOptions().timeZone,
    pruneHeartbeatOk:
      env.LCM_PRUNE_HEARTBEAT_OK !== undefined
        ? env.LCM_PRUNE_HEARTBEAT_OK === "true"
        : (toBool(pc.pruneHeartbeatOk) ?? false),
    observer: {
      enabled:
        env.LCM_OBSERVER_ENABLED !== undefined
          ? env.LCM_OBSERVER_ENABLED === "true"
          : (toBool((pc.observer as Record<string, unknown> | undefined)?.enabled) ?? false),
      targetRatio: clampNumber(
        toNumber(env.LCM_OBSERVER_TARGET_RATIO) ??
          toNumber((pc.observer as Record<string, unknown> | undefined)?.targetRatio) ??
          0.2,
        0.05,
        0.5,
      ),
      messageInterval: Math.max(
        1,
        Math.floor(
          toNumber(env.LCM_OBSERVER_MESSAGE_INTERVAL) ??
            toNumber((pc.observer as Record<string, unknown> | undefined)?.messageInterval) ??
            5,
        ),
      ),
      model:
        env.LCM_OBSERVER_MODEL?.trim() ??
        toStr((pc.observer as Record<string, unknown> | undefined)?.model) ??
        "",
      provider:
        env.LCM_OBSERVER_PROVIDER?.trim() ??
        toStr((pc.observer as Record<string, unknown> | undefined)?.provider) ??
        "",
      maxStalenessMs: Math.max(
        1_000,
        toNumber(env.LCM_OBSERVER_MAX_STALENESS_MS) ??
          toNumber((pc.observer as Record<string, unknown> | undefined)?.maxStalenessMs) ??
          60_000,
      ),
    },
  };
}
