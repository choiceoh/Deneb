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

export interface MemoryPage {
  path: string;
  title?: string;
  summary?: string;
  category?: string;
  tags?: string[];
  related?: string[];
  created?: string;
  updated?: string;
  due?: string;
  importance?: number;
  body: string;
}

export function getPage(initData: string, path: string): Promise<MemoryPage> {
  return call<MemoryPage>('miniapp.memory.get_page', { path }, initData);
}
