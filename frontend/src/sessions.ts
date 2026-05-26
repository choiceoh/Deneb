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

export interface TranscriptMessage {
  id?: string;
  role: string;
  content: string;
  timestampMs?: number;
}

export interface TranscriptResult {
  sessionKey: string;
  messages: TranscriptMessage[];
  total: number;
}

export function getTranscript(
  initData: string,
  sessionKey: string,
  limit?: number,
): Promise<TranscriptResult> {
  return call<TranscriptResult>(
    'miniapp.sessions.transcript',
    { sessionKey, limit },
    initData,
  );
}
