import type { GlancePayload } from "./deneb";

// Refresh-loop policy, kept pure so it is testable without the glasses bridge.
//
// The loop runs on a battery device whose display sits in the wearer's field of
// view, over a private network that is routinely slow or absent (Tailscale, out
// of range). Those two facts drive every decision here: a background poll must
// be invisible when nothing changed, and a dead link must back off instead of
// retrying every 45s forever.

/** Base interval between background polls when the gateway is answering. */
export const BASE_REFRESH_MS = 45_000;

/** Ceiling for the failure backoff — roughly 12 minutes between retries. */
export const MAX_REFRESH_MS = 720_000;

/**
 * How long a single glance/status request may take before it is aborted.
 *
 * Without this the whole app wedges: refreshGlance holds `busy = true` for the
 * duration, and every input handler starts with `if (busy) return`, so one
 * hanging fetch silently kills tap, swipe and detail-open until the WebView is
 * restarted. The device's own Custom AI deadline is 15s, so a glance that has
 * not answered in 10 is already useless to the wearer.
 */
export const REQUEST_TIMEOUT_MS = 10_000;

/**
 * nextDelayMs backs off exponentially while the gateway is unreachable.
 *
 * Walking out of Tailscale range used to mean a "불러오는 중… → 오류" flash in
 * the wearer's vision every 45 seconds indefinitely; the backoff turns that
 * into a handful of retries that fade out, and any success resets it.
 */
export function nextDelayMs(consecutiveFailures: number): number {
  if (consecutiveFailures <= 0) return BASE_REFRESH_MS;
  const grown = BASE_REFRESH_MS * 2 ** Math.min(consecutiveFailures, 8);
  return Math.min(grown, MAX_REFRESH_MS);
}

/**
 * payloadSignature is the render key: two payloads with the same signature
 * produce the same screen, so the loop can skip the redraw entirely.
 *
 * `generated`/`cached` are deliberately EXCLUDED. They only feed a relative
 * "3분 전" stamp, and including them would make every poll look like a change
 * — which is exactly the flicker this exists to prevent.
 */
export function payloadSignature(payload: GlancePayload): string {
  const pages = payload.pages
    .map((p) => `${p.id}${p.title}${p.text}${p.empty ? 1 : 0}`)
    .join("");
  const items = payload.items
    .map(
      (it) =>
        `${it.id}${it.title}${it.preview ?? ""}${it.body ?? ""}${it.priority ?? ""}`,
    )
    .join("");
  return `${pages}${items}`;
}

/**
 * connectionLabel is the quiet counterpart to the silent-failure policy.
 *
 * A background failure deliberately never reaches the display — flashing
 * "오류" into someone's field of view every time they walk out of Tailscale
 * range is worse than saying nothing. But saying nothing turns a stale screen
 * into a confident lie: with the backoff running out to 12 minutes, the wearer
 * glances at alerts from a quarter of an hour ago and reads them as current.
 *
 * So: no flash, but one word in the header once the link is properly down. Two
 * failures, not one, because a single missed poll is normal on a phone-relayed
 * private network and would otherwise blink the marker on and off.
 */
export function connectionLabel(
  consecutiveFailures: number,
  servingCache = false,
): string {
  // A saved copy says so from the FIRST frame — the two-failure grace period is
  // about not blinking a marker over live data, and this is not live data.
  if (servingCache) return "오프라인 · 저장본";
  return consecutiveFailures >= 2 ? "연결 끊김" : "";
}

/**
 * Usable text lines on the glass, measured — not guessed.
 *
 * A CI frame with eight alerts (run 30180528557, `11-after-payload-change.png`)
 * put the footer half off the bottom edge at eleven lines, so ten is what there
 * is. Everything the alert page draws has to be budgeted against this: the
 * gateway can return twelve alerts and an app-drawn text page does not scroll.
 */
export const HUD_LINES = 10;

/** Fallback window when the caller has no budget to hand (tests, defaults). */
export const ALERT_WINDOW = 5;

/**
 * alertSlots is how many alert lines are left once the fixed furniture is paid
 * for: an optional lead line, the meta line, one blank separator and a footer.
 *
 * The header used to cost one more than this, because its first line was the
 * app's own name. Peripheral vision reliably reads the top line or two of a HUD
 * and the wearer already knows which app they opened, so that line now carries
 * "지금 금호타이어 · 종료 20분" instead.
 */
export function alertSlots(
  hasLead: boolean,
  lines: number = HUD_LINES,
): number {
  const furniture = (hasLead ? 1 : 0) + 1 + 1 + 1;
  return Math.max(1, lines - furniture);
}

/**
 * windowRange picks which slice of the alerts to draw so the cursor is always
 * visible, keeping it off the edge while there is list on both sides.
 */
export function windowRange(
  cursor: number,
  count: number,
  size: number = ALERT_WINDOW,
): { start: number; end: number } {
  if (count <= 0 || size <= 0) return { start: 0, end: 0 };
  if (count <= size) return { start: 0, end: count };
  const at = clampCursor(cursor, count);
  const half = Math.floor(size / 2);
  const start = Math.min(Math.max(at - half, 0), count - size);
  return { start, end: start + size };
}

/**
 * skipPage decides whether a swipe should pass straight over a page.
 *
 * `alerts` ("알림 전체") exists because the gateway's home page used to print
 * only the top few alerts as text — the second page was the "see all". The app
 * now draws EVERY alert on one page with a cursor and a window, so both pages
 * render the same list from the same items and differ only in their title. On a
 * device with four gestures, one of them going to a screen the wearer has just
 * left is not free.
 */
