// Session cost/usage tracking (disabled in solo-dev mode).

export type SessionMessageCounts = {
  total: number;
  user: number;
  assistant: number;
  toolCalls: number;
  toolResults: number;
  errors: number;
};

export type SessionToolUsageEntry = {
  name: string;
  count: number;
};

export type SessionToolUsage = {
  totalCalls: number;
  uniqueTools: number;
  tools: SessionToolUsageEntry[];
};

export type SessionModelUsageTotals = {
  totalTokens: number;
  totalCost: number;
  input: number;
  output: number;
  cacheRead: number;
  cacheWrite: number;
  inputCost: number;
  outputCost: number;
  cacheReadCost: number;
  cacheWriteCost: number;
  missingCostEntries: number;
};

export type SessionModelUsage = {
  model?: string;
  provider?: string;
  count: number;
  totals: SessionModelUsageTotals;
};

export type SessionLatencyStats = {
  avgMs: number;
  minMs: number;
  p50Ms?: number;
  p95Ms: number;
  p99Ms?: number;
  maxMs: number;
  count: number;
};

export type SessionDailyLatency = {
  date: string;
  avgMs: number;
  minMs: number;
  maxMs: number;
  p95Ms: number;
  count: number;
};

export type SessionDailyModelUsage = {
  date: string;
  provider?: string;
  model?: string;
  tokens: number;
  cost: number;
  count: number;
};

export type SessionCostSummary = {
  input: number;
  output: number;
  cacheRead: number;
  cacheWrite: number;
  totalTokens: number;
  totalCost: number;
  inputCost?: number;
  outputCost?: number;
  cacheReadCost?: number;
  cacheWriteCost?: number;
  missingCostEntries?: number;
  durationMs?: number;
  firstActivity?: number;
  lastActivity?: number;
  activityDates?: string[];
  dailyBreakdown?: Array<{ date: string; tokens: number; cost: number }>;
  messageCounts?: SessionMessageCounts;
  toolUsage?: SessionToolUsage;
  modelUsage?: SessionModelUsage[];
  latency?: SessionLatencyStats;
  dailyMessageCounts?: Array<{ date: string; total: number; toolCalls: number; errors: number }>;
  dailyLatency?: SessionDailyLatency[];
  dailyModelUsage?: SessionDailyModelUsage[];
};

export type CostUsageSummary = {
  updatedAt?: number;
  days?: number;
  totals: SessionModelUsageTotals;
  daily: Array<{
    date: string;
    totalCost: number;
    totalTokens: number;
    input: number;
    output: number;
    cacheRead: number;
    cacheWrite: number;
    inputCost?: number;
    outputCost?: number;
    cacheReadCost?: number;
    cacheWriteCost?: number;
    missingCostEntries?: number;
  }>;
};

export async function loadSessionCostUsageSummary(): Promise<CostUsageSummary> {
  return {
    totals: {
      totalTokens: 0,
      totalCost: 0,
      input: 0,
      output: 0,
      cacheRead: 0,
      cacheWrite: 0,
      inputCost: 0,
      outputCost: 0,
      cacheReadCost: 0,
      cacheWriteCost: 0,
      missingCostEntries: 0,
    },
    daily: [],
  };
}
