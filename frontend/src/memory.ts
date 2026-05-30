// memory.ts — typed clients for the miniapp.memory.* page/category RPCs.
//
// Free-text search across wiki / diary / people moved to search.ts
// (miniapp.search.all). This file is now just the page-CRUD + category
// browsing surface used by wiki_page, wiki_new, categories, and
// category_pages views.

import { call } from './rpc';

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

// --- Page merge (category-page multi-select → 병합) ---

export interface MergeResult {
  ok: boolean;
  started: boolean; // merge accepted; runs in the background
  targetPath: string;
  mergedTitle?: string;
}

/**
 * mergePages starts folding the source page into the target and returns as
 * soon as the job is accepted — the merge runs in the BACKGROUND on the
 * gateway. The slow step (synthesizing the combined body with the lightweight
 * model) happens off the request path with a generous timeout; when it
 * finishes (combined body written, referencing pages repointed, source
 * deleted — or a concatenation fallback if the model is unavailable) the user
 * gets a Telegram completion notice. So this call returns quickly and does NOT
 * mean the merge is done yet.
 */
export function mergePages(
  initData: string,
  targetPath: string,
  sourcePath: string,
): Promise<MergeResult> {
  return call<MergeResult>(
    'miniapp.memory.merge',
    { targetPath, sourcePath },
    initData,
  );
}
