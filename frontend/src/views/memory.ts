// views/memory.ts — Wiki / memory search view. Single input + result list.
//
// Search runs on form submit (Enter), debounced typing is overkill for the
// PoC and would hammer the backend. Empty query → empty state; error →
// banner that doesn't replace prior results.

import { searchMemory, type MemoryHit } from '../memory';
import { RpcError } from '../rpc';
import { navigate } from '../router';

let lastQuery = '';

export function renderMemory(root: HTMLElement, initData: string): void {
  root.innerHTML = '';

  const header = document.createElement('div');
  header.className = 'view-header';
  header.innerHTML = `
    <span class="view-title">메모리 검색</span>
    <span></span>
  `;
  root.appendChild(header);

  const form = document.createElement('form');
  form.className = 'search-form';
  form.innerHTML = `
    <input type="search" class="search-input" placeholder="검색어를 입력하세요" autocomplete="off" enterkeyhint="search" />
    <button type="submit" class="primary search-submit">검색</button>
  `;
  const input = form.querySelector('input') as HTMLInputElement;
  if (lastQuery) input.value = lastQuery;
  const results = document.createElement('div');
  results.className = 'search-results';
  if (!lastQuery) {
    results.innerHTML = '<div class="empty-state">기억할 만한 것을 찾아보세요</div>';
  }
  root.appendChild(form);
  root.appendChild(results);

  form.addEventListener('submit', (ev) => {
    ev.preventDefault();
    const q = input.value.trim();
    if (!q) {
      results.innerHTML = '<div class="empty-state">검색어를 입력하세요</div>';
      return;
    }
    lastQuery = q;
    void runSearch(initData, q, results);
  });

  // If we're returning to this view with a remembered query, re-run.
  if (lastQuery) {
    void runSearch(initData, lastQuery, results);
  }

  // Focus the input on first paint so the soft keyboard pops up.
  setTimeout(() => input.focus(), 50);
}

async function runSearch(initData: string, q: string, mount: HTMLElement): Promise<void> {
  mount.innerHTML = '<div class="loading">검색 중…</div>';
  try {
    const { results } = await searchMemory(initData, q, 20);
    if (results.length === 0) {
      mount.innerHTML = '';
      const empty = document.createElement('div');
      empty.className = 'empty-state';
      empty.textContent = `"${q}" 에 대한 결과가 없습니다`;
      mount.appendChild(empty);
      return;
    }
    mount.innerHTML = '';
    for (const hit of results) {
      mount.appendChild(buildHit(hit));
    }
  } catch (err) {
    const msg =
      err instanceof RpcError
        ? `${err.code} — ${err.message}`
        : err instanceof Error
          ? err.message
          : '알 수 없는 오류';
    mount.innerHTML = '';
    const banner = document.createElement('div');
    banner.className = 'error';
    banner.textContent = `검색 실패: ${msg}`;
    mount.appendChild(banner);
  }
}

function buildHit(hit: MemoryHit): HTMLElement {
  const card = document.createElement('button');
  card.className = 'memory-card';

  const title = document.createElement('div');
  title.className = 'memory-title';
  title.textContent = hit.title || hit.path;
  card.appendChild(title);

  if (hit.summary) {
    const summary = document.createElement('div');
    summary.className = 'memory-summary';
    summary.textContent = hit.summary;
    card.appendChild(summary);
  }

  const snippet = document.createElement('div');
  snippet.className = 'memory-snippet';
  snippet.textContent = hit.snippet;
  card.appendChild(snippet);

  const meta = document.createElement('div');
  meta.className = 'memory-meta';
  const cat = hit.category ? `#${hit.category}` : '';
  const score = `score ${hit.score.toFixed(2)}`;
  meta.textContent = [cat, hit.path, score].filter(Boolean).join(' · ');
  card.appendChild(meta);

  // Tapping a result navigates back home for now — opening a full wiki
  // page view is left as future work. We keep the card focusable so
  // screen-reader users can still feel the interaction surface.
  card.addEventListener('click', () => navigate({ name: 'home' }));
  return card;
}
