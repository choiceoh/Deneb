import type { DenebConfig } from "../../config/config.js";
import type { DmPolicy, GroupPolicy } from "../../config/types.js";
import { DEFAULT_ACCOUNT_ID, normalizeAccountId } from "../../routing/session-key.js";
import type { WizardPrompter } from "../../wizard/prompts.js";
import {
  moveSingleAccountChannelSectionToDefaultAccount,
  patchScopedAccountConfig,
} from "./setup-helpers.js";
import { addWildcardAllowFrom, resolveParsedAllowFromEntries } from "./setup-wizard-parse.js";
import type {
  ChannelSetupDmPolicy,
  PromptAccountId,
  PromptAccountIdParams,
} from "./setup-wizard-types.js";
import type { ChannelSetupWizard } from "./setup-wizard.js";

export const promptAccountId: PromptAccountId = async (params: PromptAccountIdParams) => {
  const existingIds = params.listAccountIds(params.cfg);
  const initial = params.currentId?.trim() || params.defaultAccountId || DEFAULT_ACCOUNT_ID;
  const choice = await params.prompter.select({
    message: `${params.label} account`,
    options: [
      ...existingIds.map((id) => ({
        value: id,
        label: id === DEFAULT_ACCOUNT_ID ? "default (primary)" : id,
      })),
      { value: "__new__", label: "Add a new account" },
    ],
    initialValue: initial,
  });

  if (choice !== "__new__") {
    return normalizeAccountId(choice);
  }

  const entered = await params.prompter.text({
    message: `New ${params.label} account id`,
    validate: (value) => (value?.trim() ? undefined : "Required"),
  });
  const normalized = normalizeAccountId(String(entered));
  if (String(entered).trim() !== normalized) {
    await params.prompter.note(
      `Normalized account id to "${normalized}".`,
      `${params.label} account`,
    );
  }
  return normalized;
};

export function resolveSetupAccountId(params: {
  accountId?: string;
  defaultAccountId: string;
}): string {
  return params.accountId?.trim() ? normalizeAccountId(params.accountId) : params.defaultAccountId;
}

export async function resolveAccountIdForConfigure(params: {
  cfg: DenebConfig;
  prompter: WizardPrompter;
  label: string;
  accountOverride?: string;
  shouldPromptAccountIds: boolean;
  listAccountIds: (cfg: DenebConfig) => string[];
  defaultAccountId: string;
}): Promise<string> {
  const override = params.accountOverride?.trim();
  let accountId = override ? normalizeAccountId(override) : params.defaultAccountId;
  if (params.shouldPromptAccountIds && !override) {
    accountId = await promptAccountId({
      cfg: params.cfg,
      prompter: params.prompter,
      label: params.label,
      currentId: accountId,
      listAccountIds: params.listAccountIds,
      defaultAccountId: params.defaultAccountId,
    });
  }
  return accountId;
}

export function patchTopLevelChannelConfigSection(params: {
  cfg: DenebConfig;
  channel: string;
  enabled?: boolean;
  clearFields?: string[];
  patch: Record<string, unknown>;
}): DenebConfig {
  const channelConfig = {
    ...(params.cfg.channels?.[params.channel] as Record<string, unknown> | undefined),
  };
  for (const field of params.clearFields ?? []) {
    delete channelConfig[field];
  }
  return {
    ...params.cfg,
    channels: {
      ...params.cfg.channels,
      [params.channel]: {
        ...channelConfig,
        ...(params.enabled ? { enabled: true } : {}),
        ...params.patch,
      },
    },
  };
}

export function patchNestedChannelConfigSection(params: {
  cfg: DenebConfig;
  channel: string;
  section: string;
  enabled?: boolean;
  clearFields?: string[];
  patch: Record<string, unknown>;
}): DenebConfig {
  const channelConfig = {
    ...(params.cfg.channels?.[params.channel] as Record<string, unknown> | undefined),
  };
  const sectionConfig = {
    ...(channelConfig[params.section] as Record<string, unknown> | undefined),
  };
  for (const field of params.clearFields ?? []) {
    delete sectionConfig[field];
  }
  return {
    ...params.cfg,
    channels: {
      ...params.cfg.channels,
      [params.channel]: {
        ...channelConfig,
        ...(params.enabled ? { enabled: true } : {}),
        [params.section]: {
          ...sectionConfig,
          ...params.patch,
        },
      },
    },
  };
}

