// sessions.ts — typed client for the miniapp.sessions.recent RPC.

import { call } from './rpc';

export interface SessionRow {
  key: string;
  kind?: string;
  status?: string;
  channel?: string;
  model?: string;
  label?: string;
  /** UnixMilli */
  updatedAtMs?: number;
  startedAtMs?: number;
  runtimeMs?: number;
  totalTokens?: number;
}

interface SessionsResult {
  sessions: SessionRow[];
  count: number;
}

export function recentSessions(
  initData: string,
  opts: { limit?: number; channel?: string } = {},
): Promise<SessionsResult> {
  return call<SessionsResult>('miniapp.sessions.recent', opts, initData);
}
