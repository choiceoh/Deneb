// Pure display helpers — turn dynamic gateway payloads into readable strings for
// both the grids and the AI context projection. No React here.
import type { CalEvent, CalTimestamp } from "./types";

// Narrow loose gateway JSON to a primitive (or undefined) — shared so callers
// stop re-inlining `typeof v === "string" ? v : undefined` per field.
export const asStr = (v: unknown): string | undefined => (typeof v === "string" ? v : undefined);
export const asNum = (v: unknown): number | undefined => (typeof v === "number" ? v : undefined);
export const asBool = (v: unknown): boolean => v === true;

// First NON-empty (trimmed) string among `keys` of an object; "" if none. Picking
// the first non-empty field (not `??`) lets `{ name: "", email }` surface the email.
export function firstString(obj: unknown, keys: string[]): string {
  if (!obj || typeof obj !== "object") return "";
  const o = obj as Record<string, unknown>;
  for (const k of keys) {
    const v = o[k];
    if (typeof v === "string" && v.trim()) return v;
  }
  return "";
}

// Coerce a possibly-structured field (e.g. mail `from`) to a short label.
export function text(v: unknown): string {
  if (v == null) return "";
  if (typeof v === "string") return v;
  if (typeof v === "object") return firstString(v, ["name", "email", "title"]);
  return String(v);
}

// A mail sender's display NAME with the address dropped — "홍길동 <a@b.com>" → "홍길동".
// Accepts the gateway's "Name <addr>" string or the legacy { name, email } object.
// When there's no display name, falls back to the bare address (matching Gmail) so a
// nameless sender still renders something rather than an empty cell.
export function senderName(v: unknown): string {
  if (v && typeof v === "object") return firstString(v, ["name", "email", "title"]);
  const raw = text(v).trim();
  const m = /^(.*)<([^>]*)>\s*$/.exec(raw); // "Name <addr>" → [name, addr]
  const name = (m ? m[1] : raw)
    .trim()
    .replace(/^"(.*)"$/, "$1")
    .trim(); // unquote "Last, First"
  return name || (m ? m[2].trim() : raw);
}

// Render a timestamp compactly. Accepts an ISO-ish string OR epoch milliseconds
// (the gateway uses both: RFC3339 for dates, `*AtMs` numbers for cron/workfeed).
// Passes through anything unparseable.
export function fmtDate(v?: string | number): string {
  if (v == null || v === "") return "";
  const d = new Date(v);
  if (Number.isNaN(d.getTime())) return String(v);
  return d.toLocaleString(undefined, {
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
    hour12: false,
  });
}

// Mail timestamps within the last 6 hours read relatively ("3시간 전" / "45분 전" /
// "방금"); older or future ones fall back to the absolute fmtDate. `now` is injectable
// for deterministic tests.
export function fmtMailDate(v?: string | number, now = Date.now()): string {
  if (v == null || v === "") return "";
  const d = new Date(v);
  if (Number.isNaN(d.getTime())) return String(v);
  const diff = now - d.getTime();
  if (diff >= 0 && diff < 6 * 60 * 60 * 1000) {
    const mins = Math.floor(diff / 60_000);
    if (mins < 1) return "방금";
    if (mins < 60) return `${mins}분 전`;
    return `${Math.floor(mins / 60)}시간 전`;
  }
  return fmtDate(v);
}

// A calendar timestamp is a bare ISO string or a { dateTime } / { date } object.
// `date` (no time) marks an all-day event, which must NOT go through new Date()'s
// UTC-midnight parsing or it shifts a day in western zones.
export function calStamp(v: CalTimestamp | undefined): { iso?: string; allDay: boolean } {
  if (typeof v === "string") return { iso: v, allDay: /^\d{4}-\d{2}-\d{2}$/.test(v) };
  if (v && typeof v === "object") {
    if (typeof v.dateTime === "string") return { iso: v.dateTime, allDay: false };
    if (typeof v.date === "string") return { iso: v.date, allDay: true };
  }
  return { allDay: false };
}

// Format an all-day YYYY-MM-DD in LOCAL terms (date only, no UTC shift, no time).
// `offsetDays` lets callers step back Google's exclusive all-day end.date.
function fmtDay(ymd: string, offsetDays = 0): string {
  const m = /^(\d{4})-(\d{2})-(\d{2})/.exec(ymd);
  if (!m) return fmtDate(ymd);
  const d = new Date(Number(m[1]), Number(m[2]) - 1, Number(m[3]) + offsetDays);
  return d.toLocaleDateString(undefined, { month: "short", day: "numeric" });
}

// Human span for a calendar event. All-day events render date-only with the
// exclusive end stepped back to the last inclusive day; timed events show time.
export function calSpan(start: CalTimestamp | undefined, end: CalTimestamp | undefined): string {
  const s = calStamp(start);
  if (s.allDay && s.iso) {
    const e = calStamp(end);
    const startDay = fmtDay(s.iso);
    const endDay = e.iso ? fmtDay(e.iso, -1) : ""; // end.date is exclusive → -1 day
    return !endDay || endDay === startDay ? startDay : `${startDay} ~ ${endDay}`;
  }
  const e = calStamp(end);
  return [s.iso ? fmtDate(s.iso) : "", e.iso ? fmtDate(e.iso) : ""].filter(Boolean).join(" ~ ");
}

