import { describe, expect, it } from 'vitest'
import {
  BASE_REFRESH_MS,
  MAX_REFRESH_MS,
  nextDelayMs,
  payloadSignature,
  resolveSelectionIndex,
} from './refresh'
import type { GlancePayload } from './deneb'

function payload(over: Partial<GlancePayload> = {}): GlancePayload {
  return {
    text: '알림 2건',
    pages: [{ id: 'home', title: '알림', text: '알림 2건' }],
    items: [{ id: 'a1', title: '견적 회신 필요', preview: '금호타이어', priority: 4 }],
    generated: '2026-07-25T10:00:00Z',
    cached: false,
    ...over,
  }
}

describe('nextDelayMs', () => {
  it('polls at the base interval while the gateway answers', () => {
    expect(nextDelayMs(0)).toBe(BASE_REFRESH_MS)
    expect(nextDelayMs(-1)).toBe(BASE_REFRESH_MS)
  })

  // Walking out of Tailscale range used to mean a retry every 45s forever.
  it('backs off while the gateway is unreachable', () => {
    expect(nextDelayMs(1)).toBe(BASE_REFRESH_MS * 2)
    expect(nextDelayMs(3)).toBe(BASE_REFRESH_MS * 8)
    expect(nextDelayMs(2)).toBeGreaterThan(nextDelayMs(1))
  })

  it('caps the backoff so recovery is never further than the ceiling away', () => {
    expect(nextDelayMs(50)).toBe(MAX_REFRESH_MS)
    expect(nextDelayMs(9)).toBeLessThanOrEqual(MAX_REFRESH_MS)
  })
})

describe('payloadSignature', () => {
  it('is stable for identical content — this is what suppresses the redraw', () => {
    expect(payloadSignature(payload())).toBe(payloadSignature(payload()))
  })

  // generated/cached only feed a relative "3분 전" stamp. Including them would
  // make every poll look like a change, which is the flicker being fixed.
  it('ignores the generated stamp and cache flag', () => {
    const a = payloadSignature(payload())
    const b = payloadSignature(payload({ generated: '2026-07-25T11:22:33Z', cached: true }))
    expect(a).toBe(b)
  })

  it('changes when a page body changes', () => {
    const b = payloadSignature(payload({ pages: [{ id: 'home', title: '알림', text: '알림 3건' }] }))
    expect(b).not.toBe(payloadSignature(payload()))
  })

  it('changes when an alert arrives, is edited, or is resolved', () => {
    const base = payloadSignature(payload())
    const added = payloadSignature(
      payload({ items: [...payload().items, { id: 'a2', title: '새 알림' }] }),
    )
    const edited = payloadSignature(
      payload({ items: [{ id: 'a1', title: '견적 회신 필요', preview: '기아', priority: 4 }] }),
    )
    const cleared = payloadSignature(payload({ items: [] }))
    expect(new Set([base, added, edited, cleared]).size).toBe(4)
  })
})

describe('resolveSelectionIndex', () => {
  it('accepts a real selection', () => {
    expect(resolveSelectionIndex(2, 5)).toBe(2)
    expect(resolveSelectionIndex(0, 1)).toBe(0)
  })

  // The old code clamped to item 0, so an unattributable tap opened the WRONG
  // alert. Refusing is the honest response — the wearer taps again.
  it('refuses an absent, non-numeric, or out-of-range index', () => {
    for (const raw of [undefined, null, NaN, '1', {}, -1, 5, 99]) {
      expect(resolveSelectionIndex(raw, 5)).toBe(-1)
    }
  })

  it('refuses any index when the list is empty', () => {
    expect(resolveSelectionIndex(0, 0)).toBe(-1)
  })

  it('truncates a fractional index rather than rejecting it', () => {
    expect(resolveSelectionIndex(2.7, 5)).toBe(2)
  })
})
