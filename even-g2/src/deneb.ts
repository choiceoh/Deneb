import type { GlanceSettings } from './settings'

export type GlancePage = {
  id: string
  title: string
  text: string
}

export type GlancePayload = {
  text: string
  pages: GlancePage[]
  generated?: string
  cached?: boolean
}

type GlanceResponse = {
  text?: string
  pages?: GlancePage[]
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
  return { text, pages, generated: data.generated, cached: data.cached }
}

export function normalizePages(pages: GlancePage[] | undefined, text: string): GlancePage[] {
  if (Array.isArray(pages) && pages.length > 0) {
    return pages
      .filter((p) => p && p.id && p.text?.trim())
      .map((p) => ({
        id: String(p.id),
        title: String(p.title || pageTitle(p.id)),
        text: String(p.text).trim(),
      }))
  }
  return [{ id: 'home', title: '오늘', text }]
}

export function pageTitle(id: string): string {
  switch (id) {
    case 'home':
      return '오늘'
    case 'cal':
      return '일정'
    case 'urgent':
      return '긴급'
    case 'todo':
      return '할 일'
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