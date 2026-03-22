import type { DenebConfig } from "../config/config.js";

const IGNORED_CHANNEL_CONFIG_KEYS = new Set(["defaults", "modelByChannel"]);

const CHANNEL_ENV_PREFIXES = [["TELEGRAM_", "telegram"]] as const;

function hasNonEmptyString(value: unknown): boolean {
  return typeof value === "string" && value.trim().length > 0;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return Boolean(value) && typeof value === "object" && !Array.isArray(value);
}

export function hasMeaningfulChannelConfig(value: unknown): boolean {
  if (!isRecord(value)) {
    return false;
  }
  return Object.keys(value).some((key) => key !== "enabled");
}

export function listPotentialConfiguredChannelIds(
  cfg: DenebConfig,
  env: NodeJS.ProcessEnv = process.env,
): string[] {
  const configuredChannelIds = new Set<string>();
  const channels = isRecord(cfg.channels) ? cfg.channels : null;
  if (channels) {
    for (const [key, value] of Object.entries(channels)) {
      if (IGNORED_CHANNEL_CONFIG_KEYS.has(key)) {
        continue;
      }
      if (hasMeaningfulChannelConfig(value)) {
        configuredChannelIds.add(key);
      }
    }
  }

  for (const [key, value] of Object.entries(env)) {
    if (!hasNonEmptyString(value)) {
      continue;
    }
    for (const [prefix, channelId] of CHANNEL_ENV_PREFIXES) {
      if (key.startsWith(prefix)) {
        configuredChannelIds.add(channelId);
      }
    }
    if (key === "TELEGRAM_BOT_TOKEN") {
      configuredChannelIds.add("telegram");
    }
  }
  return [...configuredChannelIds];
}

function hasEnvConfiguredChannel(env: NodeJS.ProcessEnv): boolean {
  for (const [key, value] of Object.entries(env)) {
    if (!hasNonEmptyString(value)) {
      continue;
    }
    if (
      CHANNEL_ENV_PREFIXES.some(([prefix]) => key.startsWith(prefix)) ||
      key === "TELEGRAM_BOT_TOKEN"
    ) {
      return true;
    }
  }
  return hasEnvConfiguredChannel(env);
}

export function hasPotentialConfiguredChannels(
  cfg: DenebConfig,
  env: NodeJS.ProcessEnv = process.env,
): boolean {
  const channels = isRecord(cfg.channels) ? cfg.channels : null;
  if (channels) {
    for (const [key, value] of Object.entries(channels)) {
      if (IGNORED_CHANNEL_CONFIG_KEYS.has(key)) {
        continue;
      }
      if (hasMeaningfulChannelConfig(value)) {
        return true;
      }
    }
  }
  return hasEnvConfiguredChannel(env);
}
