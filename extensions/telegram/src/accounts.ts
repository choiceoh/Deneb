import util from "node:util";
import {
  createAccountActionGate,
  DEFAULT_ACCOUNT_ID,
  listConfiguredAccountIds as listConfiguredAccountIdsFromSection,
  normalizeAccountId,
  normalizeOptionalAccountId,
  resolveAccountEntry,
  resolveAccountWithDefaultFallback,
  type DenebConfig,
} from "deneb/plugin-sdk/account-resolution";
import { isTruthyEnvValue } from "deneb/plugin-sdk/infra-runtime";
import { listBoundAccountIds, resolveDefaultAgentBoundAccountId } from "deneb/plugin-sdk/routing";
import { formatSetExplicitDefaultInstruction } from "deneb/plugin-sdk/routing";
import { createSubsystemLogger } from "deneb/plugin-sdk/runtime-env";
import type { TelegramAccountConfig, TelegramActionConfig } from "../runtime-api.js";
import { resolveTelegramToken } from "./token.js";

let log: ReturnType<typeof createSubsystemLogger> | null = null;

function getLog() {
  if (!log) {
    log = createSubsystemLogger("telegram/accounts");
  }
  return log;
}

function formatDebugArg(value: unknown): string {
  if (typeof value === "string") {
    return value;
  }
  if (value instanceof Error) {
    return value.stack ?? value.message;
  }
  return util.inspect(value, { colors: false, depth: null, compact: true, breakLength: Infinity });
}

const debugAccounts = (...args: unknown[]) => {
  if (isTruthyEnvValue(process.env.DENEB_DEBUG_TELEGRAM_ACCOUNTS)) {
    const parts = args.map((arg) => formatDebugArg(arg));
    getLog().warn(parts.join(" ").trim());
  }
};

export type ResolvedTelegramAccount = {
  accountId: string;
  enabled: boolean;
  name?: string;
  token: string;
  tokenSource: "env" | "tokenFile" | "config" | "none";
  config: TelegramAccountConfig;
};

function listConfiguredAccountIds(cfg: DenebConfig): string[] {
  return listConfiguredAccountIdsFromSection({
    accounts: cfg.channels?.telegram?.accounts,
    normalizeAccountId,
  });
}

/** Singleton result for the common single-account (default) setup. */
const SINGLE_DEFAULT_ACCOUNT_IDS: readonly string[] = [DEFAULT_ACCOUNT_ID];

export function listTelegramAccountIds(cfg: DenebConfig): string[] {
  const configuredIds = listConfiguredAccountIds(cfg);
  const boundIds = listBoundAccountIds(cfg, "telegram");

  // Single-account fast path: no configured accounts and no bound accounts
  // → return the cached singleton array without Set/sort overhead.
  if (configuredIds.length === 0 && boundIds.length === 0) {
    debugAccounts("listTelegramAccountIds", SINGLE_DEFAULT_ACCOUNT_IDS);
    return SINGLE_DEFAULT_ACCOUNT_IDS as string[];
  }

  // Single-account fast path: exactly one configured account, no bound accounts.
  if (configuredIds.length === 1 && boundIds.length === 0) {
    debugAccounts("listTelegramAccountIds", configuredIds);
    return configuredIds;
  }

  const ids = Array.from(new Set([...configuredIds, ...boundIds]));
  debugAccounts("listTelegramAccountIds", ids);
  if (ids.length === 0) {
    return SINGLE_DEFAULT_ACCOUNT_IDS as string[];
  }
  return ids.length === 1 ? ids : ids.toSorted((a, b) => a.localeCompare(b));
}

let emittedMissingDefaultWarn = false;

/** @internal Reset the once-per-process warning flag. Exported for tests only. */
export function resetMissingDefaultWarnFlag(): void {
  emittedMissingDefaultWarn = false;
}

export function resolveDefaultTelegramAccountId(cfg: DenebConfig): string {
  // Single-account fast path: no accounts section and no bound default
  // → skip all resolution and return the default immediately.
  const accountsSection = cfg.channels?.telegram?.accounts;
  if (!accountsSection || Object.keys(accountsSection).length <= 1) {
    const boundDefault = resolveDefaultAgentBoundAccountId(cfg, "telegram");
    if (boundDefault) {
      return boundDefault;
    }
    // Single or zero accounts: DEFAULT_ACCOUNT_ID is always correct.
    if (!accountsSection || Object.keys(accountsSection).length === 0) {
      return DEFAULT_ACCOUNT_ID;
    }
    // Exactly one account configured: return it directly.
    const soleId = Object.keys(accountsSection)[0];
    return normalizeAccountId(soleId);
  }

  // Multi-account path
  const boundDefault = resolveDefaultAgentBoundAccountId(cfg, "telegram");
  if (boundDefault) {
    return boundDefault;
  }
  const preferred = normalizeOptionalAccountId(cfg.channels?.telegram?.defaultAccount);
  if (
    preferred &&
    listTelegramAccountIds(cfg).some((accountId) => normalizeAccountId(accountId) === preferred)
  ) {
    return preferred;
  }
  const ids = listTelegramAccountIds(cfg);
  if (ids.includes(DEFAULT_ACCOUNT_ID)) {
    return DEFAULT_ACCOUNT_ID;
  }
  if (ids.length > 1 && !emittedMissingDefaultWarn) {
    emittedMissingDefaultWarn = true;
    getLog().warn(
      `channels.telegram: accounts.default is missing; falling back to "${ids[0]}". ` +
        `${formatSetExplicitDefaultInstruction("telegram")} to avoid routing surprises in multi-account setups.`,
    );
  }
  return ids[0] ?? DEFAULT_ACCOUNT_ID;
}

