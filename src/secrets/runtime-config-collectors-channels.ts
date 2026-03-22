import type { DenebConfig } from "../config/config.js";
import { coerceSecretRef } from "../config/types.secrets.js";
import {
  collectSecretInputAssignment,
  hasOwnProperty,
  isChannelAccountEffectivelyEnabled,
  isEnabledFlag,
  type ResolverContext,
  type SecretDefaults,
} from "./runtime-shared.js";
import { isRecord } from "./shared.js";

type ChannelAccountEntry = {
  accountId: string;
  account: Record<string, unknown>;
  enabled: boolean;
};

type ChannelAccountSurface = {
  hasExplicitAccounts: boolean;
  channelEnabled: boolean;
  accounts: ChannelAccountEntry[];
};

function resolveChannelAccountSurface(channel: Record<string, unknown>): ChannelAccountSurface {
  const channelEnabled = isEnabledFlag(channel);
  const accounts = channel.accounts;
  if (!isRecord(accounts) || Object.keys(accounts).length === 0) {
    return {
      hasExplicitAccounts: false,
      channelEnabled,
      accounts: [{ accountId: "default", account: channel, enabled: channelEnabled }],
    };
  }
  const accountEntries: ChannelAccountEntry[] = [];
  for (const [accountId, account] of Object.entries(accounts)) {
    if (!isRecord(account)) {
      continue;
    }
    accountEntries.push({
      accountId,
      account,
      enabled: isChannelAccountEffectivelyEnabled(channel, account),
    });
  }
  return {
    hasExplicitAccounts: true,
    channelEnabled,
    accounts: accountEntries,
  };
}

function normalizeSecretStringValue(value: unknown): string {
  return typeof value === "string" ? value.trim() : "";
}

function hasConfiguredSecretInputValue(
  value: unknown,
  defaults: SecretDefaults | undefined,
): boolean {
  return normalizeSecretStringValue(value).length > 0 || coerceSecretRef(value, defaults) !== null;
}

function collectTelegramAssignments(params: {
  config: DenebConfig;
  defaults: SecretDefaults | undefined;
  context: ResolverContext;
}): void {
  const channels = params.config.channels as Record<string, unknown> | undefined;
  if (!isRecord(channels)) {
    return;
  }
  const telegram = channels.telegram;
  if (!isRecord(telegram)) {
    return;
  }
  const surface = resolveChannelAccountSurface(telegram);
  const baseTokenFile = typeof telegram.tokenFile === "string" ? telegram.tokenFile.trim() : "";
  const topLevelBotTokenActive = !surface.channelEnabled
    ? false
    : !surface.hasExplicitAccounts
      ? baseTokenFile.length === 0
      : surface.accounts.some(({ account, enabled }) => {
          if (!enabled || baseTokenFile.length > 0) {
            return false;
          }
          const accountBotTokenConfigured = hasConfiguredSecretInputValue(
            account.botToken,
            params.defaults,
          );
          const accountTokenFileConfigured =
            typeof account.tokenFile === "string" && account.tokenFile.trim().length > 0;
          return !accountBotTokenConfigured && !accountTokenFileConfigured;
        });
  collectSecretInputAssignment({
    value: telegram.botToken,
    path: "channels.telegram.botToken",
    expected: "string",
    defaults: params.defaults,
    context: params.context,
    active: topLevelBotTokenActive,
    inactiveReason:
      "no enabled Telegram surface inherits this top-level botToken (tokenFile is configured).",
    apply: (value) => {
      telegram.botToken = value;
    },
  });
  if (surface.hasExplicitAccounts) {
    for (const { accountId, account, enabled } of surface.accounts) {
      if (!hasOwnProperty(account, "botToken")) {
        continue;
      }
      const accountTokenFile =
        typeof account.tokenFile === "string" ? account.tokenFile.trim() : "";
      collectSecretInputAssignment({
        value: account.botToken,
        path: `channels.telegram.accounts.${accountId}.botToken`,
        expected: "string",
        defaults: params.defaults,
        context: params.context,
        active: enabled && accountTokenFile.length === 0,
        inactiveReason: "Telegram account is disabled or tokenFile is configured.",
        apply: (value) => {
          account.botToken = value;
        },
      });
    }
  }
  const baseWebhookUrl = typeof telegram.webhookUrl === "string" ? telegram.webhookUrl.trim() : "";
  const topLevelWebhookSecretActive = !surface.channelEnabled
    ? false
    : !surface.hasExplicitAccounts
      ? baseWebhookUrl.length > 0
      : surface.accounts.some(
          ({ account, enabled }) =>
            enabled &&
            !hasOwnProperty(account, "webhookSecret") &&
            (hasOwnProperty(account, "webhookUrl")
              ? typeof account.webhookUrl === "string" && account.webhookUrl.trim().length > 0
              : baseWebhookUrl.length > 0),
        );
  collectSecretInputAssignment({
    value: telegram.webhookSecret,
    path: "channels.telegram.webhookSecret",
    expected: "string",
    defaults: params.defaults,
    context: params.context,
    active: topLevelWebhookSecretActive,
    inactiveReason:
      "no enabled Telegram webhook surface inherits this top-level webhookSecret (webhook mode is not active).",
    apply: (value) => {
      telegram.webhookSecret = value;
    },
  });
  if (!surface.hasExplicitAccounts) {
    return;
  }
  for (const { accountId, account, enabled } of surface.accounts) {
    if (!hasOwnProperty(account, "webhookSecret")) {
      continue;
    }
    const accountWebhookUrl = hasOwnProperty(account, "webhookUrl")
      ? typeof account.webhookUrl === "string"
        ? account.webhookUrl.trim()
        : ""
      : baseWebhookUrl;
    collectSecretInputAssignment({
      value: account.webhookSecret,
      path: `channels.telegram.accounts.${accountId}.webhookSecret`,
      expected: "string",
      defaults: params.defaults,
      context: params.context,
      active: enabled && accountWebhookUrl.length > 0,
      inactiveReason:
        "Telegram account is disabled or webhook mode is not active for this account.",
      apply: (value) => {
        account.webhookSecret = value;
      },
    });
  }
}

export function collectChannelConfigAssignments(params: {
  config: DenebConfig;
  defaults: SecretDefaults | undefined;
  context: ResolverContext;
}): void {
  collectTelegramAssignments(params);
}
