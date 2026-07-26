import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { clearCachedGlance, loadCachedGlance, saveCachedGlance } from './cache'
import type { GlancePayload } from './deneb'

function memoryStorage() {
  const map = new Map<string, string>()
  return {
    getItem: (k: string) => map.get(k) ?? null,
    setItem: (k: string, v: string) => void map.set(k, v),
    removeItem: (k: string) => void map.delete(k),
    clear: () => map.clear(),
    key: (i: number) => [...map.keys()][i] ?? null,
    get length() {
      return map.size
    },
  } as Storage
}

const payload: GlancePayload = {
  text: '알림 2건',
  pages: [{ id: 'home', title: '알림', text: '견적 회신 필요' }],
  items: [{ id: 'a1', title: '견적 회신 필요', body: '금호타이어 곡성', priority: 4 }],
  generated: '2026-07-26T00:00:00.000Z',
}

beforeEach(() => {
  vi.stubGlobal('localStorage', memoryStorage())
})

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('glance cache', () => {
  it('round-trips a payload', () => {
    saveCachedGlance(payload)
    expect(loadCachedGlance()).toEqual(payload)
  })

  it('is empty before anything is saved', () => {
    expect(loadCachedGlance()).toBeNull()
  })

  it('clears', () => {
    saveCachedGlance(payload)
    clearCachedGlance()
    expect(loadCachedGlance()).toBeNull()
  })

  it('refuses junk rather than wedging boot', () => {
    // A cache is read on the cold-open path, where a throw would leave the
    // wearer with no screen at all.
    localStorage.setItem('deneb.even.g2.glance', 'not json{')
    expect(loadCachedGlance()).toBeNull()
  })

  it('refuses a payload that would not render', () => {
    for (const bad of [
      {},
      { text: '', pages: [], items: [] },
      { text: 'x', pages: 'nope', items: [] },
      { text: 'x', pages: [{ id: 'home' }], items: [] },
      { text: 'x', pages: [], items: null },
    ]) {
      localStorage.setItem('deneb.even.g2.glance', JSON.stringify(bad))
      expect(loadCachedGlance()).toBeNull()
    }
  })

  it('survives a storage that refuses to write', () => {
    vi.stubGlobal('localStorage', {
      ...memoryStorage(),
      setItem: () => {
        throw new Error('QuotaExceededError')
      },
    } as Storage)
    // A cache miss must never fail the poll that produced the payload.
    expect(() => saveCachedGlance(payload)).not.toThrow()
  })
})
