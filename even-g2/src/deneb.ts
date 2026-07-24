import type { GlanceSettings } from './settings'

const GLANCE_PROMPT =
  '안경 HUD용으로 오늘 일정·긴급 메일·할 일을 2~4줄 한국어 평문으로만 요약해. ' +
  '마크다운·URL·이모지 금지. 없으면 없다고 짧게.'

type ChatCompletionResponse = {
  choices?: Array<{ message?: { content?: string } }>
  error?: { message?: string }
}

export async function fetchGlance(settings: GlanceSettings): Promise<string> {
  const url = `${settings.baseUrl}/api/even/v1/chat/completions`
  const res = await fetch(url, {
    method: 'POST',
    headers: {
      Authorization: `Bearer ${settings.token}`,
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({
      model: 'deneb-glance',
      messages: [{ role: 'user', content: GLANCE_PROMPT }],
    }),
  })
  const data = (await res.json()) as ChatCompletionResponse
  if (!res.ok) {
    throw new Error(data.error?.message || `HTTP ${res.status}`)
  }
  const text = data.choices?.[0]?.message?.content?.trim()
  if (!text) {
    throw new Error('empty glance')
  }
  return text
}
