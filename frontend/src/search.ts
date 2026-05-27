// search.ts — typed client for the miniapp.search.all unified RPC.
//
// One call returns three sections (wiki / diary / people) so home's
// search input can fan out to all three indexes without the operator
// picking a domain up front.

import { call } from './rpc';

export interface SearchPersonHit {
  email: string;
  name?: string;
  messageCount: number;
  /** ISO 8601 — when the most recent message arrived. */
  lastSeen?: string;
  /** Truncated preview of the most recent message subject. */
  lastSubject?: string;
}

export interface SearchWikiHit {
  path: string;
  title?: string;
  summary?: string;
  category?: string;
  snippet: string;
  score: number;
}

export interface SearchDiaryHit {
  file: string;
  header: string;
  content: string;
  at?: number;
  score: number;
}

export interface SearchAllResult {
  wiki: SearchWikiHit[];
  diary: SearchDiaryHit[];
  people: SearchPersonHit[];
}

export function searchAll(
  initData: string,
  query: string,
  limit?: number,
): Promise<SearchAllResult> {
  return call<SearchAllResult>('miniapp.search.all', { query, limit }, initData);
}
