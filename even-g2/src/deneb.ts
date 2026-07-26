import type { GlanceSettings } from './settings'
import { REQUEST_TIMEOUT_MS } from './refresh'

export type GlancePage = {
  id: string
  title: string
  text: string
  empty?: boolean
}

export type GlanceItem = {
  id: string
  title: string
  preview?: string
  body?: string
  priority?: number
  age?: string
}

export type GlancePayload = {
  text: string
  pages: GlancePage[]
  items: GlanceItem[]
  /** "일정 2 · 할 일 3" — what is behind the alert page. */
  counts?: string
  generated?: string
  cached?: boolean
}

type GlanceResponse = {
  text?: string
  pages?: GlancePage[]
  items?: GlanceItem[]
  counts?: string
  generated?: string
  cached?: boolean
  error?: { message?: string }
}

type StatusResponse = {
  ok?: boolean
  chatReady?: boolean
  session?: string
  error?: { message?: string }
}

/**
 * getJSON wraps fetch with a hard deadline.
 *
 * AbortController + setTimeout rather than AbortSignal.timeout: this runs in
 * the glasses' embedded WebView, and the explicit controller works on every
 * host the SDK targets. Without a deadline a stalled private-network request
 * pins the app's `busy` flag forever and every input stops responding.
 */
async function getJSON<T>(url: string, token: string, timeoutMs: number): Promise<{ res: Response; data: T }> {
  const controller = new AbortController()
  const timer = setTimeout(() => controller.abort(), timeoutMs)
  try {
    const res = await fetch(url, {
      method: 'GET',
      headers: {
        Authorization: `Bearer ${token}`,
        Accept: 'application/json',
      },
      signal: controller.signal,
    })
    const data = (await res.json()) as T
    return { res, data }
  } catch (err) {
    // A caught abort must read as a timeout, not as an opaque DOMException —
    // this string is what the wearer sees on the HUD.
    if (err instanceof Error && err.name === 'AbortError') {
      throw new Error(`시간 초과 (${Math.round(timeoutMs / 1000)}초)`)
    }
    throw err
  } finally {
    clearTimeout(timer)
  }
}

export async function fetchGlance(
  settings: GlanceSettings,
  opts?: { fresh?: boolean },
): Promise<GlancePayload> {
  const qs = opts?.fresh ? '?fresh=1' : ''
  const url = `${settings.baseUrl}/api/even/glance${qs}`
  const { res, data } = await getJSON<GlanceResponse>(url, settings.token, REQUEST_TIMEOUT_MS)
  if (!res.ok) {
    throw new Error(data.error?.message || `HTTP ${res.status}`)
  }
  const text = data.text?.trim()
  if (!text) {
    throw new Error('empty glance')
  }
  const pages = normalizePages(data.pages, text)
  const items = normalizeItems(data.items)
  return {
    text,
    pages,
    items,
    counts: typeof data.counts === 'string' ? data.counts.trim() : undefined,
    generated: data.generated,
    cached: data.cached,
  }
}

export function normalizePages(pages: GlancePage[] | undefined, text: string): GlancePage[] {
  if (Array.isArray(pages) && pages.length > 0) {
    return pages
      .filter((p) => p && p.id && p.text?.trim())
      .map((p) => ({
        id: String(p.id),
        title: String(p.title || pageTitle(p.id)),
        text: String(p.text).trim(),
        empty: !!p.empty,
      }))
  }
  return [{ id: 'home', title: '알림', text }]
}

export function normalizeItems(items: GlanceItem[] | undefined): GlanceItem[] {
  if (!Array.isArray(items)) return []
  return items
    .filter((it) => it && String(it.title || '').trim())
    .map((it, i) => {
      const title = String(it.title).trim()
      const preview = String(it.preview || '').trim()
      const body = String(it.body || preview || title).trim()
      return {
        id: String(it.id || `alert-${i + 1}`),
        title: truncate(title, 48),
        preview: preview ? truncate(preview, 80) : undefined,
        body: truncate(body, 280),
        priority: typeof it.priority === 'number' ? it.priority : undefined,
        age: String(it.age || '').trim() || undefined,
      }
    })
    .slice(0, 12)
}

/**
 * formatAlertDetail renders one alert full-screen.
 *
 * `pos` drives the footer, which used to promise `↓다음페이지` — a swipe here
 * has never gone to the next page, and now it steps to the next ALERT so the
 * wearer can read through a morning's worth without going back to the list
 * between each one. A HUD footer is the only affordance there is, so it has to
 * say what the swipe actually does.
 */
