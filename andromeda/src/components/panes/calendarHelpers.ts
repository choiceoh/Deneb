// Pure date helpers for the calendar pane (no React) — extracted so the
// month-range and day-key math can be unit-tested in isolation.
import { monthLabel, monthMatrix } from "@/format";

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