export function setTopLevelChannelAllowFrom(params: {
  cfg: DenebConfig;
  channel: string;
  allowFrom: string[];
  enabled?: boolean;
}): DenebConfig {
  return patchTopLevelChannelConfigSection({
    cfg: params.cfg,
    channel: params.channel,
    enabled: params.enabled,
    patch: { allowFrom: params.allowFrom },
  });
}

export function setNestedChannelAllowFrom(params: {
  cfg: DenebConfig;
  channel: string;
  section: string;
  allowFrom: string[];
  enabled?: boolean;
}): DenebConfig {
  return patchNestedChannelConfigSection({
    cfg: params.cfg,
    channel: params.channel,
    section: params.section,
    enabled: params.enabled,
    patch: { allowFrom: params.allowFrom },
  });
}

export function setTopLevelChannelDmPolicyWithAllowFrom(params: {
  cfg: DenebConfig;
  channel: string;
  dmPolicy: DmPolicy;
  getAllowFrom?: (cfg: DenebConfig) => Array<string | number> | undefined;
}): DenebConfig {
  const channelConfig =
    (params.cfg.channels?.[params.channel] as Record<string, unknown> | undefined) ?? {};
  const existingAllowFrom =
    params.getAllowFrom?.(params.cfg) ??
    (channelConfig.allowFrom as Array<string | number> | undefined) ??
    undefined;
  const allowFrom =
    params.dmPolicy === "open" ? addWildcardAllowFrom(existingAllowFrom) : undefined;
  return patchTopLevelChannelConfigSection({
    cfg: params.cfg,
    channel: params.channel,
    patch: {
      dmPolicy: params.dmPolicy,
      ...(allowFrom ? { allowFrom } : {}),
    },
  });
}

export function setNestedChannelDmPolicyWithAllowFrom(params: {
  cfg: DenebConfig;
  channel: string;
  section: string;
  dmPolicy: DmPolicy;
  getAllowFrom?: (cfg: DenebConfig) => Array<string | number> | undefined;
  enabled?: boolean;
}): DenebConfig {
  const channelConfig =
    (params.cfg.channels?.[params.channel] as Record<string, unknown> | undefined) ?? {};
  const sectionConfig =
    (channelConfig[params.section] as Record<string, unknown> | undefined) ?? {};
  const existingAllowFrom =
    params.getAllowFrom?.(params.cfg) ??
    (sectionConfig.allowFrom as Array<string | number> | undefined) ??
    undefined;
  const allowFrom =
    params.dmPolicy === "open" ? addWildcardAllowFrom(existingAllowFrom) : undefined;
  return patchNestedChannelConfigSection({
    cfg: params.cfg,
    channel: params.channel,
    section: params.section,
    enabled: params.enabled,
    patch: {
      policy: params.dmPolicy,
      ...(allowFrom ? { allowFrom } : {}),
    },
  });
}

export function setTopLevelChannelGroupPolicy(params: {
  cfg: DenebConfig;
  channel: string;
  groupPolicy: GroupPolicy;
  enabled?: boolean;
}): DenebConfig {
  return patchTopLevelChannelConfigSection({
    cfg: params.cfg,
    channel: params.channel,
    enabled: params.enabled,
    patch: { groupPolicy: params.groupPolicy },
  });
}

export function createTopLevelChannelDmPolicy(params: {
  label: string;
  channel: string;
  policyKey: string;
  allowFromKey: string;
  getCurrent: (cfg: DenebConfig) => DmPolicy;
  promptAllowFrom?: ChannelSetupDmPolicy["promptAllowFrom"];
  getAllowFrom?: (cfg: DenebConfig) => Array<string | number> | undefined;
}): ChannelSetupDmPolicy {
  const setPolicy = createTopLevelChannelDmPolicySetter({
    channel: params.channel,
    getAllowFrom: params.getAllowFrom,
  });
  return {
    label: params.label,
    channel: params.channel,
    policyKey: params.policyKey,
    allowFromKey: params.allowFromKey,
    getCurrent: params.getCurrent,
    setPolicy,
    ...(params.promptAllowFrom ? { promptAllowFrom: params.promptAllowFrom } : {}),
  };
}

