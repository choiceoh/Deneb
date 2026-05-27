// people.ts — typed client for the miniapp.people.* RPCs.

import { call } from './rpc';

export interface PersonRow {
  email: string;
  name?: string;
  messageCount: number;
  /** ISO 8601 — when the most recent message arrived. */
  lastSeen?: string;
  /** Truncated preview of the most recent message subject. */
  lastSubject?: string;
}

interface ListResult {
  people: PersonRow[];
  windowDays: number;
  scannedCount: number;
}

export function listPeople(
  initData: string,
  opts?: { limit?: number; windowDays?: number },
): Promise<ListResult> {
  return call<ListResult>(
    'miniapp.people.list',
    { limit: opts?.limit, windowDays: opts?.windowDays },
    initData,
  );
}
