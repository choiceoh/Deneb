// Pure display helpers — turn dynamic gateway payloads into readable strings for
// both the grids and the AI context projection. No React here.
import type { CalTimestamp } from "./types";

// Coerce a possibly-structured field (e.g. mail `from`) to a short label. `??`
// would keep an empty-string `name` and hide a present email — so pick the first
// NON-empty field, letting `{ name: "", email }` still surface the email.
export function text(v: unknown): string {
  if (v == null) return "";
  if (typeof v === "string") return v;
  if (typeof v === "object") {
    const o = v as Record<string, unknown>;
    for (const k of ["name", "email", "title"]) {
      const s = o[k];
      if (typeof s === "string" && s.trim()) return s;
    }
    return "";
  }
  return String(v);
}

// Render an ISO-ish timestamp compactly; pass through anything unparseable.
export function fmtDate(v?: string): string {
  if (!v) return "";
  const d = new Date(v);
  if (Number.isNaN(d.getTime())) return v;
  return d.toLocaleString(undefined, { month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" });
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

// Best-effort message from a Refine/HttpError (plain object with `message`) or any throwable.
export function errText(e: unknown): string {
  if (!e) return "알 수 없는 오류";
  if (typeof e === "string") return e;
  if (typeof e === "object" && "message" in e) return String((e as { message: unknown }).message);
  return String(e);
}
