// views/calendar.ts — Upcoming Google Calendar events.
//
// Read-only list of events in the next ~7 days, grouped by local day so
// the user can scan "오늘 / 내일 / 모레" at a glance. Tapping a row
// opens the event detail view.

import { calendarListUpcoming, type CalendarEventSummary, RpcError } from '../rpc';
import { isCurrentHash, navigate } from '../router';

const HOURS_AHEAD = 24 * 7;

export async function renderCalendar(root: HTMLElement, initData: string): Promise<void> {
  const expectedHash = location.hash;
  root.innerHTML = '';

  const header = document.createElement('div');
  header.className = 'view-header';
  header.innerHTML = `
    <span class="view-title">📅 일정</span>
    <button class="link-button">새로고침</button>
  `;
  header.querySelector('button')!.addEventListener('click', () => {
    void renderCalendar(root, initData);
  });
  root.appendChild(header);

  const status = document.createElement('div');
  status.className = 'loading';
  status.textContent = '일정 불러오는 중…';
  root.appendChild(status);

  try {
    const { events } = await calendarListUpcoming(initData, { hoursAhead: HOURS_AHEAD });
    if (!isCurrentHash(expectedHash)) return;
    status.remove();

    if (events.length === 0) {
      const empty = document.createElement('div');
      empty.className = 'empty-state';
      empty.textContent = '향후 7일간 일정이 없습니다';
      root.appendChild(empty);
      return;
    }

    for (const group of groupByDay(events)) {
      root.appendChild(buildDayHeader(group.dayLabel));
      for (const ev of group.events) {
        root.appendChild(buildEventRow(ev));
      }
    }
  } catch (err) {
    if (!isCurrentHash(expectedHash)) return;
    status.remove();
    const msg =
      err instanceof RpcError
        ? `${err.code} — ${err.message}`
        : err instanceof Error
          ? err.message
          : '알 수 없는 오류';
    const banner = document.createElement('div');
    banner.className = 'error';
    banner.textContent =
      err instanceof RpcError && err.code === 'UNAVAILABLE'
        ? `캘린더가 아직 연결되지 않았습니다 (${msg})`
        : `일정 로드 실패: ${msg}`;
    root.appendChild(banner);
  }
}

interface DayGroup {
  dayLabel: string;
  events: CalendarEventSummary[];
}

// groupByDay buckets events into local-day groups for visual rhythm.
// Events without a valid start time fall into a "(시간 미상)" bucket at
// the end so the user still sees them.
function groupByDay(events: CalendarEventSummary[]): DayGroup[] {
  const buckets = new Map<string, DayGroup>();
  const unknown: DayGroup = { dayLabel: '(시간 미상)', events: [] };

  for (const ev of events) {
    if (!ev.start) {
      unknown.events.push(ev);
      continue;
    }
    const d = new Date(ev.start);
    if (Number.isNaN(d.getTime())) {
      unknown.events.push(ev);
      continue;
    }
    const key = d.toISOString().slice(0, 10);
    const label = formatDayLabel(d);
    if (!buckets.has(key)) buckets.set(key, { dayLabel: label, events: [] });
    buckets.get(key)!.events.push(ev);
  }

  const groups = Array.from(buckets.entries())
    .sort(([a], [b]) => (a < b ? -1 : a > b ? 1 : 0))
    .map(([, g]) => g);
  if (unknown.events.length > 0) groups.push(unknown);
  return groups;
}

const DAY_NAMES = ['일', '월', '화', '수', '목', '금', '토'];

function formatDayLabel(d: Date): string {
  const today = new Date();
  const todayKey = today.toISOString().slice(0, 10);
  const tomorrow = new Date(today.getTime() + 24 * 60 * 60 * 1000);
  const tomorrowKey = tomorrow.toISOString().slice(0, 10);
  const key = d.toISOString().slice(0, 10);

  const prefix = key === todayKey ? '오늘' : key === tomorrowKey ? '내일' : '';
  const date = `${d.getMonth() + 1}월 ${d.getDate()}일 (${DAY_NAMES[d.getDay()]})`;
  return prefix ? `${prefix} · ${date}` : date;
}

function buildDayHeader(label: string): HTMLElement {
  const el = document.createElement('div');
  el.className = 'list-section-header';
  el.textContent = label;
  return el;
}

function buildEventRow(ev: CalendarEventSummary): HTMLElement {
  const el = document.createElement('button');
  el.className = 'event-row';

  const top = document.createElement('div');
  top.className = 'event-row-top';
  const timeEl = document.createElement('span');
  timeEl.className = 'event-row-time';
  timeEl.textContent = ev.allDay ? '하루 종일' : formatTimeRange(ev.start, ev.end);
  top.appendChild(timeEl);

  const titleEl = document.createElement('span');
  titleEl.className = 'event-row-title';
  titleEl.textContent = ev.summary || '(제목 없음)';
  top.appendChild(titleEl);
  el.appendChild(top);

  const sub: string[] = [];
  if (ev.location) sub.push(`📍 ${ev.location}`);
  if (ev.hasMeet) sub.push('🔗 Meet');
  const counterparts = nonSelfAttendeeNames(ev.attendees ?? []);
  if (counterparts) sub.push(`👤 ${counterparts}`);
  if (sub.length > 0) {
    const subEl = document.createElement('div');
    subEl.className = 'event-row-sub';
    subEl.textContent = sub.join(' · ');
    el.appendChild(subEl);
  }

  el.addEventListener('click', () => navigate({ name: 'calendarEvent', eventId: ev.id }));
  return el;
}

function formatTimeRange(startISO: string, endISO: string): string {
  if (!startISO) return '';
  const start = new Date(startISO);
  if (Number.isNaN(start.getTime())) return '';
  const startStr = formatHHMM(start);
  if (!endISO) return startStr;
  const end = new Date(endISO);
  if (Number.isNaN(end.getTime())) return startStr;
  return `${startStr} – ${formatHHMM(end)}`;
}

function formatHHMM(d: Date): string {
  return `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`;
}

function nonSelfAttendeeNames(
  attendees: { displayName?: string; email?: string; self?: boolean }[],
): string {
  const picks: string[] = [];
  for (const a of attendees) {
    if (a.self) continue;
    const label = (a.displayName || a.email || '').trim();
    if (label) picks.push(label);
    if (picks.length >= 3) break;
  }
  return picks.join(' · ');
}
