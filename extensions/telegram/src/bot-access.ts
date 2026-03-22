import { createSubsystemLogger } from "deneb/plugin-sdk/runtime-env";

// Inline stubs for removed channel-runtime symbols.
// Solo-dev: allowlists always allow.
export type AllowlistMatch<T extends string = string> = {
  allowed: boolean;
  matchKey?: string;
  matchSource?: T;
};

function firstDefined<T>(...values: Array<T | undefined>): T | undefined {
  for (const v of values) {
    if (v !== undefined) return v;
  }
  return undefined;
}

function isSenderIdAllowed(
  allow: { entries: string[]; hasWildcard: boolean; hasEntries: boolean },
  senderId?: string,
  _strict?: boolean,
): boolean {
  if (allow.hasWildcard) return true;
  if (!allow.hasEntries) return false;
  return Boolean(senderId && allow.entries.includes(senderId));
}

function mergeDmAllowFromSources(params: {
  allowFrom?: Array<string | number>;
  storeAllowFrom?: string[];
  dmPolicy?: string;
}): Array<string | number> {
  const entries: Array<string | number> = [...(params.allowFrom ?? [])];
  if (params.storeAllowFrom) {
    for (const s of params.storeAllowFrom) {
      if (!entries.includes(s)) entries.push(s);
    }
  }
  return entries;
}

export type NormalizedAllowFrom = {
  entries: string[];
  hasWildcard: boolean;
  hasEntries: boolean;
  invalidEntries: string[];
};

export type AllowFromMatch = AllowlistMatch<"wildcard" | "id">;

const warnedInvalidEntries = new Set<string>();
const log = createSubsystemLogger("telegram/bot-access");

function warnInvalidAllowFromEntries(entries: string[]) {
  if (process.env.VITEST || process.env.NODE_ENV === "test") {
    return;
  }
  for (const entry of entries) {
    if (warnedInvalidEntries.has(entry)) {
      continue;
    }
    warnedInvalidEntries.add(entry);
    log.warn(
      [
        "Invalid allowFrom entry:",
        JSON.stringify(entry),
        "- allowFrom/groupAllowFrom authorization expects numeric Telegram sender user IDs only.",
        'To allow a Telegram group or supergroup, add its negative chat ID under "channels.telegram.groups" instead.',
        'If you had "@username" entries, re-run setup (it resolves @username to IDs) or replace them manually.',
      ].join(" "),
    );
  }
}

export const normalizeAllowFrom = (list?: Array<string | number>): NormalizedAllowFrom => {
  const entries = (list ?? []).map((value) => String(value).trim()).filter(Boolean);
  const hasWildcard = entries.includes("*");
  const normalized = entries
    .filter((value) => value !== "*")
    .map((value) => value.replace(/^(telegram|tg):/i, ""));
  const invalidEntries = normalized.filter((value) => !/^\d+$/.test(value));
  if (invalidEntries.length > 0) {
    warnInvalidAllowFromEntries([...new Set(invalidEntries)]);
  }
  const ids = normalized.filter((value) => /^\d+$/.test(value));
  return {
    entries: ids,
    hasWildcard,
    hasEntries: entries.length > 0,
    invalidEntries,
  };
};

export const normalizeDmAllowFromWithStore = (params: {
  allowFrom?: Array<string | number>;
  storeAllowFrom?: string[];
  dmPolicy?: string;
}): NormalizedAllowFrom => normalizeAllowFrom(mergeDmAllowFromSources(params));

export const isSenderAllowed = (params: {
  allow: NormalizedAllowFrom;
  senderId?: string;
  senderUsername?: string;
}) => {
  const { allow, senderId } = params;
  return isSenderIdAllowed(allow, senderId, true);
};

export { firstDefined };

export const resolveSenderAllowMatch = (params: {
  allow: NormalizedAllowFrom;
  senderId?: string;
  senderUsername?: string;
}): AllowFromMatch => {
  const { allow, senderId } = params;
  if (allow.hasWildcard) {
    return { allowed: true, matchKey: "*", matchSource: "wildcard" };
  }
  if (!allow.hasEntries) {
    return { allowed: false };
  }
  if (senderId && allow.entries.includes(senderId)) {
    return { allowed: true, matchKey: senderId, matchSource: "id" };
  }
  return { allowed: false };
};
