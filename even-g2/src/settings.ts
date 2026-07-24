export type GlanceSettings = {
  /** Gateway origin, e.g. http://100.x.x.x:18789 — no trailing slash. */
  baseUrl: string
  /** Same value as DENEB_EVEN_G2_BRIDGE_TOKEN. */
  token: string
}

const STORAGE_KEY = 'deneb.even.g2.settings'

export function loadSettings(): GlanceSettings {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (!raw) return { baseUrl: '', token: '' }
    const parsed = JSON.parse(raw) as Partial<GlanceSettings>
    return {
      baseUrl: String(parsed.baseUrl ?? '').replace(/\/$/, ''),
      token: String(parsed.token ?? ''),
    }
  } catch {
    return { baseUrl: '', token: '' }
  }
}

export function saveSettings(next: GlanceSettings): void {
  localStorage.setItem(
    STORAGE_KEY,
    JSON.stringify({
      baseUrl: next.baseUrl.replace(/\/$/, ''),
      token: next.token,
    }),
  )
}
