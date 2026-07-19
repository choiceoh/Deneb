// Stat count-up math for DenebUi's stat tiles — pure module (component-free
// so react fast-refresh stays clean); also exercised directly by tests.

/** First numeric run (commas/decimal allowed) inside a stat value. */
const STAT_NUM_RE = /\d[\d,]*(?:\.\d+)?/;

/** One frame of the stat count-up: the numeric run scaled by an eased
 * [progress], prefix/suffix intact, decimal width and comma grouping matching
 * the target. progress ≥ 1 returns the ORIGINAL string so exact metrics
 * ("12.45%", 2-decimal FX) keep their full precision (native-parity rule). */
export function statCountUpFrame(value: string, progress: number): string {
  if (progress >= 1) return value;
  const m = STAT_NUM_RE.exec(value);
  if (!m) return value;
  const target = Number(m[0].replace(/,/g, ""));
  if (!Number.isFinite(target)) return value;
  const decimals = m[0].includes(".") ? m[0].length - m[0].indexOf(".") - 1 : 0;
  const eased = 1 - Math.pow(1 - Math.max(0, progress), 3); // fast-out-slow-in
  const v = target * eased;
  const s = m[0].includes(",")
    ? v.toLocaleString("ko-KR", { minimumFractionDigits: decimals, maximumFractionDigits: decimals })
    : v.toFixed(decimals);
  return value.slice(0, m.index) + s + value.slice(m.index + m[0].length);
}
