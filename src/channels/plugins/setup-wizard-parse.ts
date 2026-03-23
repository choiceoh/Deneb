import type { ChannelSetupWizardAllowFromEntry } from "./setup-wizard.js";

export function addWildcardAllowFrom(allowFrom?: Array<string | number> | null): string[] {
  const next = (allowFrom ?? []).map((v) => String(v).trim()).filter(Boolean);
  if (!next.includes("*")) {
    next.push("*");
  }
  return next;
}

export function mergeAllowFromEntries(
  current: Array<string | number> | null | undefined,
  additions: Array<string | number>,
): string[] {
  const merged = [...(current ?? []), ...additions].map((v) => String(v).trim()).filter(Boolean);
  return [...new Set(merged)];
}

export function splitSetupEntries(raw: string): string[] {
  return raw
    .split(/[\n,;]+/g)
    .map((entry) => entry.trim())
    .filter(Boolean);
}

type ParsedSetupEntry = { value: string } | { error: string };

export function parseSetupEntriesWithParser(
  raw: string,
  parseEntry: (entry: string) => ParsedSetupEntry,
): { entries: string[]; error?: string } {
  const parts = splitSetupEntries(String(raw ?? ""));
  const entries: string[] = [];
  for (const part of parts) {
    const parsed = parseEntry(part);
    if ("error" in parsed) {
      return { entries: [], error: parsed.error };
    }
    entries.push(parsed.value);
  }
  return { entries: normalizeAllowFromEntries(entries) };
}

export function parseSetupEntriesAllowingWildcard(
  raw: string,
  parseEntry: (entry: string) => ParsedSetupEntry,
): { entries: string[]; error?: string } {
  return parseSetupEntriesWithParser(raw, (entry) => {
    if (entry === "*") {
      return { value: "*" };
    }
    return parseEntry(entry);
  });
}

export function parseMentionOrPrefixedId(params: {
  value: string;
  mentionPattern: RegExp;
  prefixPattern?: RegExp;
  idPattern: RegExp;
  normalizeId?: (id: string) => string;
}): string | null {
  const trimmed = params.value.trim();
  if (!trimmed) {
    return null;
  }

  const mentionMatch = trimmed.match(params.mentionPattern);
  if (mentionMatch?.[1]) {
    return params.normalizeId ? params.normalizeId(mentionMatch[1]) : mentionMatch[1];
  }

  const stripped = params.prefixPattern ? trimmed.replace(params.prefixPattern, "") : trimmed;
  if (!params.idPattern.test(stripped)) {
    return null;
  }

  return params.normalizeId ? params.normalizeId(stripped) : stripped;
}

export function normalizeAllowFromEntries(
  entries: Array<string | number>,
  normalizeEntry?: (value: string) => string | null | undefined,
): string[] {
  const normalized = entries
    .map((entry) => String(entry).trim())
    .filter(Boolean)
    .map((entry) => {
      if (entry === "*") {
        return "*";
      }
      if (!normalizeEntry) {
        return entry;
      }
      const value = normalizeEntry(entry);
      return typeof value === "string" ? value.trim() : "";
    })
    .filter(Boolean);
  return [...new Set(normalized)];
}

export function resolveParsedAllowFromEntries(params: {
  entries: string[];
  parseId: (raw: string) => string | null;
}): ChannelSetupWizardAllowFromEntry[] {
  return params.entries.map((entry) => {
    const id = params.parseId(entry);
    return {
      input: entry,
      resolved: Boolean(id),
      id,
    };
  });
}
