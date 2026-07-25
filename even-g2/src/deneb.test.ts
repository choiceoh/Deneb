import { describe, expect, it, vi, afterEach } from 'vitest'
import { fetchGlance, normalizeItems, normalizePages, formatGeneratedLabel, listLabel } from './deneb'
import { REQUEST_TIMEOUT_MS } from './refresh'

const settings = { baseUrl: 'http://gw.test', token: 't0k' }

afterEach(() => {
  vi.unstubAllGlobals()
  vi.useRealTimers()
})

describe('fetchGlance deadline', () => {
  // Without a deadline a stalled private-network request pins the app's `busy`
  // flag forever and every input handler (`if (busy) return`) stops responding.
  it('aborts a hanging request and reports it as a timeout', async () => {
    vi.useFakeTimers()
    vi.stubGlobal('fetch', (_url: string, init?: RequestInit) =>
      new Promise((_resolve, reject) => {
        init?.signal?.addEventListener('abort', () => {
          const err = new Error('aborted')
          err.name = 'AbortError'
          reject(err)
        })
      }),
    )
    const pending = fetchGlance(settings)
    const assertion = expect(pending).rejects.toThrow('시간 초과')
    await vi.advanceTimersByTimeAsync(REQUEST_TIMEOUT_MS + 100)
    await assertion
  })

  it('passes an abort signal on every request', async () => {
    let sawSignal = false
    vi.stubGlobal('fetch', async (_url: string, init?: RequestInit) => {
      sawSignal = !!init?.signal
      return new Response(JSON.stringify({ text: '알림 없음' }), { status: 200 })
    })
    await fetchGlance(settings)
    expect(sawSignal).toBe(true)
  })

  it('surfaces the gateway error message on a non-2xx', async () => {
    vi.stubGlobal('fetch', async () =>
      new Response(JSON.stringify({ error: { message: '토큰 불일치' } }), { status: 401 }),
    )
    await expect(fetchGlance(settings)).rejects.toThrow('토큰 불일치')
  })
})

describe('normalizePages', () => {
  it('falls back to a single home page when the gateway sends none', () => {
    expect(normalizePages(undefined, '알림 2건')).toEqual([
      { id: 'home', title: '알림', text: '알림 2건' },
    ])
  })

  it('drops pages with no id or blank text', () => {
    const got = normalizePages(
      [
        { id: 'cal', title: '일정', text: ' 14:00 회의 ' },
        { id: '', title: 'x', text: 'y' },
        { id: 'todo', title: '할 일', text: '   ' },
      ],
      'fallback',
    )
    expect(got).toEqual([{ id: 'cal', title: '일정', text: '14:00 회의', empty: false }])
  })
})

describe('normalizeItems', () => {
  it('bounds the list and every field so one long body cannot blow the HUD', () => {
    const many = Array.from({ length: 30 }, (_, i) => ({
      id: `a${i}`,
      title: 'ㄱ'.repeat(80),
      body: 'ㄴ'.repeat(500),
    }))
    const got = normalizeItems(many)
    expect(got).toHaveLength(12)
    expect(Array.from(got[0].title)).toHaveLength(48)
    expect(Array.from(got[0].body!).length).toBeLessThanOrEqual(280)
  })

  it('synthesizes an id when the gateway omits one', () => {
    expect(normalizeItems([{ id: '', title: '알림' } as never])[0].id).toBe('alert-1')
  })

  it('drops entries with no title', () => {
    expect(normalizeItems([{ id: 'a', title: '  ' } as never])).toHaveLength(0)
  })
})

describe('HUD labels', () => {
  it('marks high priority so the wearer sees urgency in one glance', () => {
    expect(listLabel({ id: 'a', title: '급건', priority: 4 })).toMatch(/^! /)
    expect(listLabel({ id: 'b', title: '일반', priority: 1 })).toMatch(/^· /)
  })

  it('renders a relative age, and says 캐시 when there is no timestamp', () => {
    expect(formatGeneratedLabel(new Date().toISOString(), false)).toBe('방금')
    expect(formatGeneratedLabel(undefined, true)).toBe('캐시')
    expect(formatGeneratedLabel('not-a-date', false)).toBe('')
  })
})
