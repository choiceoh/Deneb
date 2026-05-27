// views/diary.ts — Recent diary entries timeline.
//
// Deneb writes a timestamped diary entry as part of normal operation
// (every meaningful event lands in the daily diary file). This view
// renders the most recent entries as a vertical timeline grouped by
// date, so the user can scan "what's been happening in my world."

import { recentDiary, type DiaryEntry } from '../memory';
import { formatRpcError } from '../format';
import { isCurrentHash, navigate } from '../router';
import { buildErrorBanner, buildLoadingNode, buildViewHeader } from './ui';

interface DiaryGroup {
  dateLabel: string; // e.g., "오늘 · 5월 26일 (월)"
  entries: DiaryEntry[];
}

const DAY_NAMES = ['일', '월', '화', '수', '목', '금', '토'];

export async function renderDiary(
  root: HTMLElement,
  initData: string,
): Promise<void> {
  const expectedHash = location.hash;
  root.innerHTML = '';

  root.appendChild(
    buildViewHeader({
      title: 'diary',
      left: { label: '← home', onClick: () => navigate({ name: 'home' }) },
      right: { label: 'refresh', onClick: () => void renderDiary(root, initData) },
    }),
  );

  const status = buildLoadingNode('다이어리 불러오는 중…');
  root.appendChild(status);

  try {
    const { entries } = await recentDiary(initData);
    if (!isCurrentHash(expectedHash)) return;
    status.remove();

    if (entries.length === 0) {
      const empty = document.createElement('div');
      empty.className = 'empty-state';
      empty.textContent = '다이어리에 기록이 없습니다';
      root.appendChild(empty);
      return;
    }

    for (const group of groupByDay(entries)) {
      root.appendChild(buildDayHeader(group.dateLabel));
      for (const e of group.entries) {
        root.appendChild(buildEntryBubble(e));
      }
    }
  } catch (err) {
    if (!isCurrentHash(expectedHash)) return;
    status.remove();
    root.appendChild(buildErrorBanner(`다이어리 로드 실패: ${formatRpcError(err)}`));
  }
}

// groupByDay buckets entries by local YYYY-MM-DD so groups match what
// the user sees on the wall clock. Entries arrive already sorted
// newest-first from the backend, so within each group we just preserve
// order.
function groupByDay(entries: DiaryEntry[]): DiaryGroup[] {
  const buckets = new Map<string, DiaryGroup>();
  for (const e of entries) {
    const key = dayKey(e);
    const label = formatDayLabel(e);
    if (!buckets.has(key)) buckets.set(key, { dateLabel: label, entries: [] });
    buckets.get(key)!.entries.push(e);
  }
  return Array.from(buckets.values());
}

function dayKey(e: DiaryEntry): string {
  if (e.at && e.at > 0) {
    const d = new Date(e.at);
    return localKey(d);
  }
  // Fallback: parse from filename "diary-YYYY-MM-DD.md".
  const m = e.file.match(/diary-(\d{4})-(\d{2})-(\d{2})\.md/);
  if (m) return `${m[1]}-${m[2]}-${m[3]}`;
  return e.file; // worst case: each unparseable filename buckets alone
}

function localKey(d: Date): string {
  const y = d.getFullYear();
  const m = String(d.getMonth() + 1).padStart(2, '0');
  const day = String(d.getDate()).padStart(2, '0');
  return `${y}-${m}-${day}`;
}

function formatDayLabel(e: DiaryEntry): string {
  let date: Date | null = null;
  if (e.at && e.at > 0) date = new Date(e.at);
  else {
    const m = e.file.match(/diary-(\d{4})-(\d{2})-(\d{2})\.md/);
    if (m) date = new Date(Number(m[1]), Number(m[2]) - 1, Number(m[3]));
  }
  if (!date || Number.isNaN(date.getTime())) return e.file;

  const today = new Date();
  const todayKey = localKey(today);
  const tomorrow = new Date(today.getTime() + 24 * 60 * 60 * 1000);
  const yesterday = new Date(today.getTime() - 24 * 60 * 60 * 1000);
  const key = localKey(date);

  const prefix =
    key === todayKey
      ? '오늘'
      : key === localKey(yesterday)
        ? '어제'
        : key === localKey(tomorrow)
          ? '내일'
          : '';
  const formatted = `${date.getMonth() + 1}월 ${date.getDate()}일 (${DAY_NAMES[date.getDay()]})`;
  return prefix ? `${prefix} · ${formatted}` : formatted;
}

function buildDayHeader(label: string): HTMLElement {
  const el = document.createElement('div');
  el.className = 'list-section-header';
  el.textContent = label;
  return el;
}

function buildEntryBubble(e: DiaryEntry): HTMLElement {
  const wrap = document.createElement('div');
  wrap.className = 'diary-entry';

  const meta = document.createElement('div');
  meta.className = 'diary-entry-meta';
  meta.textContent = e.header || '—';
  wrap.appendChild(meta);

  const body = document.createElement('div');
  body.className = 'diary-entry-body';
  body.textContent = e.content;
  wrap.appendChild(body);

  return wrap;
}
