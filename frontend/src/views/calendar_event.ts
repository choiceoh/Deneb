// views/calendar_event.ts — Calendar event detail.
//
// Shows the full event metadata (time, location, description, attendees,
// conference link) and a "Gmail에서 관련 메일 보기" link that searches
// Gmail by counterpart email so the user can pull recent context with
// one tap. Person-card join and unresolved-promise extraction land in
// a follow-up PR once Phase 0 (structured analysis storage) is in.

import { calendarGet, type CalendarEventDetail, RpcError } from '../rpc';
import { isCurrentHash, navigate } from '../router';

export async function renderCalendarEvent(
  root: HTMLElement,
  initData: string,
  eventId: string,
): Promise<void> {
  const expectedHash = location.hash;
  root.innerHTML = '<div class="loading">일정 불러오는 중…</div>';

  try {
    const ev = await calendarGet(initData, eventId);
    if (!isCurrentHash(expectedHash)) return;
    paint(root, initData, ev);
  } catch (err) {
    if (!isCurrentHash(expectedHash)) return;
    const msg =
      err instanceof RpcError
        ? `${err.code} — ${err.message}`
        : err instanceof Error
          ? err.message
          : '알 수 없는 오류';
    root.innerHTML = '';
    const banner = document.createElement('div');
    banner.className = 'error';
    banner.textContent = `일정 로드 실패: ${msg}`;
    root.appendChild(banner);
  }
}

function paint(root: HTMLElement, _initData: string, ev: CalendarEventDetail): void {
  root.innerHTML = '';

  const h1 = document.createElement('h1');
  h1.textContent = ev.summary || '(제목 없음)';
  root.appendChild(h1);

  // Time block — when does this happen?
  const meta = document.createElement('div');
  meta.className = 'card';
  appendRow(meta, '시작', formatFull(ev.start, ev.allDay));
  appendRow(meta, '끝', formatFull(ev.end, ev.allDay));
  if (ev.location) appendRow(meta, '장소', ev.location);
  if (ev.status && ev.status !== 'confirmed') appendRow(meta, '상태', ev.status);
  root.appendChild(meta);

  if (ev.conference?.uri) {
    const join = document.createElement('a');
    join.className = 'primary';
    join.textContent = '🔗 화상회의 참여';
    join.href = ev.conference.uri;
    join.target = '_blank';
    join.rel = 'noopener noreferrer';
    root.appendChild(join);
  }

  if (ev.description?.trim()) {
    const desc = document.createElement('div');
    desc.className = 'card';
    const label = document.createElement('div');
    label.className = 'card-label';
    label.textContent = '설명';
    desc.appendChild(label);
    const body = document.createElement('div');
    body.className = 'event-description';
    body.textContent = ev.description;
    desc.appendChild(body);
    root.appendChild(desc);
  }

  const counterparts = (ev.attendees ?? []).filter((a) => !a.self);
  if (counterparts.length > 0 || ev.organizer) {
    const peopleCard = document.createElement('div');
    peopleCard.className = 'card';
    const label = document.createElement('div');
    label.className = 'card-label';
    label.textContent = '참석자';
    peopleCard.appendChild(label);
    if (ev.organizer && !ev.organizer.self) {
      peopleCard.appendChild(buildAttendeeRow(ev.organizer, true));
    }
    for (const a of counterparts) {
      if (ev.organizer && a.email === ev.organizer.email) continue;
      peopleCard.appendChild(buildAttendeeRow(a, false));
    }
    root.appendChild(peopleCard);
  }

  if (ev.htmlLink) {
    const a = document.createElement('a');
    a.className = 'link-button';
    a.textContent = '🗓 Google Calendar에서 열기';
    a.href = ev.htmlLink;
    a.target = '_blank';
    a.rel = 'noopener noreferrer';
    root.appendChild(a);
  }

  const back = document.createElement('button');
  back.className = 'link-button';
  back.textContent = '← 일정 목록';
  back.addEventListener('click', () => navigate({ name: 'calendar' }));
  root.appendChild(back);
}

function appendRow(card: HTMLElement, label: string, value: string): void {
  if (!value) return;
  const row = document.createElement('div');
  row.className = 'row';
  const l = document.createElement('span');
  l.className = 'label';
  l.textContent = label;
  const v = document.createElement('span');
  v.className = 'value';
  v.textContent = value;
  row.appendChild(l);
  row.appendChild(v);
  card.appendChild(row);
}

function buildAttendeeRow(
  a: { displayName?: string; email?: string; responseStatus?: string; organizer?: boolean },
  isOrganizer: boolean,
): HTMLElement {
  const row = document.createElement('div');
  row.className = 'attendee-row';
  const name = (a.displayName || a.email || '').trim();
  const label = document.createElement('span');
  label.className = 'attendee-name';
  label.textContent = isOrganizer ? `${name} (주최)` : name;
  row.appendChild(label);
  const status = document.createElement('span');
  status.className = 'attendee-status';
  status.textContent = formatRSVP(a.responseStatus);
  row.appendChild(status);
  return row;
}

function formatRSVP(s?: string): string {
  switch (s) {
    case 'accepted':
      return '✓ 수락';
    case 'declined':
      return '✗ 거절';
    case 'tentative':
      return '? 미정';
    case 'needsAction':
    case undefined:
    case '':
      return '대기';
    default:
      return s;
  }
}

function formatFull(iso: string, allDay?: boolean): string {
  if (!iso) return '';
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  if (allDay) return d.toLocaleDateString('ko-KR');
  return d.toLocaleString('ko-KR', {
    year: 'numeric',
    month: 'long',
    day: 'numeric',
    weekday: 'short',
    hour: '2-digit',
    minute: '2-digit',
  });
}
