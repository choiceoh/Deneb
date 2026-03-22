// Stub: rate limiting removed for solo-dev simplification.

export type RateLimitConfig = {
  maxAttempts?: number;
  windowMs?: number;
  lockoutMs?: number;
  exemptLoopback?: boolean;
  pruneIntervalMs?: number;
};

export type AuthRateLimiter = {
  check: (
    ip: string,
    scope?: string,
  ) => { allowed: boolean; remaining: number; retryAfterMs: number };
  recordFailure: (ip: string, scope?: string) => void;
  reset: (ip: string, scope?: string) => void;
  size: () => number;
  prune: () => void;
  dispose: () => void;
};

export const AUTH_RATE_LIMIT_SCOPE_DEFAULT = "default";
export const AUTH_RATE_LIMIT_SCOPE_SHARED_SECRET = "shared-secret";
export const AUTH_RATE_LIMIT_SCOPE_DEVICE_TOKEN = "device-token";
export const AUTH_RATE_LIMIT_SCOPE_HOOK_AUTH = "hook-auth";

export type RateLimitCheckResult = { allowed: boolean; remaining: number; retryAfterMs: number };

export function normalizeRateLimitClientIp(ip?: string | null): string {
  return ip?.trim() || "127.0.0.1";
}

export function createAuthRateLimiter(_config?: RateLimitConfig): AuthRateLimiter {
  return {
    check: () => ({ allowed: true, remaining: 999, retryAfterMs: 0 }),
    recordFailure: () => {},
    reset: () => {},
    size: () => 0,
    prune: () => {},
    dispose: () => {},
  };
}
