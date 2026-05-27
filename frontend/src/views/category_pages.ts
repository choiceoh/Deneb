// views/category_pages.ts — Pages within a single wiki category.
//
// Drilled into from the categories explorer. Each row shows title +
// summary + updated date; tap to open the full wiki page view.

import { listPagesInCategory, type MemoryPageRow } from '../memory';
import { formatRpcError, relativeTime } from '../format';
import { isCurrentHash, navigate } from '../router';
import { buildErrorBanner, buildLoadingNode, buildViewHeader } from './ui';

export async function renderCategoryPages(
  root: HTMLElement,
  initData: string,
  category: string,
): Promise<void> {
  const expectedHash = location.hash;
  root.innerHTML = '';

  const displayName = category === '(root)' ? 'root' : category;
  root.appendChild(
    buildViewHeader({
      title: displayName,
      left: { label: '← categories', onClick: () => navigate({ name: 'categories' }) },
    }),
  );

  const status = buildLoadingNode('페이지 목록 불러오는 중…');
  root.appendChild(status);

  try {
    const { pages, total } = await listPagesInCategory(initData, category);
    if (!isCurrentHash(expectedHash)) return;
    status.remove();

    if (pages.length === 0) {
      const empty = document.createElement('div');
      empty.className = 'empty-state';
      empty.textContent = '이 카테고리에 페이지가 없습니다';
      root.appendChild(empty);
      return;
    }
    for (const page of pages) {
      root.appendChild(buildPageRow(page));
    }
    if (total > pages.length) {
      const note = document.createElement('div');
      note.className = 'muted';
      note.textContent = `최근 ${pages.length}건 표시 · 전체 ${total.toLocaleString('ko-KR')}건`;
      root.appendChild(note);
    }
  } catch (err) {
    if (!isCurrentHash(expectedHash)) return;
    status.remove();
    root.appendChild(buildErrorBanner(`페이지 목록 로드 실패: ${formatRpcError(err)}`));
  }
}

function buildPageRow(page: MemoryPageRow): HTMLElement {
  const card = document.createElement('button');
  card.type = 'button';
  card.className = 'memory-card';

  const title = document.createElement('div');
  title.className = 'memory-title';
  title.textContent = page.title || page.path;
  card.appendChild(title);

  if (page.summary) {
    const summary = document.createElement('div');
    summary.className = 'memory-summary';
    summary.textContent = page.summary;
    card.appendChild(summary);
  }

  const meta = document.createElement('div');
  meta.className = 'memory-meta';
  const parts: string[] = [page.path];
  if (page.updated) parts.push(`갱신 ${relativeTime(page.updated)}`);
  meta.textContent = parts.join(' · ');
  card.appendChild(meta);

  card.addEventListener('click', () => navigate({ name: 'wikiPage', path: page.path }));
  return card;
}
