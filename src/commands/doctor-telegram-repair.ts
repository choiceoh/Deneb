import {
  fetchTelegramChatId,
  inspectTelegramAccount,
  isNumericTelegramUserId,
  listTelegramAccountIds,
  normalizeTelegramAllowFromEntry,
} from "deneb/plugin-sdk/telegram";
import { resolveCommandSecretRefsViaGateway } from "../cli/command-secret-gateway.js";
import { getChannelsCommandSecretTargetIds } from "../cli/command-secret-targets.js";
import type { DenebConfig } from "../config/config.js";
import { resolveTelegramAccount } from "../plugin-sdk/account-resolution.js";
import { describeUnknownError } from "../secrets/shared.js";

type TelegramAllowFromUsernameHit = { path: string; entry: string };

type TelegramAllowFromListRef = {
  pathLabel: string;
  holder: Record<string, unknown>;
  key: "allowFrom" | "groupAllowFrom";
};

export function asObjectRecord(value: unknown): Record<string, unknown> | null {
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    return null;
  }
  return value as Record<string, unknown>;
}

export function collectTelegramAccountScopes(
  cfg: DenebConfig,
): Array<{ prefix: string; account: Record<string, unknown> }> {
  const scopes: Array<{ prefix: string; account: Record<string, unknown> }> = [];
  const telegram = asObjectRecord(cfg.channels?.telegram);
  if (!telegram) {
    return scopes;
  }

  scopes.push({ prefix: "channels.telegram", account: telegram });
  const accounts = asObjectRecord(telegram.accounts);
  if (!accounts) {
    return scopes;
  }
  for (const key of Object.keys(accounts)) {
    const account = asObjectRecord(accounts[key]);
    if (!account) {
      continue;
    }
    scopes.push({ prefix: `channels.telegram.accounts.${key}`, account });
  }

  return scopes;
}

export function collectTelegramAllowFromLists(
  prefix: string,
  account: Record<string, unknown>,
): TelegramAllowFromListRef[] {
  const refs: TelegramAllowFromListRef[] = [
    { pathLabel: `${prefix}.allowFrom`, holder: account, key: "allowFrom" },
    { pathLabel: `${prefix}.groupAllowFrom`, holder: account, key: "groupAllowFrom" },
  ];
  const groups = asObjectRecord(account.groups);
  if (!groups) {
    return refs;
  }

  for (const groupId of Object.keys(groups)) {
    const group = asObjectRecord(groups[groupId]);
    if (!group) {
      continue;
    }
    refs.push({
      pathLabel: `${prefix}.groups.${groupId}.allowFrom`,
      holder: group,
      key: "allowFrom",
    });
    const topics = asObjectRecord(group.topics);
    if (!topics) {
      continue;
    }
    for (const topicId of Object.keys(topics)) {
      const topic = asObjectRecord(topics[topicId]);
      if (!topic) {
        continue;
      }
      refs.push({
        pathLabel: `${prefix}.groups.${groupId}.topics.${topicId}.allowFrom`,
        holder: topic,
        key: "allowFrom",
      });
    }
  }
  return refs;
}

export function scanTelegramAllowFromUsernameEntries(
  cfg: DenebConfig,
): TelegramAllowFromUsernameHit[] {
  const hits: TelegramAllowFromUsernameHit[] = [];

  const scanList = (pathLabel: string, list: unknown) => {
    if (!Array.isArray(list)) {
      return;
    }
    for (const entry of list) {
      const normalized = normalizeTelegramAllowFromEntry(entry);
      if (!normalized || normalized === "*") {
        continue;
      }
      if (isNumericTelegramUserId(normalized)) {
        continue;
      }
      hits.push({ path: pathLabel, entry: String(entry).trim() });
    }
  };

  for (const scope of collectTelegramAccountScopes(cfg)) {
    for (const ref of collectTelegramAllowFromLists(scope.prefix, scope.account)) {
      scanList(ref.pathLabel, ref.holder[ref.key]);
    }
  }

  return hits;
}

