// crons.ts — typed client for the miniapp.crons.* RPCs.
//
// The Mini App's 더보기 > ⚡ 자동 작업 surface uses this to show what's
// wired up. Power-user cron editing lives in the operator tool RPCs
// (`cron.add` / `cron.update` / `cron.remove`) — those aren't exposed
// through the Mini App because mutating cron jobs from a phone surface
// would be more risky than useful for a single-operator deployment.

import { call } from './rpc';

export interface CronJobRow {
  id: string;
  name?: string;
  enabled: boolean;
  /** Human-readable summary, Korean. e.g. "0 9 * * * (Asia/Seoul)" or "2분마다". */
  schedule: string;
  /** "agentTurn" | "systemEvent". */
  payloadKind: string;
  /** Capped preview of the job message/text (max 120 runes). */
  payloadPreview?: string;
  /** Unix ms — when the job will fire next. 0 if unknown. */
  nextRunAtMs?: number;
  consecutiveErrors?: number;
  /** Unix ms — when the job was auto-disabled due to repeated errors. */
  autoDisabledAtMs?: number;
  lastError?: string;
}

interface ListResult {
  jobs: CronJobRow[];
  total: number;
}

export function listCrons(
  initData: string,
  opts?: { limit?: number; includeDisabled?: boolean },
): Promise<ListResult> {
  return call<ListResult>(
    'miniapp.crons.list',
    { limit: opts?.limit, includeDisabled: opts?.includeDisabled },
    initData,
  );
}

/**
 * CronJobDetail is the full per-job view behind a list row — what the
 * cron detail screen renders on tap. Unlike CronJobRow it carries the
 * full prompt (no 120-rune cap), the delivery target, the parsed
 * schedule pieces, and the execution context. Mirror of the Go
 * `MiniappCronDetail` (handlerminiapp/crons.go). Still read-only —
 * editing lives in the operator tool RPCs.
 */
export interface CronJobDetail {
  id: string;
  name?: string;
  enabled: boolean;
  /** Which agent runs the job. */
  agentId?: string;
  /** "main" | "isolated" | "current" | "subagent". */
  sessionTarget?: string;

  /** Same one-line Korean summary as the list row. */
  schedule: string;
  /** Round-trippable spec the edit form pre-fills ("0 9 * * *", "15m", ISO). */
  scheduleSpec: string;
  /** "at" | "every" | "cron". */
  scheduleKind: string;
  timezone?: string;
  /** Raw cron expression (kind=cron). */
  cronExpr?: string;
  staggerMs?: number;

  /** "agentTurn" | "systemEvent". */
  payloadKind: string;
  /** Full job message/text — not truncated. */
  prompt?: string;
  model?: string;
  thinking?: string;
  timeoutSeconds?: number;
  lightContext?: boolean;
  retryCount?: number;

  deliveryChannel?: string;
  deliveryTo?: string;
  deliveryThreadId?: string;

  /** Consecutive failures before a heads-up fires. */
  failureAlertAfter?: number;

  nextRunAtMs?: number;
  lastSessionKey?: string;
  lastDeliveryStatus?: string;
  lastError?: string;
  consecutiveErrors?: number;
  autoDisabledAtMs?: number;
  createdAtMs?: number;
  updatedAtMs?: number;
}

export function getCron(initData: string, id: string): Promise<CronJobDetail> {
  return call<CronJobDetail>('miniapp.crons.get', { id }, initData);
}

/**
 * Patch a cron job. Every field is optional — only the keys present are
 * changed (the toggle sends just `{ enabled }`, the edit form sends the
 * full set). `schedule` is a smart spec ("0 9 * * *", "15m", an ISO
 * timestamp); the backend validates it and rejects a malformed value.
 * Returns the updated detail.
 */
export interface CronUpdatePatch {
  name?: string;
  enabled?: boolean;
  schedule?: string;
  tz?: string;
  prompt?: string;
  model?: string;
  thinking?: string;
  timeoutSeconds?: number;
  retryCount?: number;
  agentId?: string;
  delivery?: { channel?: string; to?: string; threadId?: string };
}

export function updateCron(
  initData: string,
  id: string,
  patch: CronUpdatePatch,
): Promise<CronJobDetail> {
  return call<CronJobDetail>('miniapp.crons.update', { id, ...patch }, initData);
}

/** Queue an immediate run (async — result arrives via the job's delivery). */
export function runCron(initData: string, id: string): Promise<{ enqueued: boolean }> {
  return call<{ enqueued: boolean }>('miniapp.crons.run', { id }, initData);
}

/** Delete a cron job. */
export function removeCron(initData: string, id: string): Promise<{ removed: boolean }> {
  return call<{ removed: boolean }>('miniapp.crons.remove', { id }, initData);
}
