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

/** Selection index guard: an out-of-range or absent index must not silently
 * open item 0 — the wearer tapped a specific row, and opening a different one
 * is worse than doing nothing. */
export function resolveSelectionIndex(raw: unknown, count: number): number {
  if (count <= 0) return -1
  if (typeof raw !== 'number' || !Number.isFinite(raw)) return -1
  const i = Math.trunc(raw)
  if (i < 0 || i >= count) return -1
  return i
}
