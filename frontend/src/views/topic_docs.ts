// views/topic_docs.ts — list the per-topic knowledge files under
// <workspace>/topics/*.md. Tap a row to edit; header "+ 새 파일" creates one.
// Auto-scans the folder (no config needed); an empty folder shows a hint.

import { listTopicFiles } from '../topicdocs';
import type { TopicFile } from '../topicdocs';
import { formatRpcError } from '../format';
import { isCurrentHash, navigate } from '../router';
import { buildViewHeader, buildLoadingNode, renderErrorView } from './ui';

export async function renderTopicDocs(root: HTMLElement, initData: string): Promise<void> {
  const expectedHash = location.hash;
  root.innerHTML = '';
  root.appendChild(header());
  root.appendChild(buildLoadingNode('불러오는 중…'));

  try {
    const { files } = await listTopicFiles(initData);
    if (!isCurrentHash(expectedHash) || !root.isConnected) return;
    paint(root, files);
  } catch (err) {
    if (!isCurrentHash(expectedHash) || !root.isConnected) return;
    renderErrorView(root, `토픽 파일 목록 로드 실패: ${formatRpcError(err)}`, {
      label: '← back',
      onClick: () => history.back(),
    });
  }
}

function header(): HTMLElement {
  return buildViewHeader({
    title: 'topic docs',
    left: { label: '← back', onClick: () => history.back() },
    right: { label: '+ 새 파일', onClick: () => navigate({ name: 'topicDocNew' }) },
  });
}

function paint(root: HTMLElement, files: TopicFile[]): void {
  root.innerHTML = '';
  root.appendChild(header());

  if (files.length === 0) {
    const empty = document.createElement('div');
    empty.className = 'muted';
    empty.textContent = '토픽 파일이 없습니다. "+ 새 파일"로 시작하세요.';
    root.appendChild(empty);
    return;
  }

  const list = document.createElement('div');
  list.className = 'topicdoc-list';
  for (const f of files) {
    const row = document.createElement('button');
    row.type = 'button';
    row.className = 'card topicdoc-row';

    const name = document.createElement('div');
    name.className = 'topicdoc-name';
    name.textContent = f.name;
    row.appendChild(name);

    const meta = document.createElement('div');
    meta.className = 'muted topicdoc-meta';
    meta.textContent = [formatSize(f.size), formatDate(f.modified)].filter(Boolean).join(' · ');
    row.appendChild(meta);

    row.addEventListener('click', () => navigate({ name: 'topicDocEdit', file: f.name }));
    list.appendChild(row);
  }
  root.appendChild(list);
}

function formatSize(bytes: number): string {
  if (!bytes) return '';
  if (bytes < 1024) return `${bytes} B`;
  return `${(bytes / 1024).toFixed(1)} KB`;
}

function formatDate(iso: string): string {
  if (!iso) return '';
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return '';
  return d.toLocaleString('ko-KR', { dateStyle: 'short', timeStyle: 'short' });
}