export function createNestedChannelDmPolicy(params: {
  label: string;
  channel: string;
  section: string;
  policyKey: string;
  allowFromKey: string;
  getCurrent: (cfg: DenebConfig) => DmPolicy;
  promptAllowFrom?: ChannelSetupDmPolicy["promptAllowFrom"];
  getAllowFrom?: (cfg: DenebConfig) => Array<string | number> | undefined;
  enabled?: boolean;
}): ChannelSetupDmPolicy {
  const setPolicy = createNestedChannelDmPolicySetter({
    channel: params.channel,
    section: params.section,
    getAllowFrom: params.getAllowFrom,
    enabled: params.enabled,
  });
  return {
    label: params.label,
    channel: params.channel,
    policyKey: params.policyKey,
    allowFromKey: params.allowFromKey,
    getCurrent: params.getCurrent,
    setPolicy,
    ...(params.promptAllowFrom ? { promptAllowFrom: params.promptAllowFrom } : {}),
  };
}

export function createTopLevelChannelDmPolicySetter(params: {
  channel: string;
  getAllowFrom?: (cfg: DenebConfig) => Array<string | number> | undefined;
}): (cfg: DenebConfig, dmPolicy: DmPolicy) => DenebConfig {
  return (cfg, dmPolicy) =>
    setTopLevelChannelDmPolicyWithAllowFrom({
      cfg,
      channel: params.channel,
      dmPolicy,
      getAllowFrom: params.getAllowFrom,
    });
}

export function createNestedChannelDmPolicySetter(params: {
  channel: string;
  section: string;
  getAllowFrom?: (cfg: DenebConfig) => Array<string | number> | undefined;
  enabled?: boolean;
}): (cfg: DenebConfig, dmPolicy: DmPolicy) => DenebConfig {
  return (cfg, dmPolicy) =>
    setNestedChannelDmPolicyWithAllowFrom({
      cfg,
      channel: params.channel,
      section: params.section,
      dmPolicy,
      getAllowFrom: params.getAllowFrom,
      enabled: params.enabled,
    });
}

export function createTopLevelChannelAllowFromSetter(params: {
  channel: string;
  enabled?: boolean;
}): (cfg: DenebConfig, allowFrom: string[]) => DenebConfig {
  return (cfg, allowFrom) =>
    setTopLevelChannelAllowFrom({
      cfg,
      channel: params.channel,
      allowFrom,
      enabled: params.enabled,
    });
}

export function createNestedChannelAllowFromSetter(params: {
  channel: string;
  section: string;
  enabled?: boolean;
}): (cfg: DenebConfig, allowFrom: string[]) => DenebConfig {
  return (cfg, allowFrom) =>
    setNestedChannelAllowFrom({
      cfg,
      channel: params.channel,
      section: params.section,
      allowFrom,
      enabled: params.enabled,
    });
}

export function createTopLevelChannelGroupPolicySetter(params: {
  channel: string;
  enabled?: boolean;
}): (cfg: DenebConfig, groupPolicy: "open" | "allowlist" | "disabled") => DenebConfig {
  return (cfg, groupPolicy) =>
    setTopLevelChannelGroupPolicy({
      cfg,
      channel: params.channel,
      groupPolicy,
      enabled: params.enabled,
    });
}

export function setChannelDmPolicyWithAllowFrom(params: {
  cfg: DenebConfig;
  channel: "imessage" | "signal" | "telegram";
  dmPolicy: DmPolicy;
}): DenebConfig {
  const { cfg, channel, dmPolicy } = params;
  const allowFrom =
    dmPolicy === "open"
      ? addWildcardAllowFrom(
          cfg.channels?.[channel]?.allowFrom as (string | number)[] | null | undefined,
        )
      : undefined;
  return {
    ...cfg,
    channels: {
      ...cfg.channels,
      [channel]: {
        ...cfg.channels?.[channel],
        dmPolicy,
        ...(allowFrom ? { allowFrom } : {}),
      },
    },
  };
}

type LegacyDmChannel = "discord" | "slack";