export async function maybeRepairTelegramAllowFromUsernames(cfg: DenebConfig): Promise<{
  config: DenebConfig;
  changes: string[];
}> {
  const hits = scanTelegramAllowFromUsernameEntries(cfg);
  if (hits.length === 0) {
    return { config: cfg, changes: [] };
  }

  const { resolvedConfig } = await resolveCommandSecretRefsViaGateway({
    config: cfg,
    commandName: "doctor --fix",
    targetIds: getChannelsCommandSecretTargetIds(),
    mode: "read_only_status",
  });
  const hasConfiguredUnavailableToken = listTelegramAccountIds(cfg).some((accountId) => {
    const inspected = inspectTelegramAccount({ cfg, accountId });
    return inspected.enabled && inspected.tokenStatus === "configured_unavailable";
  });
  const tokenResolutionWarnings: string[] = [];
  const tokens = Array.from(
    new Set(
      listTelegramAccountIds(resolvedConfig)
        .map((accountId) => {
          try {
            return resolveTelegramAccount({ cfg: resolvedConfig, accountId });
          } catch (error) {
            tokenResolutionWarnings.push(
              `- Telegram account ${accountId}: failed to inspect bot token (${describeUnknownError(error)}).`,
            );
            return null;
          }
        })
        .filter((account): account is NonNullable<ReturnType<typeof resolveTelegramAccount>> =>
          Boolean(account),
        )
        .map((account) => (account.tokenSource === "none" ? "" : account.token))
        .map((token) => token.trim())
        .filter(Boolean),
    ),
  );

  if (tokens.length === 0) {
    return {
      config: cfg,
      changes: [
        ...tokenResolutionWarnings,
        hasConfiguredUnavailableToken
          ? `- Telegram allowFrom contains @username entries, but configured Telegram bot credentials are unavailable in this command path; cannot auto-resolve (start the gateway or make the secret source available, then rerun doctor --fix).`
          : `- Telegram allowFrom contains @username entries, but no Telegram bot token is configured; cannot auto-resolve (run setup or replace with numeric sender IDs).`,
      ],
    };
  }

  const resolveUserId = async (raw: string): Promise<string | null> => {
    const trimmed = raw.trim();
    if (!trimmed) {
      return null;
    }
    const stripped = normalizeTelegramAllowFromEntry(trimmed);
    if (!stripped || stripped === "*") {
      return null;
    }
    if (isNumericTelegramUserId(stripped)) {
      return stripped;
    }
    if (/\s/.test(stripped)) {
      return null;
    }
    const username = stripped.startsWith("@") ? stripped : `@${stripped}`;
    for (const token of tokens) {
      const controller = new AbortController();
      const timeout = setTimeout(() => controller.abort(), 4000);
      try {
        const id = await fetchTelegramChatId({
          token,
          chatId: username,
          signal: controller.signal,
        });
        if (id) {
          return id;
        }
      } catch {
        // ignore and try next token
      } finally {
        clearTimeout(timeout);
      }
    }
    return null;
  };

  const changes: string[] = [];
  const next = structuredClone(cfg);

  const repairList = async (pathLabel: string, holder: Record<string, unknown>, key: string) => {
    const raw = holder[key];
    if (!Array.isArray(raw)) {
      return;
    }
    const out: Array<string | number> = [];
    const replaced: Array<{ from: string; to: string }> = [];
    for (const entry of raw) {
      const normalized = normalizeTelegramAllowFromEntry(entry);
      if (!normalized) {
        continue;
      }
      if (normalized === "*") {
        out.push("*");
        continue;
      }
      if (isNumericTelegramUserId(normalized)) {
        out.push(normalized);
        continue;
      }
      const resolved = await resolveUserId(String(entry));
      if (resolved) {
        out.push(resolved);
        replaced.push({ from: String(entry).trim(), to: resolved });
      } else {
        out.push(String(entry).trim());
      }
    }
    const deduped: Array<string | number> = [];
    const seen = new Set<string>();
    for (const entry of out) {
      const k = String(entry).trim();
      if (!k || seen.has(k)) {
        continue;
      }
      seen.add(k);
      deduped.push(entry);
    }
    holder[key] = deduped;
    if (replaced.length > 0) {
      for (const rep of replaced.slice(0, 5)) {
        changes.push(`- ${pathLabel}: resolved ${rep.from} -> ${rep.to}`);
      }
      if (replaced.length > 5) {
        changes.push(`- ${pathLabel}: resolved ${replaced.length - 5} more @username entries`);
      }
    }
  };

  const repairAccount = async (prefix: string, account: Record<string, unknown>) => {
    for (const ref of collectTelegramAllowFromLists(prefix, account)) {
      await repairList(ref.pathLabel, ref.holder, ref.key);
    }
  };

  for (const scope of collectTelegramAccountScopes(next)) {
    await repairAccount(scope.prefix, scope.account);
  }

  if (changes.length === 0) {
    return { config: cfg, changes: [] };
  }
  return { config: next, changes };
}
