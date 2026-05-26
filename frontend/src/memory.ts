// memory.ts — typed client for the miniapp.memory.search RPC.

import { call } from './rpc';

export interface MemoryHit {
  path: string;
  title?: string;
  summary?: string;
  category?: string;
  snippet: string;
  score: number;
}

interface SearchResult {
  results: MemoryHit[];
}

export function searchMemory(
  initData: string,
  query: string,
  limit?: number,
): Promise<SearchResult> {
  return call<SearchResult>('miniapp.memory.search', { query, limit }, initData);
}
