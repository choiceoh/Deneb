import { homedir } from "os";
import { join } from "path";

export type AuroraConfig = {
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
  /** When true, retroactively delete HEARTBEAT_OK turn cycles from Aurora storage. */
  pruneHeartbeatOk: boolean;
  /** Compression observer configuration for proactive background summarization. */
  observer: {
    enabled: boolean;
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
 * Resolve Aurora configuration with three-tier precedence:
 *   1. Environment variables (highest)
 *   2. Config object (from plugins.entries.aurora.config, with lossless-claw fallback)
 *   3. Hardcoded defaults (lowest)
 */
export function resolveAuroraConfig(
  env: NodeJS.ProcessEnv = process.env,
  pluginConfig?: Record<string, unknown>,
): AuroraConfig {
  const pc = pluginConfig ?? {};

  return {
    enabled:
      env.AURORA_ENABLED !== undefined ? env.AURORA_ENABLED !== "false" : (toBool(pc.enabled) ?? true),
    databasePath:
      env.AURORA_DATABASE_PATH ??
      toStr(pc.dbPath) ??
      toStr(pc.databasePath) ??
      join(homedir(), ".deneb", "aurora.db"),
    contextThreshold: clampNumber(
      toNumber(env.AURORA_CONTEXT_THRESHOLD) ?? toNumber(pc.contextThreshold) ?? 0.75,
      0.1,
      1.0,
    ),
    freshTailCount: toNumber(env.AURORA_FRESH_TAIL_COUNT) ?? toNumber(pc.freshTailCount) ?? 32,
    leafMinFanout: toNumber(env.AURORA_LEAF_MIN_FANOUT) ?? toNumber(pc.leafMinFanout) ?? 8,
    condensedMinFanout:
      toNumber(env.AURORA_CONDENSED_MIN_FANOUT) ?? toNumber(pc.condensedMinFanout) ?? 4,
    condensedMinFanoutHard:
      toNumber(env.AURORA_CONDENSED_MIN_FANOUT_HARD) ?? toNumber(pc.condensedMinFanoutHard) ?? 2,
    incrementalMaxDepth:
      toNumber(env.AURORA_INCREMENTAL_MAX_DEPTH) ?? toNumber(pc.incrementalMaxDepth) ?? 0,
    // DGX SPARK: moderately larger chunks — GPU can handle more context per pass
    leafChunkTokens: toNumber(env.AURORA_LEAF_CHUNK_TOKENS) ?? toNumber(pc.leafChunkTokens) ?? 30000,
    leafTargetTokens: toNumber(env.AURORA_LEAF_TARGET_TOKENS) ?? toNumber(pc.leafTargetTokens) ?? 1500,
    condensedTargetTokens:
      toNumber(env.AURORA_CONDENSED_TARGET_TOKENS) ?? toNumber(pc.condensedTargetTokens) ?? 2500,
    maxExpandTokens: toNumber(env.AURORA_MAX_EXPAND_TOKENS) ?? toNumber(pc.maxExpandTokens) ?? 6000,
    largeFileTokenThreshold:
      toNumber(env.AURORA_LARGE_FILE_TOKEN_THRESHOLD) ??
      toNumber(pc.largeFileThresholdTokens) ??
      toNumber(pc.largeFileTokenThreshold) ??
      35000,
    largeFileSummaryProvider:
      env.AURORA_LARGE_FILE_SUMMARY_PROVIDER?.trim() ?? toStr(pc.largeFileSummaryProvider) ?? "",
    largeFileSummaryModel:
      env.AURORA_LARGE_FILE_SUMMARY_MODEL?.trim() ?? toStr(pc.largeFileSummaryModel) ?? "",
    summaryModel: env.AURORA_SUMMARY_MODEL?.trim() ?? toStr(pc.summaryModel) ?? "",
    summaryProvider: env.AURORA_SUMMARY_PROVIDER?.trim() ?? toStr(pc.summaryProvider) ?? "",
    autocompactDisabled:
      env.AURORA_AUTOCOMPACT_DISABLED !== undefined
        ? env.AURORA_AUTOCOMPACT_DISABLED === "true"
        : (toBool(pc.autocompactDisabled) ?? false),
    timezone: env.TZ ?? toStr(pc.timezone) ?? Intl.DateTimeFormat().resolvedOptions().timeZone,
    pruneHeartbeatOk:
      env.AURORA_PRUNE_HEARTBEAT_OK !== undefined
        ? env.AURORA_PRUNE_HEARTBEAT_OK === "true"
        : (toBool(pc.pruneHeartbeatOk) ?? false),
    observer: {
      enabled:
        env.AURORA_OBSERVER_ENABLED !== undefined
          ? env.AURORA_OBSERVER_ENABLED === "true"
          : (toBool((pc.observer as Record<string, unknown> | undefined)?.enabled) ?? false),
      messageInterval: Math.max(
        1,
        Math.floor(
          toNumber(env.AURORA_OBSERVER_MESSAGE_INTERVAL) ??
            toNumber((pc.observer as Record<string, unknown> | undefined)?.messageInterval) ??
            5,
        ),
      ),
      model:
        env.AURORA_OBSERVER_MODEL?.trim() ??
        toStr((pc.observer as Record<string, unknown> | undefined)?.model) ??
        "",
      provider:
        env.AURORA_OBSERVER_PROVIDER?.trim() ??
        toStr((pc.observer as Record<string, unknown> | undefined)?.provider) ??
        "",
      maxStalenessMs: Math.max(
        1_000,
        toNumber(env.AURORA_OBSERVER_MAX_STALENESS_MS) ??
          toNumber((pc.observer as Record<string, unknown> | undefined)?.maxStalenessMs) ??
          60_000,
      ),
    },
  };
}