export function skipPage(
  page: { id: string; empty?: boolean },
  itemCount: number,
): boolean {
  if (page.id === "home") return false;
  if (page.id === "alerts" && itemCount > 0) return true;
  return !!page.empty;
}

/** What the device says about itself; every field is optional on the wire. */
export type WearStatus = {
  isWearing?: boolean;
  isInCase?: boolean;
  isCharging?: boolean;
  batteryLevel?: number;
};

/**
 * shouldPoll decides whether the background loop is worth running.
 *
 * This is the question that could not be answered by guessing: the loop pauses
 * on FOREGROUND_EXIT, but that event has never been observed — the simulator
 * cannot inject it — so "45s is too slow because we only run while visible" and
 * "45s burns radio for a screen nobody is looking at" were both plausible and
 * implied opposite tuning. The device answers it directly: it knows whether it
 * is on someone's face.
 *
 * FAIL-OPEN is the whole safety story. `isWearing` is optional on the wire, so
 * a host that never reports it must leave the app working exactly as before —
 * never silently stop refreshing forever.
 */
export function shouldPoll(status: WearStatus | undefined): boolean {
  if (!status) return true;
  if (status.isInCase === true) return false;
  if (status.isWearing === false) return false;
  return true;
}

/**
 * mergeWearStatus folds a device-status event into what is already known,
 * discarding events that are really just SDK defaults.
 *
 * The G2 SDK's `DeviceStatus.fromJson` fills absent fields in — `isWearing`
 * becomes `false` and `batteryLevel` becomes `0` (verified in the shipped
 * bundle: `'isWearing':![]`, `'batteryLevel':0x0`). So a status event about
 * something else entirely — a connection change — arrives looking exactly like
 * "not worn, flat battery", and reporting that to the wearer while the glasses
 * are on their face is what the device actually did.
 *
 * The tell is batteryLevel 0: a G2 at 0% is powered off and cannot report
 * anything, so 0 is never a reading. When it appears, the whole payload is
 * defaults and the previously known state stands.
 */
export function mergeWearStatus(
  known: WearStatus | undefined,
  incoming: WearStatus | undefined,
): WearStatus | undefined {
  if (!incoming) return known;
  const battery = incoming.batteryLevel;
  const looksDefaulted = battery === 0;
  if (looksDefaulted) return known;
  return {
    isWearing: incoming.isWearing,
    isInCase: incoming.isInCase,
    isCharging: incoming.isCharging,
    // Keep a battery figure only when it is a real one; an absent field is
    // "unknown", which the status screen renders differently from a number.
    batteryLevel:
      typeof battery === "number" && Number.isFinite(battery) && battery > 0
        ? battery
        : known?.batteryLevel,
  };
}

/** Battery below this, off charge, is when a wearable should stop being eager. */
export const LOW_BATTERY_PCT = 15;

/**
 * pollInterval stretches the base interval when the glasses are nearly flat.
 *
 * A HUD that dies at 4pm is worse than one that is a minute stale, and the
 * proactive path (the phone's notification HUD) is unaffected either way — so
 * the glance is exactly the thing that can afford to slow down.
 */
export function pollInterval(
  base: number,
  status: WearStatus | undefined,
): number {
  if (!status || status.isCharging === true) return base;
  const pct = status.batteryLevel;
  if (typeof pct !== "number" || !Number.isFinite(pct)) return base;
  return pct <= LOW_BATTERY_PCT ? base * 4 : base;
}

/** clampCursor keeps an alert cursor inside the current item list. */
export function clampCursor(cursor: number, count: number): number {
  if (count <= 0) return 0;
  if (!Number.isFinite(cursor) || cursor < 0) return 0;
  const i = Math.trunc(cursor);
  return i >= count ? count - 1 : i;
}

/**
 * advanceCursor decides what a swipe on an alert page means.
 *
 * Returns the new cursor position, or `'page'` when the swipe should leave the
 * alert page entirely. That boundary is the whole point: the host's list
 * container used to own it and emitted NOTHING at the end of the list, which
 * stranded every page behind the alerts. Owning it here also makes it testable
 * without the glasses.
 */
export function advanceCursor(
  cursor: number,
  count: number,
  dir: 1 | -1,
): number | "page" {
  if (count <= 0) return "page";
  const at = clampCursor(cursor, count);
  const next = at + dir;
  if (next < 0 || next >= count) return "page";
  return next;
}

/**
 * Selection index guard for a list click.
 *
 * ABSENT is the normal case, not an edge case. The simulator's automation run
 * (2026-07-25) showed the host delivering a list click as bare
 * `{"listEvent":{"containerID":1,"containerName":"alerts"}}` — no `eventType`,
 * no `currentSelectItemIndex`. #4267 hardened this to "refuse unless valid",
 * which turned the plugin's PRIMARY interaction (tap an alert → read it) into a
 * no-op; the smoke harness caught it as "a tap changes the screen: FAIL".
 *
 * So absent falls back to the first item — the gateway orders items by
 * priority, so item 0 is the best guess available, and opening something the
 * wearer can dismiss beats a dead tap. A PRESENT but nonsensical index
 * (non-numeric, negative, past the end) is still refused: that is a host bug,
 * and guessing there really would open the wrong alert.
 */
export function resolveSelectionIndex(raw: unknown, count: number): number {
  if (count <= 0) return -1;
  if (raw === undefined || raw === null) return 0;
  if (typeof raw !== "number" || !Number.isFinite(raw)) return -1;
  const i = Math.trunc(raw);
  if (i < 0 || i >= count) return -1;
  return i;
}