// ── Month-grid helpers (the 일정 calendar view) ───────────────────────────────

// Event display name. Gateway sends `summary`; `title` is a legacy alias.
export function eventTitle(ev: CalEvent): string {
  return ev.summary ?? ev.title ?? "(제목 없음)";
}

// Local HH:MM for a timed stamp; "" for all-day or unparseable (chips stay terse).
export function hhmm(v: CalTimestamp | undefined): string {
  const s = calStamp(v);
  if (!s.iso || s.allDay) return "";
  const d = new Date(s.iso);
  return Number.isNaN(d.getTime())
    ? ""
    : d.toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit", hour12: false });
}

// Localized "year month" heading, e.g. "2026년 6월" / "June 2026". month0 is 0-based.
export function monthLabel(year: number, month0: number): string {
  return new Date(year, month0, 1).toLocaleDateString(undefined, { year: "numeric", month: "long" });
}

// Canonical local day key ("YYYY-M-D", not zero-padded) for map lookups — never shown.
export function dayKey(d: Date): string {
  return `${d.getFullYear()}-${d.getMonth() + 1}-${d.getDate()}`;
}

// A Date at local midnight from a calendar stamp. All-day YYYY-MM-DD is parsed
// component-wise (no UTC shift, matching fmtDay); timed values floor to their local day.
function dayStart(iso: string, allDay: boolean): Date {
  const m = /^(\d{4})-(\d{2})-(\d{2})/.exec(iso);
  if (allDay && m) return new Date(Number(m[1]), Number(m[2]) - 1, Number(m[3]));
  const d = new Date(iso);
  return new Date(d.getFullYear(), d.getMonth(), d.getDate());
}

// Local day keys an event covers (start..end inclusive), so a multi-day event
// lands on every cell it spans. All-day end.date is exclusive (Google) → stepped
// back a day; a missing end is a single day. Capped so a malformed end can't spin.
export function eventDayKeys(start: CalTimestamp | undefined, end: CalTimestamp | undefined): string[] {
  const s = calStamp(start);
  if (!s.iso) return [];
  const from = dayStart(s.iso, s.allDay);
  const e = calStamp(end);
  let to = from;
  if (e.iso) {
    to = dayStart(e.iso, e.allDay);
    if (e.allDay) to = new Date(to.getFullYear(), to.getMonth(), to.getDate() - 1);
  }
  if (to.getTime() < from.getTime()) to = from;
  const keys: string[] = [];
  const cur = new Date(from);
  for (let i = 0; i < 62 && cur.getTime() <= to.getTime(); i++) {
    keys.push(dayKey(cur));
    cur.setDate(cur.getDate() + 1);
  }
  return keys;
}

// The local instant (epoch ms) an event is OVER, or null if it carries no usable
// time. Lets the calendar drop already-ended events from the upcoming list.
// All-day end.date is exclusive (Google) so it already marks the over-instant; an
// all-day event with only a start is over at the next local midnight.
export function eventEndMs(start: CalTimestamp | undefined, end: CalTimestamp | undefined): number | null {
  const e = calStamp(end);
  if (e.iso) {
    if (e.allDay) return dayStart(e.iso, true).getTime();
    const t = new Date(e.iso).getTime();
    if (!Number.isNaN(t)) return t;
  }
  const s = calStamp(start);
  if (s.iso) {
    if (s.allDay) {
      const d = dayStart(s.iso, true);
      return new Date(d.getFullYear(), d.getMonth(), d.getDate() + 1).getTime();
    }
    const t = new Date(s.iso).getTime();
    if (!Number.isNaN(t)) return t;
  }
  return null;
}

// Sunday-first weeks of Date cells covering month0/year, with the leading and
// trailing days needed to fill whole weeks (4–6 rows). month0 is 0-based.
export function monthMatrix(year: number, month0: number): Date[][] {
  const lead = new Date(year, month0, 1).getDay(); // 0 = Sunday
  const daysInMonth = new Date(year, month0 + 1, 0).getDate();
  const weeks = Math.ceil((lead + daysInMonth) / 7);
  const cur = new Date(year, month0, 1 - lead);
  const out: Date[][] = [];
  for (let w = 0; w < weeks; w++) {
    const row: Date[] = [];
    for (let d = 0; d < 7; d++) {
      row.push(new Date(cur));
      cur.setDate(cur.getDate() + 1);
    }
    out.push(row);
  }
  return out;
}

// Best-effort message from a Refine/HttpError (plain object with `message`) or any throwable.
export function errText(e: unknown): string {
  if (!e) return "알 수 없는 오류";
  if (typeof e === "string") return e;
  if (typeof e === "object" && "message" in e) return String((e as { message: unknown }).message);
  return String(e);
}
