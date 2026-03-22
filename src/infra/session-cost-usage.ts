// Stub: session cost usage tracking removed for solo-dev simplification.
export type CostUsageSummary = {
  totalCostUsd?: number;
  totalInputTokens?: number;
  totalOutputTokens?: number;
};

export async function loadSessionCostUsageSummary(): Promise<CostUsageSummary> {
  return {};
}
