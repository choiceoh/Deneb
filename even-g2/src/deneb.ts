import type { GlanceSettings } from './settings'

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
  generated?: string
  cached?: boolean
}

type GlanceResponse = {
  text?: string
  pages?: GlancePage[]
  items?: GlanceItem[]
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

export async function fetchGlance(
  settings: GlanceSettings,
  opts?: { fresh?: boolean },
): Promise<GlancePayload> {
  const qs = opts?.fresh ? '?fresh=1' : ''
  const url = `${settings.baseUrl}/api/even/glance${qs}`
  const res = await fetch(url, {
    method: 'GET',
    headers: {
      Authorization: `Bearer ${settings.token}`,
      Accept: 'application/json',
    },
  })
  const data = (await res.json()) as GlanceResponse
  if (!res.ok) {
    throw new Error(data.error?.message || `HTTP ${res.status}`)
  }
  const text = data.text?.trim()
  if (!text) {
    throw new Error('empty glance')
  }
  const pages = normalizePages(data.pages, text)
  const items = normalizeItems(data.items)
  return { text, pages, items, generated: data.generated, cached: data.cached }
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

export function formatAlertDetail(item: GlanceItem): string {
  const mark = (item.priority ?? 0) >= 4 ? '! ' : ''
  const meta = [item.age, (item.priority ?? 0) >= 4 ? '긴급' : ''].filter(Boolean).join(' · ')
  const lines = [`${mark}${item.title}`]
  if (meta) lines.push(meta)
  lines.push('')
  lines.push(item.body || item.preview || '(내용 없음)')
  lines.push('')
  lines.push('탭=목록 · ↓다음페이지')
  return lines.join('\n')
}

export function listLabel(item: GlanceItem): string {
  const mark = (item.priority ?? 0) >= 4 ? '! ' : '· '
  const age = item.age ? ` · ${item.age}` : ''
  return truncate(`${mark}${item.title}${age}`, 64)
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
  const res = await fetch(url, {
    method: 'GET',
    headers: {
      Authorization: `Bearer ${settings.token}`,
      Accept: 'application/json',
    },
  })
  const data = (await res.json()) as StatusResponse
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
