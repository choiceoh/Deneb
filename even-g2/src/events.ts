import { OsEventTypeList } from '@evenrealities/even_hub_sdk'

// Host event → intent, as a pure function.
//
// Why this is not inline in main.ts any more: the host speaks three channels
// (listEvent / textEvent / sysEvent) and the shapes it actually sends are NOT
// what the code assumed. The simulator's automation run on 2026-07-25 recorded
//
//   click        → {"listEvent":{"containerID":1,"containerName":"alerts"}}
//   double_click → {"sysEvent":{"eventType":3,"eventSource":1}}
//
// i.e. a list click with no eventType and no selection index, and the exit
// double-tap arriving on a channel the app never read. Both were shipped as
// "fixed" in #4267 on the strength of reading code, and both were wrong.
//
// The simulator can only inject four touchpad actions (up/down/click/
// double_click), so the LIFECYCLE events — foreground enter/exit, system exit —
// cannot be exercised by the smoke harness at all. Pulling the mapping out here
// is the only way they get tested.

/** What the app should do about an event. */
export type HubIntent =
  | { kind: 'ignore' }
  | { kind: 'tap' }
  | { kind: 'openDetail'; index: unknown }
  | { kind: 'nextPage' }
  | { kind: 'prevPage' }
  /** User asked to leave: close the container and stop the loop. */
  | { kind: 'shutdown' }
  /** Host is tearing us down: stop the loop, leave the container to it. */
  | { kind: 'stopLoop' }
  /** Backgrounded — stop polling a display nobody can see. */
  | { kind: 'pause' }
  /** Foregrounded — resume polling and refresh what is on screen. */
  | { kind: 'resume' }

/** Container IDs this app draws into (1 = main/list, 2 = list header). */
const OWNED_CONTAINERS = new Set([1, 2])

type LooseEvent = {
  listEvent?: { eventType?: unknown; currentSelectItemIndex?: unknown }
  textEvent?: { eventType?: unknown; containerID?: unknown }
  sysEvent?: { eventType?: unknown }
}

/**
 * touchIntent maps the four touchpad actions. Shared by all three channels
 * because the host uses the same OsEventTypeList ordinals on each.
 *
 * `undefined` counts as a click: the host omits eventType on a plain tap (the
 * observed list-click shape above), and treating that as "unknown, ignore"
 * would make the primary interaction dead.
 */
function touchIntent(rawType: unknown, onClick: () => HubIntent): HubIntent | undefined {
  const type = rawType === undefined || rawType === null ? OsEventTypeList.CLICK_EVENT : OsEventTypeList.fromJson(rawType)
  switch (type) {
    case OsEventTypeList.CLICK_EVENT:
      return onClick()
    case OsEventTypeList.DOUBLE_CLICK_EVENT:
      return { kind: 'shutdown' }
    case OsEventTypeList.SCROLL_BOTTOM_EVENT:
      return { kind: 'nextPage' }
    case OsEventTypeList.SCROLL_TOP_EVENT:
      return { kind: 'prevPage' }
    default:
      return undefined
  }
}

/** lifecycleIntent maps the events the touchpad cannot produce. */
function lifecycleIntent(rawType: unknown): HubIntent | undefined {
  switch (OsEventTypeList.fromJson(rawType)) {
    case OsEventTypeList.SYSTEM_EXIT_EVENT:
    case OsEventTypeList.ABNORMAL_EXIT_EVENT:
      return { kind: 'stopLoop' }
    case OsEventTypeList.FOREGROUND_EXIT_EVENT:
      return { kind: 'pause' }
    case OsEventTypeList.FOREGROUND_ENTER_EVENT:
      return { kind: 'resume' }
    default:
      return undefined
  }
}

/**
 * dispatchHubEvent turns one raw host event into a single intent.
 *
 * Channel order matters: listEvent first (only it carries a selection), then
 * sysEvent (lifecycle + touchpad), then textEvent (plain pages). An event that
 * matches nothing resolves to `ignore` rather than throwing — the host may add
 * event types this build has never heard of.
 */
export function dispatchHubEvent(event: unknown): HubIntent {
  if (!event || typeof event !== 'object') return { kind: 'ignore' }
  const e = event as LooseEvent

  if (e.listEvent) {
    return (
      touchIntent(e.listEvent.eventType, () => ({
        kind: 'openDetail',
        index: e.listEvent?.currentSelectItemIndex,
      })) ?? { kind: 'ignore' }
    )
  }

  if (e.sysEvent) {
    return (
      lifecycleIntent(e.sysEvent.eventType) ??
      touchIntent(e.sysEvent.eventType, () => ({ kind: 'tap' })) ?? { kind: 'ignore' }
    )
  }

  if (e.textEvent) {
    // Events for a container this app does not own are not ours to act on.
    const id = e.textEvent.containerID
    if (typeof id === 'number' && !OWNED_CONTAINERS.has(id)) return { kind: 'ignore' }
    return touchIntent(e.textEvent.eventType, () => ({ kind: 'tap' })) ?? { kind: 'ignore' }
  }

  return { kind: 'ignore' }
}
