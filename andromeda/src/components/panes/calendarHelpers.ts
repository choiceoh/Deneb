// Pure date helpers for the calendar pane (no React) — extracted so the
// month-range and day-key math can be unit-tested in isolation.
import { eventDayKeys, monthLabel, monthMatrix } from "@/format";
import type { CalEvent } from "@/types";

// RFC3339 → <input type="datetime-local"> value (local wall-clock, minute precision).
export function toLocalInput(iso?: string): string {
  if (!iso) return "";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "";
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

// The from/to ISO range covering the whole visible month grid (leading + trailing
// days), plus a cache key for the range query and the month heading label.
export function visibleRangeForMonth(year: number, month0: number) {
  const weeks = monthMatrix(year, month0);
  const first = weeks[0][0];
  const lastWeek = weeks[weeks.length - 1];
  const last = lastWeek[lastWeek.length - 1];
  const to = new Date(last);
  to.setDate(to.getDate() + 1);
  const from = first.toISOString();
  const toIso = to.toISOString();
  return {
    from,
    to: toIso,
    cacheKey: `calendar-range.${from}.${toIso}`,
    label: monthLabel(year, month0),
  };
}

// A "YYYY-M-D" day key → local Date (or null if malformed).
export function parseDayKey(key?: string): Date | null {
  const m = /^(\d{4})-(\d{1,2})-(\d{1,2})$/.exec(key ?? "");
  if (!m) return null;
  return new Date(Number(m[1]), Number(m[2]) - 1, Number(m[3]));
}

// Does an event touch the given month (any local day it spans falls in year/month0)?
// The month agenda list filters by this so the grid's leading/trailing spill-days —
// which belong to the adjacent months — don't surface under the "{month} 일정"
// heading. (visibleRangeForMonth fetches the whole grid range, so a July-1 event in
// June's trailing week is in `events`; it belongs on the grid cell, not the June list.)
export function eventInMonth(ev: CalEvent, year: number, month0: number): boolean {
  return eventDayKeys(ev.start, ev.end).some((k) => {
    const [y, m] = k.split("-");
    return Number(y) === year && Number(m) === month0 + 1;
  });
}

// ── Event-form date/time helpers ──────────────────────────────────────────────
// The form stores start/end as one "YYYY-MM-DDTHH:MM" datetime-local string each,
// but edits them through a separate 날짜 input and 시간 input. These split/rejoin
// that string so the two inputs drive one value.

const pad2 = (n: number): string => String(n).padStart(2, "0");

// The date ("YYYY-MM-DD") and time ("HH:MM") parts of a datetime-local string.
export function dtDate(dt: string): string {
  return dt.slice(0, 10);
}
export function dtTime(dt: string): string {
  return dt.length >= 16 ? dt.slice(11, 16) : "";
}
// Replace just the date part (keeping the time; a fresh value defaults to 09:00).
export function withDatePart(dt: string, date: string): string {
  if (!date) return dt;
  return `${date}T${dtTime(dt) || "09:00"}`;
}
// Replace just the time part (keeping the date; falls back to today if unset).
export function withTimePart(dt: string, time: string, now = new Date()): string {
  const date = dtDate(dt) || `${now.getFullYear()}-${pad2(now.getMonth() + 1)}-${pad2(now.getDate())}`;
  return `${date}T${time || "00:00"}`;
}
// dt shifted by `mins` minutes, as a local datetime-local string ("" if unparseable).
export function addMinutesDt(dt: string, mins: number): string {
  const d = new Date(dt);
  if (Number.isNaN(d.getTime())) return "";
  d.setMinutes(d.getMinutes() + mins);
  return toLocalInput(d.toISOString());
}
// Sensible default start for a NEW event: the given day (or today) at the next full
// hour when it's today, else 09:00 — so the form opens pre-filled instead of blank.
export function defaultStartDt(dayKey: string | null, now = new Date()): string {
  const base = (dayKey && parseDayKey(dayKey)) || now;
  const isToday =
    base.getFullYear() === now.getFullYear() && base.getMonth() === now.getMonth() && base.getDate() === now.getDate();
  const hour = isToday ? now.getHours() + 1 : 9;
  return toLocalInput(new Date(base.getFullYear(), base.getMonth(), base.getDate(), hour, 0, 0, 0).toISOString());
}
