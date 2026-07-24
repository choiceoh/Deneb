import type { GlanceSettings } from './settings'

export type GlancePayload = {
  text: string
  generated?: string
  cached?: boolean
}

type GlanceResponse = {
  text?: string
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
  return { text, generated: data.generated, cached: data.cached }
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