export function setLegacyChannelDmPolicyWithAllowFrom(params: {
  cfg: DenebConfig;
  channel: LegacyDmChannel;
  dmPolicy: DmPolicy;
}): DenebConfig {
  const channelConfig = (params.cfg.channels?.[params.channel] as
    | {
        allowFrom?: Array<string | number>;
        dm?: { allowFrom?: Array<string | number> };
      }
    | undefined) ?? {
    allowFrom: undefined,
    dm: undefined,
  };
  const existingAllowFrom = channelConfig.allowFrom ?? channelConfig.dm?.allowFrom;
  const allowFrom =
    params.dmPolicy === "open" ? addWildcardAllowFrom(existingAllowFrom) : undefined;
  return patchLegacyDmChannelConfig({
    cfg: params.cfg,
    channel: params.channel,
    patch: {
      dmPolicy: params.dmPolicy,
      ...(allowFrom ? { allowFrom } : {}),
    },
  });
}

export function setLegacyChannelAllowFrom(params: {
  cfg: DenebConfig;
  channel: LegacyDmChannel;
  allowFrom: string[];
}): DenebConfig {
  return patchLegacyDmChannelConfig({
    cfg: params.cfg,
    channel: params.channel,
    patch: { allowFrom: params.allowFrom },
  });
}

export function setAccountGroupPolicyForChannel(params: {
  cfg: DenebConfig;
  channel: "discord" | "slack";
  accountId: string;
  groupPolicy: GroupPolicy;
}): DenebConfig {
  return patchChannelConfigForAccount({
    cfg: params.cfg,
    channel: params.channel,
    accountId: params.accountId,
    patch: { groupPolicy: params.groupPolicy },
  });
}

export function setAccountDmAllowFromForChannel(params: {
  cfg: DenebConfig;
  channel: "discord" | "slack";
  accountId: string;
  allowFrom: string[];
}): DenebConfig {
  return patchChannelConfigForAccount({
    cfg: params.cfg,
    channel: params.channel,
    accountId: params.accountId,
    patch: { dmPolicy: "allowlist", allowFrom: params.allowFrom },
  });
}

export function createLegacyCompatChannelDmPolicy(params: {
  label: string;
  channel: LegacyDmChannel;
  promptAllowFrom?: ChannelSetupDmPolicy["promptAllowFrom"];
}): ChannelSetupDmPolicy {
  return {
    label: params.label,
    channel: params.channel,
    policyKey: `channels.${params.channel}.dmPolicy`,
    allowFromKey: `channels.${params.channel}.allowFrom`,
    getCurrent: (cfg) =>
      (
        cfg.channels?.[params.channel] as
          | {
              dmPolicy?: DmPolicy;
              dm?: { policy?: DmPolicy };
            }
          | undefined
      )?.dmPolicy ??
      (
        cfg.channels?.[params.channel] as
          | {
              dmPolicy?: DmPolicy;
              dm?: { policy?: DmPolicy };
            }
          | undefined
      )?.dm?.policy ??
      "pairing",
    setPolicy: (cfg, policy) =>
      setLegacyChannelDmPolicyWithAllowFrom({
        cfg,
        channel: params.channel,
        dmPolicy: policy,
      }),
    ...(params.promptAllowFrom ? { promptAllowFrom: params.promptAllowFrom } : {}),
  };
}

export function createAccountScopedAllowFromSection(params: {
  channel: "discord" | "slack";
  credentialInputKey?: NonNullable<ChannelSetupWizard["allowFrom"]>["credentialInputKey"];
  helpTitle?: string;
  helpLines?: string[];
  message: string;
  placeholder: string;
  invalidWithoutCredentialNote: string;
  parseId: NonNullable<NonNullable<ChannelSetupWizard["allowFrom"]>["parseId"]>;
  resolveEntries: NonNullable<NonNullable<ChannelSetupWizard["allowFrom"]>["resolveEntries"]>;
}): NonNullable<ChannelSetupWizard["allowFrom"]> {
  return {
    ...(params.helpTitle ? { helpTitle: params.helpTitle } : {}),
    ...(params.helpLines ? { helpLines: params.helpLines } : {}),
    ...(params.credentialInputKey ? { credentialInputKey: params.credentialInputKey } : {}),
    message: params.message,
    placeholder: params.placeholder,
    invalidWithoutCredentialNote: params.invalidWithoutCredentialNote,
    parseId: params.parseId,
    resolveEntries: params.resolveEntries,
    apply: ({ cfg, accountId, allowFrom }) =>
      setAccountDmAllowFromForChannel({
        cfg,
        channel: params.channel,
        accountId,
        allowFrom,
      }),
  };
}

