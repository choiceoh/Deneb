import { shouldMoveSingleAccountChannelKey } from "../channels/plugins/setup-helpers.js";
import type { DenebConfig } from "../config/config.js";
import { resolveTelegramPreviewStreamMode } from "../config/discord-preview-streaming.js";
import { DEFAULT_ACCOUNT_ID } from "../routing/session-key.js";

export function normalizeCompatibilityConfigValues(cfg: DenebConfig): {
  config: DenebConfig;
  changes: string[];
} {
  const changes: string[] = [];
  const NANO_BANANA_SKILL_KEY = "nano-banana-pro";
  const NANO_BANANA_MODEL = "google/gemini-3-pro-image-preview";
  let next: DenebConfig = cfg;

  const isRecord = (value: unknown): value is Record<string, unknown> =>
    Boolean(value) && typeof value === "object" && !Array.isArray(value);

  const normalizePreviewStreamingAliases = (params: {
    entry: Record<string, unknown>;
    pathPrefix: string;
    resolveStreaming: (entry: Record<string, unknown>) => string;
  }): { entry: Record<string, unknown>; changed: boolean } => {
    let updated = params.entry;
    const hadLegacyStreamMode = updated.streamMode !== undefined;
    const beforeStreaming = updated.streaming;
    const resolved = params.resolveStreaming(updated);
    const shouldNormalize =
      hadLegacyStreamMode ||
      typeof beforeStreaming === "boolean" ||
      (typeof beforeStreaming === "string" && beforeStreaming !== resolved);
    if (!shouldNormalize) {
      return { entry: updated, changed: false };
    }

    let changed = false;
    if (beforeStreaming !== resolved) {
      updated = { ...updated, streaming: resolved };
      changed = true;
    }
    if (hadLegacyStreamMode) {
      const { streamMode: _ignored, ...rest } = updated;
      updated = rest;
      changed = true;
      changes.push(
        `Moved ${params.pathPrefix}.streamMode → ${params.pathPrefix}.streaming (${resolved}).`,
      );
    }
    if (typeof beforeStreaming === "boolean") {
      changes.push(`Normalized ${params.pathPrefix}.streaming boolean → enum (${resolved}).`);
    } else if (typeof beforeStreaming === "string" && beforeStreaming !== resolved) {
      changes.push(
        `Normalized ${params.pathPrefix}.streaming (${beforeStreaming}) → (${resolved}).`,
      );
    }

    return { entry: updated, changed };
  };

  const normalizeTelegramStreamingAliases = () => {
    const channels = next.channels as Record<string, unknown> | undefined;
    const rawEntry = channels?.telegram;
    if (!isRecord(rawEntry)) {
      return;
    }

    let updated = rawEntry;
    let changed = false;
    const topLevel = normalizePreviewStreamingAliases({
      entry: updated,
      pathPrefix: "channels.telegram",
      resolveStreaming: resolveTelegramPreviewStreamMode,
    });
    updated = topLevel.entry;
    changed = topLevel.changed;

    const rawAccounts = updated.accounts;
    if (isRecord(rawAccounts)) {
      let accountsChanged = false;
      const accounts = { ...rawAccounts };
      for (const [accountId, rawAccount] of Object.entries(rawAccounts)) {
        if (!isRecord(rawAccount)) {
          continue;
        }
        const accountStreaming = normalizePreviewStreamingAliases({
          entry: rawAccount,
          pathPrefix: `channels.telegram.accounts.${accountId}`,
          resolveStreaming: resolveTelegramPreviewStreamMode,
        });
        if (accountStreaming.changed) {
          accounts[accountId] = accountStreaming.entry;
          accountsChanged = true;
        }
      }
      if (accountsChanged) {
        updated = { ...updated, accounts };
        changed = true;
      }
    }

    if (changed) {
      next = {
        ...next,
        channels: {
          ...next.channels,
          telegram: updated,
        },
      };
    }
  };

  const seedMissingDefaultAccountsFromSingleAccountBase = () => {
    const channels = next.channels as Record<string, unknown> | undefined;
    if (!channels) {
      return;
    }

    let channelsChanged = false;
    const nextChannels = { ...channels };
    for (const [channelId, rawChannel] of Object.entries(channels)) {
      if (!isRecord(rawChannel)) {
        continue;
      }
      const rawAccounts = rawChannel.accounts;
      if (!isRecord(rawAccounts)) {
        continue;
      }
      const accountKeys = Object.keys(rawAccounts);
      if (accountKeys.length === 0) {
        continue;
      }
      const hasDefault = accountKeys.some((key) => key.trim().toLowerCase() === DEFAULT_ACCOUNT_ID);
      if (hasDefault) {
        continue;
      }

      const keysToMove = Object.entries(rawChannel)
        .filter(
          ([key, value]) =>
            key !== "accounts" &&
            key !== "enabled" &&
            value !== undefined &&
            shouldMoveSingleAccountChannelKey({ channelKey: channelId, key }),
        )
        .map(([key]) => key);
      if (keysToMove.length === 0) {
        continue;
      }

      const defaultAccount: Record<string, unknown> = {};
      for (const key of keysToMove) {
        const value = rawChannel[key];
        defaultAccount[key] = value && typeof value === "object" ? structuredClone(value) : value;
      }
      const nextChannel: Record<string, unknown> = {
        ...rawChannel,
      };
      for (const key of keysToMove) {
        delete nextChannel[key];
      }
      nextChannel.accounts = {
        ...rawAccounts,
        [DEFAULT_ACCOUNT_ID]: defaultAccount,
      };

      nextChannels[channelId] = nextChannel;
      channelsChanged = true;
      changes.push(
        `Moved channels.${channelId} single-account top-level values into channels.${channelId}.accounts.default.`,
      );
    }

    if (!channelsChanged) {
      return;
    }
    next = {
      ...next,
      channels: nextChannels as DenebConfig["channels"],
    };
  };

  normalizeTelegramStreamingAliases();
  seedMissingDefaultAccountsFromSingleAccountBase();

  const normalizeLegacyNanoBananaSkill = () => {
    type ModelProviderEntry = Partial<
      NonNullable<NonNullable<DenebConfig["models"]>["providers"]>[string]
    >;
    type ModelsConfigPatch = Partial<NonNullable<DenebConfig["models"]>>;

    const rawSkills = next.skills;
    if (!isRecord(rawSkills)) {
      return;
    }

    let skillsChanged = false;
    let skills = structuredClone(rawSkills);

    if (Array.isArray(skills.allowBundled)) {
      const allowBundled = skills.allowBundled.filter(
        (value) => typeof value !== "string" || value.trim() !== NANO_BANANA_SKILL_KEY,
      );
      if (allowBundled.length !== skills.allowBundled.length) {
        if (allowBundled.length === 0) {
          delete skills.allowBundled;
          changes.push(`Removed skills.allowBundled entry for ${NANO_BANANA_SKILL_KEY}.`);
        } else {
          skills.allowBundled = allowBundled;
          changes.push(`Removed ${NANO_BANANA_SKILL_KEY} from skills.allowBundled.`);
        }
        skillsChanged = true;
      }
    }

    const rawEntries = skills.entries;
    if (!isRecord(rawEntries)) {
      if (skillsChanged) {
        next = { ...next, skills };
      }
      return;
    }

    const rawLegacyEntry = rawEntries[NANO_BANANA_SKILL_KEY];
    if (!isRecord(rawLegacyEntry)) {
      if (skillsChanged) {
        next = { ...next, skills };
      }
      return;
    }

    const existingImageGenerationModel = next.agents?.defaults?.imageGenerationModel;
    if (existingImageGenerationModel === undefined) {
      next = {
        ...next,
        agents: {
          ...next.agents,
          defaults: {
            ...next.agents?.defaults,
            imageGenerationModel: {
              primary: NANO_BANANA_MODEL,
            },
          },
        },
      };
      changes.push(
        `Moved skills.entries.${NANO_BANANA_SKILL_KEY} → agents.defaults.imageGenerationModel.primary (${NANO_BANANA_MODEL}).`,
      );
    }

    const legacyEnv = isRecord(rawLegacyEntry.env) ? rawLegacyEntry.env : undefined;
    const legacyEnvApiKey =
      typeof legacyEnv?.GEMINI_API_KEY === "string" ? legacyEnv.GEMINI_API_KEY.trim() : "";
    const legacyApiKey =
      legacyEnvApiKey ||
      (typeof rawLegacyEntry.apiKey === "string"
        ? rawLegacyEntry.apiKey.trim()
        : rawLegacyEntry.apiKey && isRecord(rawLegacyEntry.apiKey)
          ? structuredClone(rawLegacyEntry.apiKey)
          : undefined);

    const rawModels = (
      isRecord(next.models) ? structuredClone(next.models) : {}
    ) as ModelsConfigPatch;
    const rawProviders = (
      isRecord(rawModels.providers) ? { ...rawModels.providers } : {}
    ) as Record<string, ModelProviderEntry>;
    const rawGoogle = (
      isRecord(rawProviders.google) ? { ...rawProviders.google } : {}
    ) as ModelProviderEntry;
    const hasGoogleApiKey = rawGoogle.apiKey !== undefined;
    if (!hasGoogleApiKey && legacyApiKey) {
      rawGoogle.apiKey = legacyApiKey;
      rawProviders.google = rawGoogle;
      rawModels.providers = rawProviders as NonNullable<DenebConfig["models"]>["providers"];
      next = {
        ...next,
        models: rawModels as DenebConfig["models"],
      };
      changes.push(
        `Moved skills.entries.${NANO_BANANA_SKILL_KEY}.${legacyEnvApiKey ? "env.GEMINI_API_KEY" : "apiKey"} → models.providers.google.apiKey.`,
      );
    }

    const entries = { ...rawEntries };
    delete entries[NANO_BANANA_SKILL_KEY];
    if (Object.keys(entries).length === 0) {
      delete skills.entries;
      changes.push(`Removed legacy skills.entries.${NANO_BANANA_SKILL_KEY}.`);
    } else {
      skills.entries = entries;
      changes.push(`Removed legacy skills.entries.${NANO_BANANA_SKILL_KEY}.`);
    }
    skillsChanged = true;

    if (Object.keys(skills).length === 0) {
      const { skills: _ignored, ...rest } = next;
      next = rest;
      return;
    }

    if (skillsChanged) {
      next = {
        ...next,
        skills,
      };
    }
  };

  normalizeLegacyNanoBananaSkill();

  // Migrate lossless-claw plugin entry: move top-level Aurora keys into its
  // .config sub-object so PluginEntrySchema (.strict()) passes.
  const AURORA_CONFIG_KEYS = new Set([
    "leafTargetTokens",
    "condensedTargetTokens",
    "incrementalMaxDepth",
    "leafChunkTokens",
    "leafMinFanout",
    "condensedMinFanout",
    "condensedMinFanoutHard",
    "maxExpandTokens",
    "contextThreshold",
    "freshTailCount",
    "dbPath",
    "databasePath",
    "largeFileThresholdTokens",
    "largeFileTokenThreshold",
    "largeFileSummaryProvider",
    "largeFileSummaryModel",
    "summaryModel",
    "summaryProvider",
    "autocompactDisabled",
    "timezone",
    "pruneHeartbeatOk",
  ]);
  const auroraEntry = (next.plugins as Record<string, unknown> | undefined)?.entries as
    | Record<string, Record<string, unknown>>
    | undefined;
  const auroraRaw = auroraEntry?.["lossless-claw"];
  if (isRecord(auroraRaw)) {
    const movedKeys: string[] = [];
    const existingConfig = isRecord(auroraRaw.config) ? { ...auroraRaw.config } : {};
    for (const key of Object.keys(auroraRaw)) {
      if (!AURORA_CONFIG_KEYS.has(key)) {
        continue;
      }
      if (!(key in existingConfig)) {
        existingConfig[key] = auroraRaw[key];
      }
      movedKeys.push(key);
    }
    if (movedKeys.length > 0) {
      const cleaned: Record<string, unknown> = {};
      for (const [k, v] of Object.entries(auroraRaw)) {
        if (!AURORA_CONFIG_KEYS.has(k)) {
          cleaned[k] = v;
        }
      }
      cleaned.config = existingConfig;
      next = {
        ...next,
        plugins: {
          ...(next.plugins as Record<string, unknown>),
          entries: {
            ...auroraEntry,
            "lossless-claw": cleaned,
          },
        },
      };
      changes.push(
        `Moved lossless-claw keys into plugins.entries.lossless-claw.config: ${movedKeys.join(", ")}.`,
      );
    }
  }

  // Migrate lossless-claw plugin entry to the canonical "aurora" entry.
  // After nativization the engine reads from plugins.entries.aurora.config,
  // so move the legacy entry there to complete the migration.
  const updatedEntries = (next.plugins as Record<string, unknown> | undefined)?.entries as
    | Record<string, Record<string, unknown>>
    | undefined;
  const legacyAurora = updatedEntries?.["lossless-claw"];
  if (isRecord(legacyAurora)) {
    const existingAuroraEntry = updatedEntries?.["aurora"];
    const mergedAuroraEntry = isRecord(existingAuroraEntry)
      ? { ...legacyAurora, ...existingAuroraEntry }
      : { ...legacyAurora };
    // Merge config sub-objects: existing aurora.config takes precedence over legacy.
    if (isRecord(legacyAurora.config) || isRecord(existingAuroraEntry?.config)) {
      const legacyCfg = isRecord(legacyAurora.config) ? legacyAurora.config : {};
      const existCfg = isRecord(existingAuroraEntry?.config) ? existingAuroraEntry.config : {};
      mergedAuroraEntry.config = { ...legacyCfg, ...existCfg };
    }
    const { "lossless-claw": _removed, ...restEntries } = updatedEntries!;
    next = {
      ...next,
      plugins: {
        ...(next.plugins as Record<string, unknown>),
        entries: {
          ...restEntries,
          aurora: mergedAuroraEntry,
        },
      },
    };
    changes.push("Migrated plugins.entries.lossless-claw to plugins.entries.aurora.");
  }

  return { config: next, changes };
}
