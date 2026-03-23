import type { DenebConfig } from "../../config/config.js";
import type { SecretInput } from "../../config/types.secrets.js";
import {
  promptSecretRefForSetup,
  resolveSecretInputModeForEnvSelection,
} from "../../plugins/provider-auth-input.js";
import type { WizardPrompter } from "../../wizard/prompts.js";
import {
  patchChannelConfigForAccount,
  resolveSetupAccountId,
  setAccountAllowFromForChannel,
  setLegacyChannelAllowFrom,
} from "./setup-wizard-config.js";
import { mergeAllowFromEntries, splitSetupEntries } from "./setup-wizard-parse.js";

type LegacyDmChannel = "discord" | "slack";

type ParsedAllowFromResult = { entries: string[]; error?: string };

export type SingleChannelSecretInputPromptResult =
  | { action: "keep" }
  | { action: "use-env" }
  | { action: "set"; value: SecretInput; resolvedValue: string };

export function buildSingleChannelSecretPromptState(params: {
  accountConfigured: boolean;
  hasConfigToken: boolean;
  allowEnv: boolean;
  envValue?: string;
}): {
  accountConfigured: boolean;
  hasConfigToken: boolean;
  canUseEnv: boolean;
} {
  return {
    accountConfigured: params.accountConfigured,
    hasConfigToken: params.hasConfigToken,
    canUseEnv: params.allowEnv && Boolean(params.envValue?.trim()) && !params.hasConfigToken,
  };
}

export async function promptSingleChannelToken(params: {
  prompter: Pick<WizardPrompter, "confirm" | "text">;
  accountConfigured: boolean;
  canUseEnv: boolean;
  hasConfigToken: boolean;
  envPrompt: string;
  keepPrompt: string;
  inputPrompt: string;
}): Promise<{ useEnv: boolean; token: string | null }> {
  const promptToken = async (): Promise<string> =>
    String(
      await params.prompter.text({
        message: params.inputPrompt,
        validate: (value) => (value?.trim() ? undefined : "Required"),
      }),
    ).trim();

  if (params.canUseEnv) {
    const keepEnv = await params.prompter.confirm({
      message: params.envPrompt,
      initialValue: true,
    });
    if (keepEnv) {
      return { useEnv: true, token: null };
    }
    return { useEnv: false, token: await promptToken() };
  }

  if (params.hasConfigToken && params.accountConfigured) {
    const keep = await params.prompter.confirm({
      message: params.keepPrompt,
      initialValue: true,
    });
    if (keep) {
      return { useEnv: false, token: null };
    }
  }

  return { useEnv: false, token: await promptToken() };
}

export async function promptSingleChannelSecretInput(params: {
  cfg: DenebConfig;
  prompter: Pick<WizardPrompter, "confirm" | "text" | "select" | "note">;
  providerHint: string;
  credentialLabel: string;
  secretInputMode?: "plaintext" | "ref";
  accountConfigured: boolean;
  canUseEnv: boolean;
  hasConfigToken: boolean;
  envPrompt: string;
  keepPrompt: string;
  inputPrompt: string;
  preferredEnvVar?: string;
}): Promise<SingleChannelSecretInputPromptResult> {
  const selectedMode = await resolveSecretInputModeForEnvSelection({
    prompter: params.prompter as WizardPrompter,
    explicitMode: params.secretInputMode,
    copy: {
      modeMessage: `How do you want to provide this ${params.credentialLabel}?`,
      plaintextLabel: `Enter ${params.credentialLabel}`,
      plaintextHint: "Stores the credential directly in Deneb config",
      refLabel: "Use external secret provider",
      refHint: "Stores a reference to env or configured external secret providers",
    },
  });

  if (selectedMode === "plaintext") {
    const plainResult = await promptSingleChannelToken({
      prompter: params.prompter,
      accountConfigured: params.accountConfigured,
      canUseEnv: params.canUseEnv,
      hasConfigToken: params.hasConfigToken,
      envPrompt: params.envPrompt,
      keepPrompt: params.keepPrompt,
      inputPrompt: params.inputPrompt,
    });
    if (plainResult.useEnv) {
      return { action: "use-env" };
    }
    if (plainResult.token) {
      return { action: "set", value: plainResult.token, resolvedValue: plainResult.token };
    }
    return { action: "keep" };
  }

  if (params.hasConfigToken && params.accountConfigured) {
    const keep = await params.prompter.confirm({
      message: params.keepPrompt,
      initialValue: true,
    });
    if (keep) {
      return { action: "keep" };
    }
  }

  const resolved = await promptSecretRefForSetup({
    provider: params.providerHint,
    config: params.cfg,
    prompter: params.prompter as WizardPrompter,
    preferredEnvVar: params.preferredEnvVar,
    copy: {
      sourceMessage: `Where is this ${params.credentialLabel} stored?`,
      envVarPlaceholder: params.preferredEnvVar ?? "DENEB_SECRET",
      envVarFormatError:
        'Use an env var name like "DENEB_SECRET" (uppercase letters, numbers, underscores).',
      noProvidersMessage:
        "No file/exec secret providers are configured yet. Add one under secrets.providers, or select Environment variable.",
    },
  });
  return {
    action: "set",
    value: resolved.ref,
    resolvedValue: resolved.resolvedValue,
  };
}