export function createAccountScopedGroupAccessSection<TResolved>(params: {
  channel: "discord" | "slack";
  label: string;
  placeholder: string;
  helpTitle?: string;
  helpLines?: string[];
  skipAllowlistEntries?: boolean;
  currentPolicy: NonNullable<ChannelSetupWizard["groupAccess"]>["currentPolicy"];
  currentEntries: NonNullable<ChannelSetupWizard["groupAccess"]>["currentEntries"];
  updatePrompt: NonNullable<ChannelSetupWizard["groupAccess"]>["updatePrompt"];
  resolveAllowlist?: NonNullable<
    NonNullable<ChannelSetupWizard["groupAccess"]>["resolveAllowlist"]
  >;
  fallbackResolved: (entries: string[]) => TResolved;
  applyAllowlist: (params: {
    cfg: DenebConfig;
    accountId: string;
    resolved: TResolved;
  }) => DenebConfig;
}): NonNullable<ChannelSetupWizard["groupAccess"]> {
  return {
    label: params.label,
    placeholder: params.placeholder,
    ...(params.helpTitle ? { helpTitle: params.helpTitle } : {}),
    ...(params.helpLines ? { helpLines: params.helpLines } : {}),
    ...(params.skipAllowlistEntries ? { skipAllowlistEntries: true } : {}),
    currentPolicy: params.currentPolicy,
    currentEntries: params.currentEntries,
    updatePrompt: params.updatePrompt,
    setPolicy: ({ cfg, accountId, policy }) =>
      setAccountGroupPolicyForChannel({
        cfg,
        channel: params.channel,
        accountId,
        groupPolicy: policy,
      }),
    ...(params.resolveAllowlist
      ? {
          resolveAllowlist: ({ cfg, accountId, credentialValues, entries, prompter }) =>
            resolveGroupAllowlistWithLookupNotes({
              label: params.label,
              prompter,
              entries,
              fallback: params.fallbackResolved(entries),
              resolve: async () =>
                await params.resolveAllowlist!({
                  cfg,
                  accountId,
                  credentialValues,
                  entries,
                  prompter,
                }),
            }),
        }
      : {}),
    applyAllowlist: ({ cfg, accountId, resolved }) =>
      params.applyAllowlist({
        cfg,
        accountId,
        resolved: resolved as TResolved,
      }),
  };
}

type AccountScopedChannel =
  | "bluebubbles"
  | "discord"
  | "imessage"
  | "line"
  | "signal"
  | "slack"
  | "telegram";

export function patchLegacyDmChannelConfig(params: {
  cfg: DenebConfig;
  channel: LegacyDmChannel;
  patch: Record<string, unknown>;
}): DenebConfig {
  const { cfg, channel, patch } = params;
  const channelConfig = (cfg.channels?.[channel] as Record<string, unknown> | undefined) ?? {};
  const dmConfig = (channelConfig.dm as Record<string, unknown> | undefined) ?? {};
  return {
    ...cfg,
    channels: {
      ...cfg.channels,
      [channel]: {
        ...channelConfig,
        ...patch,
        dm: {
          ...dmConfig,
          enabled: typeof dmConfig.enabled === "boolean" ? dmConfig.enabled : true,
        },
      },
    },
  };
}

export function setSetupChannelEnabled(
  cfg: DenebConfig,
  channel: string,
  enabled: boolean,
): DenebConfig {
  const channelConfig = (cfg.channels?.[channel] as Record<string, unknown> | undefined) ?? {};
  return {
    ...cfg,
    channels: {
      ...cfg.channels,
      [channel]: {
        ...channelConfig,
        enabled,
      },
    },
  };
}

function patchConfigForScopedAccount(params: {
  cfg: DenebConfig;
  channel: AccountScopedChannel;
  accountId: string;
  patch: Record<string, unknown>;
  ensureEnabled: boolean;
}): DenebConfig {
  const { cfg, channel, accountId, patch, ensureEnabled } = params;
  const seededCfg =
    accountId === DEFAULT_ACCOUNT_ID
      ? cfg
      : moveSingleAccountChannelSectionToDefaultAccount({
          cfg,
          channelKey: channel,
        });
  return patchScopedAccountConfig({
    cfg: seededCfg,
    channelKey: channel,
    accountId,
    patch,
    ensureChannelEnabled: ensureEnabled,
    ensureAccountEnabled: ensureEnabled,
  });
}

