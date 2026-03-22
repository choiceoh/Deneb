// Minimal stub: provider usage tracking removed for solo-dev simplification.

export type UsageProviderId = string;

export type UsageWindow = {
  start: number;
  end: number;
  used?: number;
  limit?: number;
  resetAt?: number;
};

export type ProviderUsageSnapshot = {
  provider: string;
  error?: string;
  windows: UsageWindow[];
};

export type UsageSummary = {
  providers: ProviderUsageSnapshot[];
};

export function resolveUsageProviderId(_provider: string): UsageProviderId | undefined {
  return undefined;
}

export async function loadProviderUsageSummary(_opts?: {
  timeoutMs?: number;
  providers?: string[];
  agentDir?: string;
}): Promise<UsageSummary> {
  return { providers: [] };
}

export function formatUsageReportLines(_usage: UsageSummary): string[] {
  return [];
}

export function formatUsageSummaryLine(_snapshot: ProviderUsageSnapshot): string {
  return "";
}

export function formatUsageWindowSummary(
  _snapshot: ProviderUsageSnapshot,
  _opts?: { now?: number; maxWindows?: number; includeResets?: boolean },
): string {
  return "";
}
