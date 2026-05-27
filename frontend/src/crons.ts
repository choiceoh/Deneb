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
