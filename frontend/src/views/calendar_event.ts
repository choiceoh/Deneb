// views/calendar_event.ts — Calendar event detail.
//
// Shows the full event metadata (time, location, description, attendees,
// conference link) plus a "관련 메일" section that searches Gmail for
// recent threads involving any non-self attendee, so the user gets the
// "when + with whom + what we last talked about" context in one screen
// — the chief-of-staff promise made concrete.
//
// Related-mail loading happens AFTER the main detail paints (independent
// fetch) so a slow Gmail call doesn't block the calendar view. Gmail
// errors render an inline notice instead of blowing away the page.

import { calendarGet, type CalendarEventDetail, type CalendarAttendee, RpcError } from '../rpc';
import { listRecent, type GmailMessageRow } from '../gmail';
import { isCurrentHash, navigate } from '../router';
import { relativeTime, shortFrom } from '../format';

const RELATED_MAIL_LIMIT = 5;
const RELATED_MAIL_WINDOW_DAYS = 90;

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
    paint(root, initData, ev, expectedHash);
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

function paint(
  root: HTMLElement,
  initData: string,
  ev: CalendarEventDetail,
  expectedHash: string,
): void {
  root.innerHTML = '';

  const h1 = document.createElement('h1');
  h1.textContent = ev.summary || '(제목 없음)';
  root.appendChild(h1);

  // Time block — when does this happen?
  // Google Calendar all-day events use an EXCLUSIVE end date (a one-
  // day event has end = start + 1day); subtract one day when rendering
  // so a "May 26" event shows "끝: 5월 26일" instead of "5월 27일".
  const meta = document.createElement('div');
  meta.className = 'card';
  appendRow(meta, '시작', formatFull(ev.start, ev.allDay));
  appendRow(meta, '끝', formatFull(ev.end, ev.allDay, /* allDayEndExclusive */ true));
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

  // Related mail — kicked off async after the main detail is painted.
  // The placeholder card is inserted now so the section position is
  // stable even if Gmail loads slowly; the fetch fills it in.
  const counterpartEmails = collectCounterpartEmails(ev);
  if (counterpartEmails.length > 0) {
    const relatedCard = buildRelatedMailPlaceholder();
    root.appendChild(relatedCard);
    void loadRelatedMail(relatedCard, initData, counterpartEmails, expectedHash);
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

// collectCounterpartEmails returns lowercased emails of non-self
// attendees. Dedupes so an organizer who is also in attendees doesn't
// double the Gmail OR-clause.
function collectCounterpartEmails(ev: CalendarEventDetail): string[] {
  const seen = new Set<string>();
  const add = (a?: CalendarAttendee): void => {
    if (!a || a.self) return;
    const email = (a.email ?? '').trim().toLowerCase();
    if (!email) return;
    seen.add(email);
  };
  add(ev.organizer);
  for (const a of ev.attendees ?? []) add(a);
  return Array.from(seen);
}

function buildRelatedMailPlaceholder(): HTMLDivElement {
  const card = document.createElement('div');
  card.className = 'card';
  const label = document.createElement('div');
  label.className = 'card-label';
  label.textContent = '관련 메일';
  card.appendChild(label);
  const status = document.createElement('div');
  status.className = 'muted';
  status.textContent = 'Gmail에서 검색 중…';
  card.appendChild(status);
  return card;
}

async function loadRelatedMail(
  card: HTMLDivElement,
  initData: string,
  emails: string[],
  expectedHash: string,
): Promise<void> {
  const query = buildGmailQuery(emails);
  try {
    const { messages } = await listRecent(initData, {
      query,
      limit: RELATED_MAIL_LIMIT,
    });
    if (!isCurrentHash(expectedHash)) return;
    renderRelatedMail(card, messages);
  } catch (err) {
    if (!isCurrentHash(expectedHash)) return;
    renderRelatedMailError(card, err);
  }
}

// buildGmailQuery joins emails into a Gmail (from:|to:) OR-clause with
// a 90-day window. Gmail's query parser handles parentheses around the
// OR-group fine; we cap to a reasonable number of clauses so a 40-
// person mailing list invite doesn't blow the query length.
function buildGmailQuery(emails: string[]): string {
  const MAX_CLAUSES = 8;
  const picks = emails.slice(0, MAX_CLAUSES);
  const orClause = picks.map((e) => `from:${e} OR to:${e}`).join(' OR ');
  return `(${orClause}) newer_than:${RELATED_MAIL_WINDOW_DAYS}d`;
}

function renderRelatedMail(card: HTMLDivElement, messages: GmailMessageRow[]): void {
  // Drop the "Gmail에서 검색 중…" placeholder, keep the label.
  const status = card.querySelector('.muted');
  if (status) status.remove();

  if (messages.length === 0) {
    const empty = document.createElement('div');
    empty.className = 'muted';
    empty.textContent = '최근 90일간 관련 메일 없음';
    card.appendChild(empty);
    return;
  }

  for (const m of messages) {
    card.appendChild(buildMailRow(m));
  }
}

function renderRelatedMailError(card: HTMLDivElement, err: unknown): void {
  const status = card.querySelector('.muted');
  if (status) status.remove();
  const notice = document.createElement('div');
  notice.className = 'muted';
  if (err instanceof RpcError && err.code === 'UNAVAILABLE') {
    notice.textContent = 'Gmail이 연결되지 않아 관련 메일을 가져올 수 없습니다';
  } else {
    const msg =
      err instanceof RpcError
        ? `${err.code} — ${err.message}`
        : err instanceof Error
          ? err.message
          : '알 수 없는 오류';
    notice.textContent = `관련 메일 로드 실패: ${msg}`;
  }
  card.appendChild(notice);
}

function buildMailRow(m: GmailMessageRow): HTMLElement {
  const row = document.createElement('button');
  row.className = 'related-mail-row';

  const top = document.createElement('div');
  top.className = 'related-mail-top';
  const subj = document.createElement('span');
  subj.className = 'related-mail-subject';
  subj.textContent = m.subject || '(제목 없음)';
  if (m.isUnread) subj.classList.add('unread');
  top.appendChild(subj);
  const when = document.createElement('span');
  when.className = 'related-mail-time';
  when.textContent = relativeTime(m.date);
  top.appendChild(when);
  row.appendChild(top);

  const meta = document.createElement('div');
  meta.className = 'related-mail-from';
  meta.textContent = shortFrom(m.from);
  row.appendChild(meta);

  if (m.snippet) {
    const snip = document.createElement('div');
    snip.className = 'related-mail-snippet';
    snip.textContent = m.snippet;
    row.appendChild(snip);
  }

  row.addEventListener('click', () => navigate({ name: 'detail', messageId: m.id }));
  return row;
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

function formatFull(iso: string, allDay?: boolean, allDayEndExclusive?: boolean): string {
  if (!iso) return '';
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  if (allDay) {
    // Google Calendar all-day ends are exclusive (point to the day
    // AFTER the last covered day); for display, subtract one day so
    // a one-day event shows the same start and end date.
    const display = allDayEndExclusive ? new Date(d.getTime() - 24 * 60 * 60 * 1000) : d;
    return display.toLocaleDateString('ko-KR');
  }
  return d.toLocaleString('ko-KR', {
    year: 'numeric',
    month: 'long',
    day: 'numeric',
    weekday: 'short',
    hour: '2-digit',
    minute: '2-digit',
  });
}
