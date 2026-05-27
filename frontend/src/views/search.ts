// views/search.ts — Unified search across wiki / diary / people.
//
// Single text input on home now feeds all three indexes. Empty query
// renders an empty screen — there is no "browse all" surface anymore;
// discovery happens through the search box only.
//
// Result sections are stacked top-down (wiki → diary → people) so the
// most-likely-relevant hits (wiki pages, scored full-text) are above
// the fold. Each section uses its domain-specific card style so the
// operator reads "this is a wiki hit" / "this is a diary excerpt" /
// "this is a person" at a glance without an explicit label header
// styled differently from the rest of the page.

import { searchAll, type SearchAllResult, type SearchDiaryHit, type SearchPersonHit, type SearchWikiHit } from '../search';
import { formatRpcError, relativeTime } from '../format';
import { isCurrentHash, navigate } from '../router';
import { buildErrorBanner, buildViewHeader } from './ui';

let lastQuery = '';

export function renderSearch(root: HTMLElement, initData: string): void {
  root.innerHTML = '';

  root.appendChild(
    buildViewHeader({
      title: 'search',
      right: { label: '+ new', onClick: () => navigate({ name: 'wikiNew' }) },
    }),
  );

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
  root.appendChild(form);
  root.appendChild(results);

  form.addEventListener('submit', (ev) => {
    ev.preventDefault();
    const q = input.value.trim();
    if (!q) {
      results.innerHTML = '';
      lastQuery = '';
      return;
    }
    lastQuery = q;
    void runSearch(initData, q, results);
  });

  if (lastQuery) {
    void runSearch(initData, lastQuery, results);
  }

  setTimeout(() => input.focus(), 50);
}

async function runSearch(initData: string, q: string, mount: HTMLElement): Promise<void> {
  const expectedHash = location.hash;
  mount.innerHTML = '<div class="loading">검색 중…</div>';
  try {
    const data = await searchAll(initData, q, 10);
    if (!isCurrentHash(expectedHash)) return;
    paintResults(mount, q, data);
  } catch (err) {
    if (!isCurrentHash(expectedHash)) return;
    mount.innerHTML = '';
    mount.appendChild(buildErrorBanner(`검색 실패: ${formatRpcError(err)}`));
  }
}

function paintResults(mount: HTMLElement, q: string, data: SearchAllResult): void {
  mount.innerHTML = '';
  const total = data.wiki.length + data.diary.length + data.people.length;
  if (total === 0) {
    const empty = document.createElement('div');
    empty.className = 'empty-state';
    empty.textContent = `no matches for "${q}"`;
    mount.appendChild(empty);
    return;
  }

  if (data.wiki.length > 0) {
    mount.appendChild(buildSectionHeader('memory'));
    for (const hit of data.wiki) mount.appendChild(buildWikiCard(hit));
  }
  if (data.diary.length > 0) {
    mount.appendChild(buildSectionHeader('diary'));
    for (const hit of data.diary) mount.appendChild(buildDiaryCard(hit));
  }
  if (data.people.length > 0) {
    mount.appendChild(buildSectionHeader('people'));
    for (const hit of data.people) mount.appendChild(buildPersonCard(hit));
  }
}

function buildSectionHeader(label: string): HTMLElement {
  const el = document.createElement('div');
  el.className = 'list-section-header';
  el.textContent = label;
  return el;
}

function buildWikiCard(hit: SearchWikiHit): HTMLElement {
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

  card.addEventListener('click', () => navigate({ name: 'wikiPage', path: hit.path }));
  return card;
}

function buildDiaryCard(hit: SearchDiaryHit): HTMLElement {
  // Reuses the diary-entry layout from the old timeline view so the
  // search hit reads identically to a "real" diary entry.
  const wrap = document.createElement('div');
  wrap.className = 'diary-entry';

  const meta = document.createElement('div');
  meta.className = 'diary-entry-meta';
  const when = hit.at && hit.at > 0 ? formatDiaryWhen(hit.at, hit.header) : `${hit.file} · ${hit.header}`;
  meta.textContent = when;
  wrap.appendChild(meta);

  const body = document.createElement('div');
  body.className = 'diary-entry-body';
  body.textContent = hit.content;
  wrap.appendChild(body);

  return wrap;
}

function buildPersonCard(p: SearchPersonHit): HTMLElement {
  const card = document.createElement('button');
  card.type = 'button';
  card.className = 'person-card';
  card.addEventListener('click', () => navigate({ name: 'personDetail', email: p.email }));

  const top = document.createElement('div');
  top.className = 'person-card-top';

  const ident = document.createElement('div');
  ident.className = 'person-card-ident';

  const name = document.createElement('div');
  name.className = 'person-card-name';
  name.textContent = p.name || p.email;
  ident.appendChild(name);

  if (p.name && p.name !== p.email) {
    const emailEl = document.createElement('div');
    emailEl.className = 'person-card-email';
    emailEl.textContent = p.email;
    ident.appendChild(emailEl);
  }
  top.appendChild(ident);

  const badge = document.createElement('span');
  badge.className = 'person-card-count';
  badge.textContent = `${p.messageCount.toLocaleString('ko-KR')}건`;
  top.appendChild(badge);
  card.appendChild(top);

  if (p.lastSubject) {
    const sub = document.createElement('div');
    sub.className = 'person-card-subject';
    sub.textContent = p.lastSubject;
    card.appendChild(sub);
  }

  if (p.lastSeen) {
    const foot = document.createElement('div');
    foot.className = 'person-card-foot';
    foot.textContent = `마지막 ${relativeTime(p.lastSeen)}`;
    card.appendChild(foot);
  }

  return card;
}

const DAY_NAMES = ['일', '월', '화', '수', '목', '금', '토'];

function formatDiaryWhen(at: number, header: string): string {
  const d = new Date(at);
  if (Number.isNaN(d.getTime())) return header;
  const date = `${d.getMonth() + 1}월 ${d.getDate()}일 (${DAY_NAMES[d.getDay()]})`;
  return `${date} · ${header}`;
}
