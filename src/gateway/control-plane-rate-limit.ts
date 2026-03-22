// Stub: control plane rate limiting removed for solo-dev simplification.
export function checkControlPlaneRateLimit(_key: string): { allowed: boolean } {
  return { allowed: true };
}

export const CONTROL_PLANE_RATE_LIMIT_MAX_REQUESTS = 999;
export const CONTROL_PLANE_RATE_LIMIT_WINDOW_MS = 60_000;

export function consumeControlPlaneWriteBudget(_key?: string): { allowed: boolean } {
  return { allowed: true };
}
