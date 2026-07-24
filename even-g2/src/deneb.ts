import type { GlanceSettings } from './settings'

type GlanceResponse = {
  text?: string
  error?: { message?: string }
}

export async function fetchGlance(settings: GlanceSettings): Promise<string> {
  const url = `${settings.baseUrl}/api/even/glance`
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
  return text
}
