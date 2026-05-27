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

/**
 * writePage replaces the body of an existing wiki page. Frontmatter
 * is preserved server-side (the handler reads the page first and
 * merges the new body in). Updated date is bumped to today. The
 * response is the full updated page in the same shape as getPage.
 */
export function writePage(
  initData: string,
  path: string,
  body: string,
): Promise<MemoryPage> {
  return call<MemoryPage>('miniapp.memory.write_page', { path, body }, initData);
}

// --- Categories explorer (더보기 > 📂 카테고리) ---

export interface MemoryCategory {
  name: string;
  pageCount: number;
}

interface CategoriesResult {
  categories: MemoryCategory[];
  totalPages: number;
  totalBytes: number;
}

export function listCategories(initData: string): Promise<CategoriesResult> {
  return call<CategoriesResult>('miniapp.memory.categories', {}, initData);
}

export interface MemoryPageRow {
  path: string;
  title?: string;
  summary?: string;
  updated?: string;
}

interface ListInCategoryResult {
  category: string;
  pages: MemoryPageRow[];
  total: number;
}

export function listPagesInCategory(
  initData: string,
  category: string,
  limit?: number,
): Promise<ListInCategoryResult> {
  return call<ListInCategoryResult>(
    'miniapp.memory.list_in_category',
    { category, limit },
    initData,
  );
}

// --- Diary timeline (더보기 > 📖 다이어리) ---

export interface DiaryEntry {
  file: string;    // e.g., "diary-2026-05-26.md"
  header: string;  // e.g., "14:30"
  content: string;
  at?: number;     // unix millis derived from filename + header
}

interface DiaryRecentResult {
  entries: DiaryEntry[];
}

export function recentDiary(
  initData: string,
  limit?: number,
): Promise<DiaryRecentResult> {
  return call<DiaryRecentResult>('miniapp.memory.diary_recent', { limit }, initData);
}
