import { describe, expect, it } from 'vitest'
import {
  ALERT_WINDOW,
  BASE_REFRESH_MS,
  MAX_REFRESH_MS,
  advanceCursor,
  clampCursor,
  connectionLabel,
  windowRange,
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

  // The simulator run (2026-07-25) showed the host delivering a list click with
  // NO index at all — the normal shape, not an edge case. #4267 refused those
  // and made tap-to-detail a dead interaction; the smoke harness caught it.
  it('falls back to the first item when the host reports no index', () => {
    expect(resolveSelectionIndex(undefined, 5)).toBe(0)
    expect(resolveSelectionIndex(null, 5)).toBe(0)
  })

  // A PRESENT but nonsensical index is a host bug; guessing there would open
  // the wrong alert, so it stays refused.
  it('refuses a present but nonsensical index', () => {
    for (const raw of [NaN, '1', {}, -1, 5, 99]) {
      expect(resolveSelectionIndex(raw, 5)).toBe(-1)
    }
  })

  it('refuses any index when the list is empty', () => {
    expect(resolveSelectionIndex(0, 0)).toBe(-1)
    expect(resolveSelectionIndex(undefined, 0)).toBe(-1)
  })

  it('truncates a fractional index rather than rejecting it', () => {
    expect(resolveSelectionIndex(2.7, 5)).toBe(2)
  })
})

describe('clampCursor', () => {
  it('pins into range', () => {
    expect(clampCursor(0, 3)).toBe(0)
    expect(clampCursor(2, 3)).toBe(2)
    expect(clampCursor(9, 3)).toBe(2)
    expect(clampCursor(-4, 3)).toBe(0)
  })

  it('is 0 when there is nothing to point at', () => {
    expect(clampCursor(5, 0)).toBe(0)
  })

  it('survives a non-finite cursor', () => {
    expect(clampCursor(NaN, 3)).toBe(0)
    expect(clampCursor(Infinity, 3)).toBe(0)
  })
})

describe('advanceCursor', () => {
  it('walks down the alerts before leaving the page', () => {
    expect(advanceCursor(0, 3, 1)).toBe(1)
    expect(advanceCursor(1, 3, 1)).toBe(2)
    // On the last alert, the next swipe is a page turn — the behaviour the
    // host's list container never delivered, which stranded cal/todo.
    expect(advanceCursor(2, 3, 1)).toBe('page')
  })

  it('walks back up symmetrically', () => {
    expect(advanceCursor(2, 3, -1)).toBe(1)
    expect(advanceCursor(1, 3, -1)).toBe(0)
    expect(advanceCursor(0, 3, -1)).toBe('page')
  })

  it('leaves immediately on a single alert', () => {
    expect(advanceCursor(0, 1, 1)).toBe('page')
    expect(advanceCursor(0, 1, -1)).toBe('page')
  })

  it('leaves immediately when there are no alerts', () => {
    expect(advanceCursor(0, 0, 1)).toBe('page')
  })

  it('clamps a stale cursor before deciding', () => {
    // A poll can shrink the list under the wearer; the swipe after that must
    // still make sense rather than paging from a position that no longer exists.
    expect(advanceCursor(9, 3, -1)).toBe(1)
    expect(advanceCursor(9, 3, 1)).toBe('page')
  })
})

describe('windowRange', () => {
  it('shows everything when it fits', () => {
    expect(windowRange(0, 3)).toEqual({ start: 0, end: 3 })
    expect(windowRange(2, ALERT_WINDOW)).toEqual({ start: 0, end: ALERT_WINDOW })
  })

  it('keeps the cursor visible in a long list', () => {
    for (let cursor = 0; cursor < 12; cursor++) {
      const { start, end } = windowRange(cursor, 12)
      expect(end - start).toBe(ALERT_WINDOW)
      expect(cursor).toBeGreaterThanOrEqual(start)
      expect(cursor).toBeLessThan(end)
    }
  })

  it('pins to the ends instead of running past them', () => {
    expect(windowRange(0, 12)).toEqual({ start: 0, end: 5 })
    expect(windowRange(11, 12)).toEqual({ start: 7, end: 12 })
  })

  it('centres the cursor in the middle of a long list', () => {
    expect(windowRange(6, 12)).toEqual({ start: 4, end: 9 })
  })

  it('is empty when there is nothing to draw', () => {
    expect(windowRange(0, 0)).toEqual({ start: 0, end: 0 })
  })

  it('survives a stale cursor past the end', () => {
    const { start, end } = windowRange(99, 12)
    expect({ start, end }).toEqual({ start: 7, end: 12 })
  })
})

describe('connectionLabel', () => {
  it('stays quiet through a single missed poll', () => {
    // One miss is routine on a phone-relayed private network; blinking a marker
    // on and off for it is the flicker this whole policy exists to avoid.
    expect(connectionLabel(0)).toBe('')
    expect(connectionLabel(1)).toBe('')
  })

  it('speaks up once the link is properly down', () => {
    expect(connectionLabel(2)).toBe('연결 끊김')
    expect(connectionLabel(8)).toBe('연결 끊김')
  })
})
