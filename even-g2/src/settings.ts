export type GlanceSettings = {
  /** Gateway origin, e.g. http://100.x.x.x:18789 — no trailing slash. */
  baseUrl: string
  /** Same value as DENEB_EVEN_G2_BRIDGE_TOKEN. */
  token: string
}

const STORAGE_KEY = 'deneb.even.g2.settings'

export function emptySettings(): GlanceSettings {
  return { baseUrl: '', token: '' }
}

export function normalizeSettings(raw: Partial<GlanceSettings> | null | undefined): GlanceSettings {
  return {
    baseUrl: String(raw?.baseUrl ?? '').trim().replace(/\/$/, ''),
    token: String(raw?.token ?? '').trim(),
  }
}

export function loadStoredSettings(): GlanceSettings {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (!raw) return emptySettings()
    return normalizeSettings(JSON.parse(raw) as Partial<GlanceSettings>)
  } catch {
    return emptySettings()
  }
}

export function saveSettings(next: GlanceSettings): void {
  const normalized = normalizeSettings(next)
  localStorage.setItem(STORAGE_KEY, JSON.stringify(normalized))
}

/** Decode ?seed= base64url(JSON) used by QR / phone bootstrap. */
export function parseSeedParam(seed: string): GlanceSettings | null {
  const trimmed = seed.trim()
  if (!trimmed) return null
  try {
    const padded = trimmed.replace(/-/g, '+').replace(/_/g, '/')
    const padLen = (4 - (padded.length % 4)) % 4
    const b64 = padded + '='.repeat(padLen)
    const json = atob(b64)
    return normalizeSettings(JSON.parse(json) as Partial<GlanceSettings>)
  } catch {
    return null
  }
}

export function encodeSeed(settings: GlanceSettings): string {
  const normalized = normalizeSettings(settings)
  const json = JSON.stringify(normalized)
  const b64 = btoa(json)
  return b64.replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/g, '')
}

/**
 * Resolve settings in priority order:
 * 1) URL seed / query params (phone QR bootstrap) — persisted to localStorage
 * 2) localStorage
 * 3) /runtime-config.json baked at pack time
 */
export async function resolveSettings(): Promise<GlanceSettings> {
  const fromURL = readURLBootstrap()
  if (fromURL && fromURL.baseUrl && fromURL.token) {
    saveSettings(fromURL)
    clearBootstrapQuery()
    return fromURL
  }

  const stored = loadStoredSettings()
  if (stored.baseUrl && stored.token) {
    return stored
  }

  const baked = await loadRuntimeConfig()
  if (baked.baseUrl && baked.token) {
    saveSettings(baked)
    return baked
  }

  // Partial URL/local merges for iterative setup.
  const merged = normalizeSettings({
    baseUrl: fromURL?.baseUrl || stored.baseUrl || baked.baseUrl,
    token: fromURL?.token || stored.token || baked.token,
  })
  if (merged.baseUrl || merged.token) {
    saveSettings(merged)
    if (fromURL) clearBootstrapQuery()
  }
  return merged
}

/** Drop seed/token query params so the bearer is not left in the WebView URL bar. */
export function clearBootstrapQuery(): void {
  try {
    const url = new URL(window.location.href)
    let changed = false
    for (const key of ['seed', 'baseUrl', 'token', 'deneb_base', 'deneb_token']) {
      if (url.searchParams.has(key)) {
        url.searchParams.delete(key)
        changed = true
      }
    }
    if (changed) {
      window.history.replaceState({}, '', url.pathname + url.search + url.hash)
    }
  } catch {
    // ignore
  }
}

function readURLBootstrap(): GlanceSettings | null {
  try {
    const url = new URL(window.location.href)
    const seed = url.searchParams.get('seed')
    if (seed) {
      const parsed = parseSeedParam(seed)
      if (parsed) return parsed
    }
    const baseUrl = url.searchParams.get('baseUrl') || url.searchParams.get('deneb_base') || ''
    const token = url.searchParams.get('token') || url.searchParams.get('deneb_token') || ''
    if (baseUrl || token) {
      return normalizeSettings({ baseUrl, token })
    }
  } catch {
    // ignore
  }
  return null
}

async function loadRuntimeConfig(): Promise<GlanceSettings> {
  try {
    const res = await fetch('./runtime-config.json', { cache: 'no-store' })
    if (!res.ok) return emptySettings()
    return normalizeSettings((await res.json()) as Partial<GlanceSettings>)
  } catch {
    return emptySettings()
  }
}
