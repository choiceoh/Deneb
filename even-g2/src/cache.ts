import type { GlancePayload } from './deneb'

// Last-known glance, kept on the glasses.
//
// Without this, walking out of Tailscale range and opening the app gives an
// error screen and nothing else — the one moment the wearer most wants to see
// what was on their plate. The gateway is on a private network reached through
// a phone, so "no link" is a normal condition, not an exceptional one.
//
// The copy is only ever shown WITH an explicit marker (see connectionLabel):
// a saved glance presented as if it were current is worse than no glance.

const STORAGE_KEY = 'deneb.even.g2.glance'

/** Refuse anything that would not render — a corrupt cache must not wedge boot. */
function usable(raw: unknown): raw is GlancePayload {
  if (!raw || typeof raw !== 'object') return false
  const p = raw as Partial<GlancePayload>
  if (typeof p.text !== 'string' || !p.text.trim()) return false
  if (!Array.isArray(p.pages) || !Array.isArray(p.items)) return false
  return p.pages.every((page) => page && typeof page.id === 'string' && typeof page.text === 'string')
}

export function saveCachedGlance(payload: GlancePayload): void {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(payload))
  } catch {
    // Quota or a locked-down WebView — a cache miss is not worth failing a poll.
  }
}

export function loadCachedGlance(): GlancePayload | null {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (!raw) return null
    const parsed = JSON.parse(raw) as unknown
    return usable(parsed) ? parsed : null
  } catch {
    return null
  }
}

export function clearCachedGlance(): void {
  try {
    localStorage.removeItem(STORAGE_KEY)
  } catch {
    // ignore
  }
}
