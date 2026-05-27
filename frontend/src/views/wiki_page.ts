// views/wiki_page.ts — full wiki page render.
//
// Reached by tapping a memory search hit or (eventually) a sender-context
// chip. Shows frontmatter meta in a sidebar-ish row, then the Markdown
// body via the in-house renderer.

import { getPage, type MemoryPage } from '../memory';
import { formatRpcError } from '../format';
import { isCurrentHash, navigate } from '../router';
import { renderMarkdown } from '../markdown';
import { buildViewHeader, renderErrorView } from './ui';

export async function renderWikiPage(
  root: HTMLElement,
  initData: string,
  path: string,
): Promise<void> {
  const expectedHash = location.hash;
  root.innerHTML = '<div class="loading">위키 페이지 불러오는 중…</div>';

  try {
    const page = await getPage(initData, path);
    if (!isCurrentHash(expectedHash)) return;
    paint(root, page);
  } catch (err) {
    if (!isCurrentHash(expectedHash)) return;
    renderErrorView(root, `위키 페이지 로드 실패: ${formatRpcError(err)}`, {
      label: '← 메모리 검색으로',
      onClick: () => navigate({ name: 'memory' }),
    });
  }
}

function paint(root: HTMLElement, page: MemoryPage): void {
  root.innerHTML = '';

  root.appendChild(
    buildViewHeader({
      title: '위키',
      left: { label: '← 검색', onClick: () => navigate({ name: 'memory' }) },
    }),
  );

  const meta = document.createElement('div');
  meta.className = 'card wiki-meta';
  const title = document.createElement('div');
  title.className = 'wiki-title';
  title.textContent = page.title || page.path;
  meta.appendChild(title);
  if (page.summary) {
    const sub = document.createElement('div');
    sub.className = 'wiki-summary';
    sub.textContent = page.summary;
    meta.appendChild(sub);
  }
  const tagsLine = document.createElement('div');
  tagsLine.className = 'wiki-meta-tags';
  const tagParts: string[] = [];
  if (page.category) tagParts.push(`#${page.category}`);
  if (page.tags) tagParts.push(...page.tags.map((t) => `#${t}`));
  tagsLine.textContent = tagParts.join(' · ');
  if (tagsLine.textContent) meta.appendChild(tagsLine);

  const dateLine = document.createElement('div');
  dateLine.className = 'wiki-meta-dates';
  const dateParts: string[] = [];
  if (page.updated) dateParts.push(`갱신 ${page.updated}`);
  if (page.created) dateParts.push(`생성 ${page.created}`);
  if (page.due) dateParts.push(`📅 ${page.due}`);
  dateLine.textContent = dateParts.join(' · ');
  if (dateLine.textContent) meta.appendChild(dateLine);
  root.appendChild(meta);

  const body = document.createElement('div');
  body.className = 'card wiki-body';
  body.innerHTML = renderMarkdown(page.body || '(본문 없음)');
  root.appendChild(body);

  if (page.related && page.related.length > 0) {
    const related = document.createElement('div');
    related.className = 'card wiki-related';
    const label = document.createElement('div');
    label.className = 'wiki-related-label';
    label.textContent = '관련 페이지';
    related.appendChild(label);
    for (const r of page.related) {
      const chip = document.createElement('button');
      chip.className = 'wiki-related-chip';
      chip.textContent = r;
      chip.addEventListener('click', () => navigate({ name: 'wikiPage', path: r }));
      related.appendChild(chip);
    }
    root.appendChild(related);
  }

  const footer = document.createElement('div');
  footer.className = 'muted';
  footer.textContent = page.path;
  root.appendChild(footer);
}
