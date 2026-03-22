import { vi } from "vitest";

/**
 * Freeze time at the given instant using Vitest fake timers.
 * Use when testing time-dependent logic (cooldowns, expiry, cron schedules).
 * The fake timer leak guard in test/setup.ts will auto-restore if you forget,
 * but calling `useRealTime()` explicitly is preferred.
 */
export function useFrozenTime(at: string | number | Date): void {
  vi.useFakeTimers();
  vi.setSystemTime(at);
}

/** Restore real timers after a `useFrozenTime()` call. */
export function useRealTime(): void {
  vi.useRealTimers();
}
