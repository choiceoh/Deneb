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
 * writePage replaces the body of an existing wiki page and optionally
 * overrides selected frontmatter fields. Category is NOT editable here
 * (it encodes the on-disk path) — for that, create a new page.
 *
 * Field semantics:
 *  - body: always sent. Required.
 *  - title / summary: omit to preserve existing; send "" to clear.
 *  - tags: omit to preserve existing; send [] to clear; send [...] to
 *    replace. Blank tag entries are dropped server-side.
 *
 * Updated date is bumped to today regardless. Response is the full
 * updated page in the same shape as getPage.
 */
export function writePage(
  initData: string,
  path: string,
  body: string,
  frontmatter?: { title?: string; summary?: string; tags?: string[] },
): Promise<MemoryPage> {
  const params: Record<string, unknown> = { path, body };
  if (frontmatter?.title !== undefined) params.title = frontmatter.title;
  if (frontmatter?.summary !== undefined) params.summary = frontmatter.summary;
  if (frontmatter?.tags !== undefined) params.tags = frontmatter.tags;
  return call<MemoryPage>('miniapp.memory.write_page', params, initData);
}

/**
 * createPage creates a brand-new wiki page. The path is computed
 * server-side from `<category>/<slugified-title>.md`. Returns the
 * full page (same shape as getPage) on success.
 */
export function createPage(
  initData: string,
  input: {
    title: string;
    category: string;
    summary?: string;
    tags?: string[];
    body?: string;
  },
): Promise<MemoryPage> {
  return call<MemoryPage>('miniapp.memory.create_page', input, initData);
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