export function setAccountAllowFromForChannel(params: {
  cfg: DenebConfig;
  channel: "imessage" | "signal";
  accountId: string;
  allowFrom: string[];
}): DenebConfig {
  const { cfg, channel, accountId, allowFrom } = params;
  return patchConfigForScopedAccount({
    cfg,
    channel,
    accountId,
    patch: { allowFrom },
    ensureEnabled: false,
  });
}

export function patchChannelConfigForAccount(params: {
  cfg: DenebConfig;
  channel: AccountScopedChannel;
  accountId: string;
  patch: Record<string, unknown>;
}): DenebConfig {
  return patchConfigForScopedAccount({
    ...params,
    ensureEnabled: true,
  });
}

export async function resolveGroupAllowlistWithLookupNotes<TResolved>(params: {
  label: string;
  prompter: Pick<WizardPrompter, "note">;
  entries: string[];
  fallback: TResolved;
  resolve: () => Promise<TResolved>;
}): Promise<TResolved> {
  try {
    return await params.resolve();
  } catch (error) {
    await noteChannelLookupFailure({
      prompter: params.prompter,
      label: params.label,
      error,
    });
    await noteChannelLookupSummary({
      prompter: params.prompter,
      label: params.label,
      resolvedSections: [],
      unresolved: params.entries,
    });
    return params.fallback;
  }
}

export function createAllowFromSection(params: {
  helpTitle?: string;
  helpLines?: string[];
  credentialInputKey?: NonNullable<ChannelSetupWizard["allowFrom"]>["credentialInputKey"];
  message: string;
  placeholder: string;
  invalidWithoutCredentialNote: string;
  parseInputs?: NonNullable<NonNullable<ChannelSetupWizard["allowFrom"]>["parseInputs"]>;
  parseId: NonNullable<NonNullable<ChannelSetupWizard["allowFrom"]>["parseId"]>;
  resolveEntries?: NonNullable<NonNullable<ChannelSetupWizard["allowFrom"]>["resolveEntries"]>;
  apply: NonNullable<NonNullable<ChannelSetupWizard["allowFrom"]>["apply"]>;
}): NonNullable<ChannelSetupWizard["allowFrom"]> {
  return {
    ...(params.helpTitle ? { helpTitle: params.helpTitle } : {}),
    ...(params.helpLines ? { helpLines: params.helpLines } : {}),
    ...(params.credentialInputKey ? { credentialInputKey: params.credentialInputKey } : {}),
    message: params.message,
    placeholder: params.placeholder,
    invalidWithoutCredentialNote: params.invalidWithoutCredentialNote,
    ...(params.parseInputs ? { parseInputs: params.parseInputs } : {}),
    parseId: params.parseId,
    resolveEntries:
      params.resolveEntries ??
      (async ({ entries }) => resolveParsedAllowFromEntries({ entries, parseId: params.parseId })),
    apply: params.apply,
  };
}

export async function noteChannelLookupSummary(params: {
  prompter: Pick<WizardPrompter, "note">;
  label: string;
  resolvedSections: Array<{ title: string; values: string[] }>;
  unresolved?: string[];
}): Promise<void> {
  const lines: string[] = [];
  for (const section of params.resolvedSections) {
    if (section.values.length === 0) {
      continue;
    }
    lines.push(`${section.title}: ${section.values.join(", ")}`);
  }
  if (params.unresolved && params.unresolved.length > 0) {
    lines.push(`Unresolved (kept as typed): ${params.unresolved.join(", ")}`);
  }
  if (lines.length > 0) {
    await params.prompter.note(lines.join("\n"), params.label);
  }
}

export async function noteChannelLookupFailure(params: {
  prompter: Pick<WizardPrompter, "note">;
  label: string;
  error: unknown;
}): Promise<void> {
  await params.prompter.note(
    `Channel lookup failed; keeping entries as typed. ${String(params.error)}`,
    params.label,
  );
}
