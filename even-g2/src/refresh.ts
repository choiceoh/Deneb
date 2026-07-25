import type { GlancePayload } from './deneb'

// Refresh-loop policy, kept pure so it is testable without the glasses bridge.
//
// The loop runs on a battery device whose display sits in the wearer's field of
// view, over a private network that is routinely slow or absent (Tailscale, out
// of range). Those two facts drive every decision here: a background poll must
// be invisible when nothing changed, and a dead link must back off instead of
// retrying every 45s forever.

/** Base interval between background polls when the gateway is answering. */
export const BASE_REFRESH_MS = 45_000

/** Ceiling for the failure backoff — roughly 12 minutes between retries. */
export const MAX_REFRESH_MS = 720_000

/**
 * How long a single glance/status request may take before it is aborted.
 *
 * Without this the whole app wedges: refreshGlance holds `busy = true` for the
 * duration, and every input handler starts with `if (busy) return`, so one
 * hanging fetch silently kills tap, swipe and detail-open until the WebView is
 * restarted. The device's own Custom AI deadline is 15s, so a glance that has
 * not answered in 10 is already useless to the wearer.
 */
export const REQUEST_TIMEOUT_MS = 10_000

/**
 * nextDelayMs backs off exponentially while the gateway is unreachable.
 *
 * Walking out of Tailscale range used to mean a "불러오는 중… → 오류" flash in
 * the wearer's vision every 45 seconds indefinitely; the backoff turns that
 * into a handful of retries that fade out, and any success resets it.
 */
export function nextDelayMs(consecutiveFailures: number): number {
  if (consecutiveFailures <= 0) return BASE_REFRESH_MS
  const grown = BASE_REFRESH_MS * 2 ** Math.min(consecutiveFailures, 8)
  return Math.min(grown, MAX_REFRESH_MS)
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
    .join('')
  const items = payload.items
    .map((it) => `${it.id}${it.title}${it.preview ?? ''}${it.body ?? ''}${it.priority ?? ''}`)
    .join('')
  return `${pages}${items}`
}

/**
 * How many alert lines fit on the HUD alongside the header and footer.
 *
 * 576×288 holds roughly a dozen lines, and the alert page spends the rest on a
 * two-line header, two blank separators and a footer. The gateway can return up
 * to 12 alerts (normalizeItems caps it), so without a window the list simply
 * ran off the bottom of the glass — the host list container used to scroll and
 * an app-drawn text page does not.
 */
export const ALERT_WINDOW = 5

/**
 * windowRange picks which slice of the alerts to draw so the cursor is always
 * visible, keeping it off the edge while there is list on both sides.
 */
export function windowRange(
  cursor: number,
  count: number,
  size: number = ALERT_WINDOW,
): { start: number; end: number } {
  if (count <= 0 || size <= 0) return { start: 0, end: 0 }
  if (count <= size) return { start: 0, end: count }
  const at = clampCursor(cursor, count)
  const half = Math.floor(size / 2)
  const start = Math.min(Math.max(at - half, 0), count - size)
  return { start, end: start + size }
}

/** clampCursor keeps an alert cursor inside the current item list. */
export function clampCursor(cursor: number, count: number): number {
  if (count <= 0) return 0
  if (!Number.isFinite(cursor) || cursor < 0) return 0
  const i = Math.trunc(cursor)
  return i >= count ? count - 1 : i
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
export function advanceCursor(cursor: number, count: number, dir: 1 | -1): number | 'page' {
  if (count <= 0) return 'page'
  const at = clampCursor(cursor, count)
  const next = at + dir
  if (next < 0 || next >= count) return 'page'
  return next
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
  if (count <= 0) return -1
  if (raw === undefined || raw === null) return 0
  if (typeof raw !== 'number' || !Number.isFinite(raw)) return -1
  const i = Math.trunc(raw)
  if (i < 0 || i >= count) return -1
  return i
}
