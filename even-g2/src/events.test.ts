import { describe, expect, it } from 'vitest'
import { OsEventTypeList } from '@evenrealities/even_hub_sdk'
import { dispatchHubEvent } from './events'

describe('observed host shapes (simulator automation, 2026-07-25)', () => {
  // These two are VERBATIM from the smoke run's console capture. They are the
  // reason #4267 shipped two broken fixes, so they are pinned as fixtures.
  it('a bare list click opens the detail with no index', () => {
    expect(
      dispatchHubEvent({ listEvent: { containerID: 1, containerName: 'alerts' } }),
    ).toEqual({ kind: 'openDetail', index: undefined })
  })

  it('the exit double-tap arrives on sysEvent, not listEvent/textEvent', () => {
    expect(dispatchHubEvent({ sysEvent: { eventType: 3, eventSource: 1 } })).toEqual({
      kind: 'shutdown',
    })
  })
})

describe('lifecycle events (the simulator cannot inject these)', () => {
  // POST /api/input only sends up/down/click/double_click, so this table is the
  // ONLY coverage these paths will ever get.
  const cases: Array<[OsEventTypeList, string]> = [
    [OsEventTypeList.SYSTEM_EXIT_EVENT, 'stopLoop'],
    [OsEventTypeList.ABNORMAL_EXIT_EVENT, 'stopLoop'],
    [OsEventTypeList.FOREGROUND_EXIT_EVENT, 'pause'],
    [OsEventTypeList.FOREGROUND_ENTER_EVENT, 'resume'],
  ]
  for (const [eventType, kind] of cases) {
    it(`${OsEventTypeList[eventType]} → ${kind}`, () => {
      expect(dispatchHubEvent({ sysEvent: { eventType } })).toEqual({ kind })
    })
  }

  // A host teardown must never be answered by fighting it for the container.
  it('a host teardown stops the loop without closing the container', () => {
    const intent = dispatchHubEvent({ sysEvent: { eventType: OsEventTypeList.SYSTEM_EXIT_EVENT } })
    expect(intent.kind).toBe('stopLoop')
    expect(intent.kind).not.toBe('shutdown')
  })
})

describe('touchpad actions on every channel', () => {
  const channels = ['listEvent', 'textEvent', 'sysEvent'] as const
  it('double-tap always means leave', () => {
    for (const ch of channels) {
      const ev = { [ch]: { eventType: OsEventTypeList.DOUBLE_CLICK_EVENT, containerID: 1 } }
      expect(dispatchHubEvent(ev).kind, ch).toBe('shutdown')
    }
  })

  it('scroll maps to page navigation on every channel', () => {
    for (const ch of channels) {
      expect(
        dispatchHubEvent({ [ch]: { eventType: OsEventTypeList.SCROLL_BOTTOM_EVENT, containerID: 1 } }).kind,
        ch,
      ).toBe('nextPage')
      expect(
        dispatchHubEvent({ [ch]: { eventType: OsEventTypeList.SCROLL_TOP_EVENT, containerID: 1 } }).kind,
        ch,
      ).toBe('prevPage')
    }
  })

  it('a click is a tap on text/sys but a detail-open on a list', () => {
    expect(dispatchHubEvent({ textEvent: { eventType: 0, containerID: 1 } }).kind).toBe('tap')
    expect(dispatchHubEvent({ sysEvent: { eventType: 0 } }).kind).toBe('tap')
    expect(dispatchHubEvent({ listEvent: { eventType: 0, currentSelectItemIndex: 2 } })).toEqual({
      kind: 'openDetail',
      index: 2,
    })
  })

  // The host may send the ordinal, the enum name, or the short form.
  it('accepts the string forms the SDK normalizer supports', () => {
    expect(dispatchHubEvent({ sysEvent: { eventType: 'DOUBLE_CLICK_EVENT' } }).kind).toBe('shutdown')
    expect(dispatchHubEvent({ sysEvent: { eventType: 'DOUBLE_CLICK' } }).kind).toBe('shutdown')
  })
})

describe('containers and unknown input', () => {
  it('ignores text events for a container this app does not draw', () => {
    expect(dispatchHubEvent({ textEvent: { eventType: 0, containerID: 9 } }).kind).toBe('ignore')
  })

  it('accepts the list header (id 2) as well as the main container (id 1)', () => {
    expect(dispatchHubEvent({ textEvent: { eventType: 0, containerID: 2 } }).kind).toBe('tap')
  })

  // A future host event type must not throw — the app just does nothing.
  it('resolves anything unrecognized to ignore', () => {
    for (const ev of [null, undefined, 42, 'click', {}, { imuEvent: {} }, { sysEvent: { eventType: 8 } }]) {
      expect(dispatchHubEvent(ev).kind, JSON.stringify(ev)).toBe('ignore')
    }
  })
})
