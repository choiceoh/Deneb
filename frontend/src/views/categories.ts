// views/categories.ts — Wiki category explorer.
//
// Entry point for "쟁여둔 컨텍스트 둘러보기" — the user knows there's
// stuff in memory but doesn't have a search query in mind. Each row is
// one wiki category with its page count; tap to see the pages inside.

import { listCategories, type MemoryCategory } from '../memory';
import { formatRpcError } from '../format';
import { isCurrentHash, navigate } from '../router';
import { buildErrorBanner, buildLoadingNode, buildViewHeader } from './ui';

export async function renderCategories(
  root: HTMLElement,
  initData: string,
): Promise<void> {
  const expectedHash = location.hash;
  root.innerHTML = '';

  root.appendChild(
    buildViewHeader({
      title: '📂 카테고리',
      left: { label: '← 더보기', onClick: () => navigate({ name: 'more' }) },
    }),
  );

  const status = buildLoadingNode('카테고리 불러오는 중…');
  root.appendChild(status);

  try {
    const { categories, totalPages } = await listCategories(initData);
    if (!isCurrentHash(expectedHash)) return;
    status.remove();

    const summary = document.createElement('div');
    summary.className = 'muted category-summary';
    summary.textContent = `총 ${totalPages.toLocaleString('ko-KR')} 페이지 · ${categories.length} 카테고리`;
    root.appendChild(summary);

    if (categories.length === 0) {
      const empty = document.createElement('div');
      empty.className = 'empty-state';
      empty.textContent = '카테고리가 비어있습니다';
      root.appendChild(empty);
      return;
    }
    for (const cat of categories) {
      root.appendChild(buildCategoryRow(cat));
    }
  } catch (err) {
    if (!isCurrentHash(expectedHash)) return;
    status.remove();
    root.appendChild(buildErrorBanner(`카테고리 로드 실패: ${formatRpcError(err)}`));
  }
}

function buildCategoryRow(cat: MemoryCategory): HTMLElement {
  const btn = document.createElement('button');
  btn.type = 'button';
  btn.className = 'category-row';

  const name = document.createElement('span');
  name.className = 'category-row-name';
  name.textContent = cat.name === '(root)' ? '(루트)' : cat.name;
  btn.appendChild(name);

  const count = document.createElement('span');
  count.className = 'category-row-count';
  count.textContent = `${cat.pageCount.toLocaleString('ko-KR')}`;
  btn.appendChild(count);

  btn.addEventListener('click', () =>
    navigate({ name: 'categoryPages', category: cat.name }),
  );
  return btn;
}
