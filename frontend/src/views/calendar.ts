// views/calendar.ts — Upcoming Google Calendar events.
//
// Read-only list of events in the next ~7 days, grouped by local day so
// the user can scan "오늘 / 내일 / 모레" at a glance. Tapping a row
// opens the event detail view.

import { calendarListUpcoming, type CalendarEventSummary, RpcError } from '../rpc';
import { formatRpcError } from '../format';
import { isCurrentHash, navigate } from '../router';
import { buildErrorBanner, buildLoadingNode, buildViewHeader } from './ui';

const HOURS_AHEAD = 24 * 7;

export async function renderCalendar(root: HTMLElement, initData: string): Promise<void> {
  const expectedHash = location.hash;
  root.innerHTML = '';

  root.appendChild(
    buildViewHeader({
      title: 'calendar',
      right: { label: 'refresh', onClick: () => void renderCalendar(root, initData) },
    }),
  );

  const status = buildLoadingNode('일정 불러오는 중…');
  root.appendChild(status);

  try {
    const { events } = await calendarListUpcoming(initData, { hoursAhead: HOURS_AHEAD });
    if (!isCurrentHash(expectedHash)) return;
    status.remove();

    if (events.length === 0) {
      const empty = document.createElement('div');
      empty.className = 'empty-state';
      empty.textContent = 'nothing on the next 7 days';
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
    // UNAVAILABLE = the calendar provider isn't connected yet; phrase
    // it as a setup hint rather than a load failure.
    const msg = formatRpcError(err);
    const banner = buildErrorBanner(
      err instanceof RpcError && err.code === 'UNAVAILABLE'
        ? `캘린더가 아직 연결되지 않았습니다 (${msg})`
        : `일정 로드 실패: ${msg}`,
    );
    root.appendChild(banner);
  }
}

interface DayGroup {
  dayLabel: string;
  events: CalendarEventSummary[];
}

// groupByDay buckets events into LOCAL-day groups (not UTC) so the
// label and the key agree. Earlier versions used toISOString().slice
// for the key which produced a UTC date — events between 00:00–09:00
// KST landed in yesterday's bucket while their visible header read
// today's local M/D, and same-local-day events that crossed UTC
// midnight split into two groups.
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
    const key = localDateKey(d);
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

// localDateKey produces a YYYY-MM-DD string in the BROWSER's local
// timezone — matches what formatDayLabel renders. Must be used for any
// "is this today/tomorrow?" comparison.
function localDateKey(d: Date): string {
  const y = d.getFullYear();
  const m = String(d.getMonth() + 1).padStart(2, '0');
  const day = String(d.getDate()).padStart(2, '0');
  return `${y}-${m}-${day}`;
}

function formatDayLabel(d: Date): string {
  const today = new Date();
  const todayKey = localDateKey(today);
  const tomorrow = new Date(today.getTime() + 24 * 60 * 60 * 1000);
  const tomorrowKey = localDateKey(tomorrow);
  const key = localDateKey(d);

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