export function resolveTelegramAccountConfig(
  cfg: DenebConfig,
  accountId: string,
): TelegramAccountConfig | undefined {
  const normalized = normalizeAccountId(accountId);
  return resolveAccountEntry(cfg.channels?.telegram?.accounts, normalized);
}

export function mergeTelegramAccountConfig(
  cfg: DenebConfig,
  accountId: string,
): TelegramAccountConfig {
  const telegramCfg = cfg.channels?.telegram;
  if (!telegramCfg) {
    return {} as TelegramAccountConfig;
  }

  const {
    accounts: _ignored,
    defaultAccount: _ignoredDefaultAccount,
    groups: channelGroups,
    ...base
  } = telegramCfg as TelegramAccountConfig & {
    accounts?: unknown;
    defaultAccount?: unknown;
  };

  const account = resolveTelegramAccountConfig(cfg, accountId) ?? {};

  // Single-account fast path: when no explicit accounts section exists, skip
  // the multi-account group-inheritance check entirely — channel-level groups
  // always apply.
  const accountsSection = telegramCfg.accounts;
  if (!accountsSection || Object.keys(accountsSection).length <= 1) {
    return { ...base, ...account, groups: account.groups ?? channelGroups };
  }

  // Multi-account: channel-level `groups` must NOT be inherited by accounts
  // that don't have their own `groups` config. A bot that is not a member of
  // a configured group will fail, disrupting delivery for *all* accounts.
  // See: https://github.com/deneb/deneb/issues/30673
  const groups = account.groups ?? undefined;

  return { ...base, ...account, groups };
}

export function createTelegramActionGate(params: {
  cfg: DenebConfig;
  accountId?: string | null;
}): (key: keyof TelegramActionConfig, defaultValue?: boolean) => boolean {
  const accountId = normalizeAccountId(params.accountId);
  return createAccountActionGate({
    baseActions: params.cfg.channels?.telegram?.actions,
    accountActions: resolveTelegramAccountConfig(params.cfg, accountId)?.actions,
  });
}

export type TelegramPollActionGateState = {
  sendMessageEnabled: boolean;
  pollEnabled: boolean;
  enabled: boolean;
};

export function resolveTelegramPollActionGateState(
  isActionEnabled: (key: keyof TelegramActionConfig, defaultValue?: boolean) => boolean,
): TelegramPollActionGateState {
  const sendMessageEnabled = isActionEnabled("sendMessage");
  const pollEnabled = isActionEnabled("poll");
  return {
    sendMessageEnabled,
    pollEnabled,
    enabled: sendMessageEnabled && pollEnabled,
  };
}

// Per-config-snapshot cache for single-account resolution.
// Avoids re-resolving account config, token, and merging on every message send
// when the config has not changed (the common hot path).
let resolvedAccountCache: {
  cfgRef: WeakRef<DenebConfig>;
  accountId: string;
  result: ResolvedTelegramAccount;
} | null = null;

function resolveAccountUncached(cfg: DenebConfig, accountId: string): ResolvedTelegramAccount {
  const baseEnabled = cfg.channels?.telegram?.enabled !== false;
  const merged = mergeTelegramAccountConfig(cfg, accountId);
  const accountEnabled = merged.enabled !== false;
  const enabled = baseEnabled && accountEnabled;
  const tokenResolution = resolveTelegramToken(cfg, { accountId });
  debugAccounts("resolve", {
    accountId,
    enabled,
    tokenSource: tokenResolution.source,
  });
  return {
    accountId,
    enabled,
    name: merged.name?.trim() || undefined,
    token: tokenResolution.token,
    tokenSource: tokenResolution.source,
    config: merged,
  };
}

export function resolveTelegramAccount(params: {
  cfg: DenebConfig;
  accountId?: string | null;
}): ResolvedTelegramAccount {
  // If accountId is omitted, prefer a configured account token over failing on
  // the implicit "default" account. This keeps env-based setups working while
  // making config-only tokens work for things like heartbeats.
  return resolveAccountWithDefaultFallback({
    accountId: params.accountId,
    normalizeAccountId,
    resolvePrimary: (accountId) => {
      // Hot-path cache: when the same config object + accountId is resolved
      // repeatedly (e.g., per-message sends with a single account), return
      // the cached result without re-merging config or re-resolving tokens.
      const cached = resolvedAccountCache;
      if (cached && cached.accountId === accountId && cached.cfgRef.deref() === params.cfg) {
        return cached.result;
      }
      const result = resolveAccountUncached(params.cfg, accountId);
      resolvedAccountCache = {
        cfgRef: new WeakRef(params.cfg),
        accountId,
        result,
      };
      return result;
    },
    hasCredential: (account) => account.tokenSource !== "none",
    resolveDefaultAccountId: () => resolveDefaultTelegramAccountId(params.cfg),
  });
}

/** Invalidate the per-config account cache (e.g., after config reload). */
export function invalidateTelegramAccountCache(): void {
  resolvedAccountCache = null;
}

export function listEnabledTelegramAccounts(cfg: DenebConfig): ResolvedTelegramAccount[] {
  return listTelegramAccountIds(cfg)
    .map((accountId) => resolveTelegramAccount({ cfg, accountId }))
    .filter((account) => account.enabled);
}