export function formatAlertDetail(item: GlanceItem, pos?: { index: number; total: number }): string {
  const mark = (item.priority ?? 0) >= 4 ? '! ' : ''
  const meta = [item.age, (item.priority ?? 0) >= 4 ? '긴급' : ''].filter(Boolean).join(' · ')
  const counter = pos && pos.total > 1 ? ` (${pos.index + 1}/${pos.total})` : ''
  const lines = [`${mark}${item.title}${counter}`]
  if (meta) lines.push(meta)
  lines.push('')
  // The detail spends five lines on furniture, so the body gets about four —
  // roughly 120 runes. normalizeItems keeps 280 for the wire; anything past
  // what fits was being drawn off the bottom edge where nobody could read it.
  lines.push(truncate(item.body || item.preview || '(내용 없음)', 120))
  lines.push('')
  const hasNext = !!pos && pos.index < pos.total - 1
  // A gesture nobody is told about does not exist on a HUD — the footer is the
  // only affordance there is.
  lines.push(`${hasNext ? '탭=목록 · ↓다음알림' : '탭=목록 · ↓목록으로'} · 더블탭=확인`)
  return lines.join('\n')
}

/**
 * listLabel renders one alert as a single line.
 *
 * 30 runes, not 64: a rendered frame fits roughly forty Korean characters on a
 * line, and a label that wraps silently costs a second line out of a ten-line
 * budget — which is how the footer ended up off the bottom of the glass.
 */
export function listLabel(item: GlanceItem): string {
  const mark = (item.priority ?? 0) >= 4 ? '! ' : '· '
  const age = item.age ? ` · ${item.age}` : ''
  return truncate(`${mark}${item.title}${age}`, 30)
}

/**
 * ackAlert marks one alert handled on the gateway.
 *
 * The only write this plugin makes. It carries the same deadline as the reads:
 * a hung ack must not leave the wearer staring at a confirm screen, and the
 * failure has to be visible because the alert stays otherwise.
 */
export async function ackAlert(settings: GlanceSettings, id: string): Promise<void> {
  const controller = new AbortController()
  const timer = setTimeout(() => controller.abort(), REQUEST_TIMEOUT_MS)
  try {
    const res = await fetch(`${settings.baseUrl}/api/even/ack`, {
      method: 'POST',
      headers: {
        Authorization: `Bearer ${settings.token}`,
        'Content-Type': 'application/json',
        Accept: 'application/json',
      },
      body: JSON.stringify({ id }),
      signal: controller.signal,
    })
    if (!res.ok) {
      const data = (await res.json().catch(() => ({}))) as { error?: { message?: string } }
      throw new Error(data.error?.message || `HTTP ${res.status}`)
    }
  } catch (err) {
    if (err instanceof Error && err.name === 'AbortError') {
      throw new Error(`시간 초과 (${Math.round(REQUEST_TIMEOUT_MS / 1000)}초)`)
    }
    throw err
  } finally {
    clearTimeout(timer)
  }
}

export function pageTitle(id: string): string {
  switch (id) {
    case 'home':
      return '알림'
    case 'alerts':
      return '알림 전체'
    case 'cal':
      return '일정'
    case 'todo':
      return '할 일'
    case 'urgent': // legacy page id
      return '알림'
    default:
      return id
  }
}

export async function fetchStatus(settings: GlanceSettings): Promise<StatusResponse> {
  const url = `${settings.baseUrl}/api/even/status`
  const { res, data } = await getJSON<StatusResponse>(url, settings.token, REQUEST_TIMEOUT_MS)
  if (!res.ok) {
    throw new Error(data.error?.message || `HTTP ${res.status}`)
  }
  return data
}

export function formatGeneratedLabel(iso?: string, cached?: boolean): string {
  if (!iso) return cached ? '캐시' : ''
  const t = Date.parse(iso)
  if (Number.isNaN(t)) return ''
  const mins = Math.max(0, Math.round((Date.now() - t) / 60000))
  const age = mins <= 0 ? '방금' : `${mins}분 전`
  return cached ? `${age}·캐시` : age
}

function truncate(s: string, max: number): string {
  const chars = Array.from(s)
  if (chars.length <= max) return s
  return chars.slice(0, Math.max(0, max - 1)).join('') + '…'
}