export async function runSingleChannelSecretStep(params: {
  cfg: DenebConfig;
  prompter: Pick<WizardPrompter, "confirm" | "text" | "select" | "note">;
  providerHint: string;
  credentialLabel: string;
  secretInputMode?: "plaintext" | "ref";
  accountConfigured: boolean;
  hasConfigToken: boolean;
  allowEnv: boolean;
  envValue?: string;
  envPrompt: string;
  keepPrompt: string;
  inputPrompt: string;
  preferredEnvVar?: string;
  onMissingConfigured?: () => Promise<void>;
  applyUseEnv?: (cfg: DenebConfig) => DenebConfig | Promise<DenebConfig>;
  applySet?: (
    cfg: DenebConfig,
    value: SecretInput,
    resolvedValue: string,
  ) => DenebConfig | Promise<DenebConfig>;
}): Promise<{
  cfg: DenebConfig;
  action: SingleChannelSecretInputPromptResult["action"];
  resolvedValue?: string;
}> {
  const promptState = buildSingleChannelSecretPromptState({
    accountConfigured: params.accountConfigured,
    hasConfigToken: params.hasConfigToken,
    allowEnv: params.allowEnv,
    envValue: params.envValue,
  });

  if (!promptState.accountConfigured && params.onMissingConfigured) {
    await params.onMissingConfigured();
  }

  const result = await promptSingleChannelSecretInput({
    cfg: params.cfg,
    prompter: params.prompter,
    providerHint: params.providerHint,
    credentialLabel: params.credentialLabel,
    secretInputMode: params.secretInputMode,
    accountConfigured: promptState.accountConfigured,
    canUseEnv: promptState.canUseEnv,
    hasConfigToken: promptState.hasConfigToken,
    envPrompt: params.envPrompt,
    keepPrompt: params.keepPrompt,
    inputPrompt: params.inputPrompt,
    preferredEnvVar: params.preferredEnvVar,
  });

  if (result.action === "use-env") {
    return {
      cfg: params.applyUseEnv ? await params.applyUseEnv(params.cfg) : params.cfg,
      action: result.action,
      resolvedValue: params.envValue?.trim() || undefined,
    };
  }

  if (result.action === "set") {
    return {
      cfg: params.applySet
        ? await params.applySet(params.cfg, result.value, result.resolvedValue)
        : params.cfg,
      action: result.action,
      resolvedValue: result.resolvedValue,
    };
  }

  return {
    cfg: params.cfg,
    action: result.action,
  };
}

export function applySingleTokenPromptResult(params: {
  cfg: DenebConfig;
  channel: "discord" | "telegram";
  accountId: string;
  tokenPatchKey: "token" | "botToken";
  tokenResult: {
    useEnv: boolean;
    token: SecretInput | null;
  };
}): DenebConfig {
  let next = params.cfg;
  if (params.tokenResult.useEnv) {
    next = patchChannelConfigForAccount({
      cfg: next,
      channel: params.channel,
      accountId: params.accountId,
      patch: {},
    });
  }
  if (params.tokenResult.token) {
    next = patchChannelConfigForAccount({
      cfg: next,
      channel: params.channel,
      accountId: params.accountId,
      patch: { [params.tokenPatchKey]: params.tokenResult.token },
    });
  }
  return next;
}

export async function promptParsedAllowFromForAccount<TConfig extends DenebConfig>(params: {
  cfg: TConfig;
  accountId?: string;
  defaultAccountId: string;
  prompter: Pick<WizardPrompter, "note" | "text">;
  noteTitle: string;
  noteLines: string[];
  message: string;
  placeholder: string;
  parseEntries: (raw: string) => ParsedAllowFromResult;
  getExistingAllowFrom: (params: { cfg: TConfig; accountId: string }) => Array<string | number>;
  mergeEntries?: (params: { existing: Array<string | number>; parsed: string[] }) => string[];
  applyAllowFrom: (params: {
    cfg: TConfig;
    accountId: string;
    allowFrom: string[];
  }) => TConfig | Promise<TConfig>;
}): Promise<TConfig> {
  const accountId = resolveSetupAccountId({
    accountId: params.accountId,
    defaultAccountId: params.defaultAccountId,
  });
  const existing = params.getExistingAllowFrom({
    cfg: params.cfg,
    accountId,
  });
  await params.prompter.note(params.noteLines.join("\n"), params.noteTitle);
  const entry = await params.prompter.text({
    message: params.message,
    placeholder: params.placeholder,
    initialValue: existing[0] ? String(existing[0]) : undefined,
    validate: (value) => {
      const raw = String(value ?? "").trim();
      if (!raw) {
        return "Required";
      }
      return params.parseEntries(raw).error;
    },
  });
  const parsed = params.parseEntries(String(entry));
  const unique =
    params.mergeEntries?.({
      existing,
      parsed: parsed.entries,
    }) ?? mergeAllowFromEntries(undefined, parsed.entries);
  return await params.applyAllowFrom({
    cfg: params.cfg,
    accountId,
    allowFrom: unique,
  });
}

export async function promptParsedAllowFromForScopedChannel(params: {
  cfg: DenebConfig;
  channel: "imessage" | "signal";
  accountId?: string;
  defaultAccountId: string;
  prompter: Pick<WizardPrompter, "note" | "text">;
  noteTitle: string;
  noteLines: string[];
  message: string;
  placeholder: string;
  parseEntries: (raw: string) => ParsedAllowFromResult;
  getExistingAllowFrom: (params: { cfg: DenebConfig; accountId: string }) => Array<string | number>;
}): Promise<DenebConfig> {
  return await promptParsedAllowFromForAccount({
    cfg: params.cfg,
    accountId: params.accountId,
    defaultAccountId: params.defaultAccountId,
    prompter: params.prompter,
    noteTitle: params.noteTitle,
    noteLines: params.noteLines,
    message: params.message,
    placeholder: params.placeholder,
    parseEntries: params.parseEntries,
    getExistingAllowFrom: params.getExistingAllowFrom,
    applyAllowFrom: ({ cfg, accountId, allowFrom }) =>
      setAccountAllowFromForChannel({
        cfg,
        channel: params.channel,
        accountId,
        allowFrom,
      }),
  });
}

type AllowFromResolution = {
  input: string;
  resolved: boolean;
  id?: string | null;
};

export async function resolveEntriesWithOptionalToken<TResult>(params: {
  token?: string | null;
  entries: string[];
  buildWithoutToken: (input: string) => TResult;
  resolveEntries: (params: { token: string; entries: string[] }) => Promise<TResult[]>;
}): Promise<TResult[]> {
  const token = params.token?.trim();
  if (!token) {
    return params.entries.map(params.buildWithoutToken);
  }
  return await params.resolveEntries({
    token,
    entries: params.entries,
  });
}

export async function promptResolvedAllowFrom(params: {
  prompter: WizardPrompter;
  existing: Array<string | number>;
  token?: string | null;
  message: string;
  placeholder: string;
  label: string;
  parseInputs: (value: string) => string[];
  parseId: (value: string) => string | null;
  invalidWithoutTokenNote: string;
  resolveEntries: (params: { token: string; entries: string[] }) => Promise<AllowFromResolution[]>;
}): Promise<string[]> {
  while (true) {
    const entry = await params.prompter.text({
      message: params.message,
      placeholder: params.placeholder,
      initialValue: params.existing[0] ? String(params.existing[0]) : undefined,
      validate: (value) => (String(value ?? "").trim() ? undefined : "Required"),
    });
    const parts = params.parseInputs(String(entry));
    if (!params.token) {
      const ids = parts.map(params.parseId).filter(Boolean) as string[];
      if (ids.length !== parts.length) {
        await params.prompter.note(params.invalidWithoutTokenNote, params.label);
        continue;
      }
      return mergeAllowFromEntries(params.existing, ids);
    }

    const results = await params
      .resolveEntries({
        token: params.token,
        entries: parts,
      })
      .catch(() => null);
    if (!results) {
      await params.prompter.note("Failed to resolve usernames. Try again.", params.label);
      continue;
    }
    const unresolved = results.filter((res) => !res.resolved || !res.id);
    if (unresolved.length > 0) {
      await params.prompter.note(
        `Could not resolve: ${unresolved.map((res) => res.input).join(", ")}`,
        params.label,
      );
      continue;
    }
    const ids = results.map((res) => res.id as string);
    return mergeAllowFromEntries(params.existing, ids);
  }
}

export async function promptLegacyChannelAllowFrom(params: {
  cfg: DenebConfig;
  channel: LegacyDmChannel;
  prompter: WizardPrompter;
  existing: Array<string | number>;
  token?: string | null;
  noteTitle: string;
  noteLines: string[];
  message: string;
  placeholder: string;
  parseId: (value: string) => string | null;
  invalidWithoutTokenNote: string;
  resolveEntries: (params: { token: string; entries: string[] }) => Promise<AllowFromResolution[]>;
}): Promise<DenebConfig> {
  await params.prompter.note(params.noteLines.join("\n"), params.noteTitle);
  const unique = await promptResolvedAllowFrom({
    prompter: params.prompter,
    existing: params.existing,
    token: params.token,
    message: params.message,
    placeholder: params.placeholder,
    label: params.noteTitle,
    parseInputs: splitSetupEntries,
    parseId: params.parseId,
    invalidWithoutTokenNote: params.invalidWithoutTokenNote,
    resolveEntries: params.resolveEntries,
  });
  return setLegacyChannelAllowFrom({
    cfg: params.cfg,
    channel: params.channel,
    allowFrom: unique,
  });
}

export async function promptLegacyChannelAllowFromForAccount<TAccount>(params: {
  cfg: DenebConfig;
  channel: LegacyDmChannel;
  prompter: WizardPrompter;
  accountId?: string;
  defaultAccountId: string;
  resolveAccount: (cfg: DenebConfig, accountId: string) => TAccount;
  resolveExisting: (account: TAccount, cfg: DenebConfig) => Array<string | number>;
  resolveToken: (account: TAccount) => string | null | undefined;
  noteTitle: string;
  noteLines: string[];
  message: string;
  placeholder: string;
  parseId: (value: string) => string | null;
  invalidWithoutTokenNote: string;
  resolveEntries: (params: { token: string; entries: string[] }) => Promise<AllowFromResolution[]>;
}): Promise<DenebConfig> {
  const accountId = resolveSetupAccountId({
    accountId: params.accountId,
    defaultAccountId: params.defaultAccountId,
  });
  const account = params.resolveAccount(params.cfg, accountId);
  return await promptLegacyChannelAllowFrom({
    cfg: params.cfg,
    channel: params.channel,
    prompter: params.prompter,
    existing: params.resolveExisting(account, params.cfg),
    token: params.resolveToken(account),
    noteTitle: params.noteTitle,
    noteLines: params.noteLines,
    message: params.message,
    placeholder: params.placeholder,
    parseId: params.parseId,
    invalidWithoutTokenNote: params.invalidWithoutTokenNote,
    resolveEntries: params.resolveEntries,
  });
}
