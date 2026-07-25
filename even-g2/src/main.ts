import {
  waitForEvenAppBridge,
  TextContainerProperty,
  TextContainerUpgrade,
  CreateStartUpPageContainer,
} from '@evenrealities/even_hub_sdk'

import { resolveSettings, type GlanceSettings } from './settings'
import { advanceCursor, clampCursor as clampCursorTo, nextDelayMs, payloadSignature, resolveSelectionIndex } from './refresh'
import { dispatchHubEvent } from './events'
import {
  fetchGlance,
  fetchStatus,
  formatAlertDetail,
  formatGeneratedLabel,
  listLabel,
  pageTitle,
  type GlanceItem,
  type GlancePage,
  type GlancePayload,
} from './deneb'

const bridge = await waitForEvenAppBridge()

type Screen = 'setup' | 'page' | 'detail' | 'status'

const PAGE_ORDER = ['home', 'alerts', 'cal', 'todo'] as const
const LIST_PAGE_IDS = new Set(['home', 'alerts'])

let settings: GlanceSettings = { baseUrl: '', token: '' }
let screen: Screen = 'setup'
let busy = false
let pages: GlancePage[] = []
let items: GlanceItem[] = []
let pageIndex = 0
let detailIndex = -1
let lastGenerated = ''
let lastCached = false
/**
 * Which alert the cursor is on, on an alert page.
 *
 * The app owns this instead of the host's list container. The simulator run
 * (30176035571) showed why: the host moves a list's own selection on a swipe
 * and then tells the app NOTHING — no scroll event at the end of the list, and
 * no `currentSelectItemIndex` on the click. So a host-owned list could neither
 * page away nor report what the wearer had actually highlighted, and a tap
 * always opened alert #1. An app-owned cursor on a plain text container makes
 * both behaviours ours, and therefore testable.
 */
let listCursor = 0
let refreshTimer: ReturnType<typeof setTimeout> | undefined
let consecutiveFailures = 0
let lastSignature = ''
let stopped = false
let paused = false

const mainText = new TextContainerProperty({
  xPosition: 0,
  yPosition: 0,
  width: 576,
  height: 288,
  borderWidth: 0,
  borderColor: 5,
  paddingLength: 4,
  containerID: 1,
  containerName: 'main',
  content: 'Deneb\n\n설정 확인 중…',
  isEventCapture: 1,
})

const started = await bridge.createStartUpPageContainer(
  new CreateStartUpPageContainer({
    containerTotalNum: 1,
    textObject: [mainText],
  }),
)
if (started !== 0) {
  console.error('createStartUpPageContainer failed', started)
}

bridge.onEvenHubEvent((event) => {
  // Mapping lives in events.ts as a pure function so every host event shape —
  // including the lifecycle events the simulator cannot inject — is unit
  // testable. This block only executes the intent.
  const intent = dispatchHubEvent(event)
  switch (intent.kind) {
    case 'tap':
      void onTap()
      break
    case 'openDetail':
      void openDetail(intent.index)
      break
    case 'nextPage':
      void onSwipeNext()
      break
    case 'prevPage':
      void onSwipePrev()
      break
    case 'shutdown':
      shutdown()
      break
    case 'stopLoop':
      stopLoop()
      break
    case 'pause':
      pauseLoop()
      break
    case 'resume':
      void resumeLoop()
      break
    case 'ignore':
      break
  }
})

void boot()

async function boot(): Promise<void> {
  settings = await resolveSettings()
  if (needsSetup(settings)) {
    screen = 'setup'
    await showText(setupCopy())
    // Still schedule: the wearer may seed the app while it is open, and the
    // setup screen's own poll is what notices.
    scheduleRefresh()
    return
  }
  screen = 'page'
  pageIndex = 0
  await refreshGlance(false)
  scheduleRefresh()
}

/** stopLoop cancels the background poll for good. */
function stopLoop(): void {
  stopped = true
  clearRefreshTimer()
}

/** pauseLoop cancels the pending poll but leaves the loop resumable. */
function pauseLoop(): void {
  paused = true
  clearRefreshTimer()
}

async function resumeLoop(): Promise<void> {
  if (stopped) return
  paused = false
  // Coming back to the foreground, the wearer wants current data, not whatever
  // was on screen when they looked away.
  await refreshGlance(false, true)
  scheduleRefresh()
}

function clearRefreshTimer(): void {
  if (refreshTimer !== undefined) {
    clearTimeout(refreshTimer)
    refreshTimer = undefined
  }
}

function shutdown(): void {
  stopLoop()
  bridge.shutDownPageContainer(1)
}

function needsSetup(s: GlanceSettings): boolean {
  return !s.baseUrl || !s.token
}

function setupCopy(): string {
  return [
    'Deneb Glance',
    '',
    '설정 필요 (QR 시드)',
    'evenhub qr --url',
    '"http://<lan>:5173/?seed=<…>"',
    '또는 pack 시 runtime-config.json',
    '',
    '탭=재확인 / 더블탭=종료',
  ].join('\n')
}

async function onTap(): Promise<void> {
  if (screen === 'setup') {
    settings = await resolveSettings()
    if (!needsSetup(settings)) {
      screen = 'page'
      pageIndex = 0
      await refreshGlance(true)
      scheduleRefresh()
      return
    }
    await showText(setupCopy())
    return
  }
  if (screen === 'detail') {
    screen = 'page'
    detailIndex = -1
    await renderCurrentPage()
    return
  }
  if (screen === 'status') {
    screen = 'page'
    await renderCurrentPage()
    return
  }
  // On an alert page a tap opens what the CURSOR is on — the primary action,
  // and now unambiguous because the app owns the cursor. Everywhere else a tap
  // is a manual refresh.
  if (onAlertPage()) {
    await openDetail(clampCursor())
    return
  }
  await refreshGlance(true)
  // A manual refresh re-anchors the schedule: after a backoff the next poll
  // would otherwise still be minutes away even though the link just worked.
  scheduleRefresh()
}

/**
 * openDetail shows one alert full-screen.
 *
 * NO `busy` guard, deliberately. It reads `items` out of memory and touches no
 * network, so gating it on an in-flight poll only served to DROP the wearer's
 * tap: the smoke harness caught exactly that (run 30171967100 — the tap was
 * delivered as a listEvent, the screen never changed, and the next tap 2.5s
 * later worked). A poll runs every 45s and may take up to the 10s deadline, so
 * that is a wide window in which the primary interaction silently does nothing.
 *
 * What the guard was really protecting against — a poll landing mid-read and
 * yanking the wearer back to the list — is handled in refreshGlance instead, by
 * re-reading the live screen after the fetch rather than trusting its entry
 * snapshot.
 */
async function openDetail(index: unknown): Promise<void> {
  if (!LIST_PAGE_IDS.has(currentPageId())) return
  // Normally called with the app's own cursor. The absent-index fallback still
  // matters because the host may deliver a `listEvent` (it did while alerts
  // were a list container, always WITHOUT currentSelectItemIndex) — opening the
  // top alert beats a dead tap. A present-but-nonsensical index is refused.
  const i = resolveSelectionIndex(index, items.length)
  if (i < 0) return
  detailIndex = i
  screen = 'detail'
  await showText(`Deneb · 상세\n\n${formatAlertDetail(items[i])}`)
}

async function onSwipeNext(): Promise<void> {
  // No `busy` guard: paging between already-loaded pages is local. See openDetail.
  if (screen === 'setup') return
  if (screen === 'detail') {
    screen = 'page'
    detailIndex = -1
    await renderCurrentPage()
    return
  }
  if (screen === 'status') {
    screen = 'page'
    pageIndex = 0
    await renderCurrentPage()
    return
  }
  // On an alert page the swipe walks the cursor DOWN the alerts first, and
  // only leaves the page once it is on the last one. That boundary belongs to
  // the app now: the host list used to keep it and emit nothing, which stranded
  // every page after the alerts.
  if (onAlertPage()) {
    const moved = advanceCursor(listCursor, items.length, 1)
    if (moved !== 'page') {
      listCursor = moved
      await renderCurrentPage()
      return
    }
  }
  let found = -1
  for (let i = pageIndex + 1; i < pages.length; i++) {
    if (!isPageEmpty(pages[i])) {
      found = i
      break
    }
  }
  if (found >= 0) {
    pageIndex = found
    listCursor = 0
    await renderCurrentPage()
    return
  }
  await showStatus()
}

async function onSwipePrev(): Promise<void> {
  // No `busy` guard: paging between already-loaded pages is local. See openDetail.
  if (screen === 'setup') return
  if (screen === 'detail') {
    screen = 'page'
    detailIndex = -1
    await renderCurrentPage()
    return
  }
  if (screen === 'status') {
    screen = 'page'
    const last = [...pages.keys()].reverse().find((i) => !isPageEmpty(pages[i]))
    pageIndex = last ?? Math.max(0, pages.length - 1)
    await renderCurrentPage()
    return
  }
  // Symmetric: walk the cursor back up the alerts before leaving the page.
  if (onAlertPage()) {
    const moved = advanceCursor(listCursor, items.length, -1)
    if (moved !== 'page') {
      listCursor = moved
      await renderCurrentPage()
      return
    }
  }
  let found = -1
  for (let i = pageIndex - 1; i >= 0; i--) {
    if (!isPageEmpty(pages[i])) {
      found = i
      break
    }
  }
  if (found >= 0) {
    pageIndex = found
    listCursor = 0
    await renderCurrentPage()
    return
  }
  await renderCurrentPage()
}

async function showStatus(): Promise<void> {
  if (busy) return
  if (needsSetup(settings)) {
    screen = 'setup'
    await showText(setupCopy())
    return
  }
  busy = true
  screen = 'status'
  try {
    const st = await fetchStatus(settings)
    const host = settings.baseUrl.replace(/^https?:\/\//, '')
    await showText(
      [
        'Deneb · 상태',
        '',
        st.ok ? '브리지 OK' : '브리지 이상',
        st.chatReady ? '챗 준비됨' : '챗 미준비',
        `세션 ${st.session || 'glasses:main'}`,
        host,
        '',
        '↓홈 · ↑이전 · 탭=페이지',
      ].join('\n'),
    )
  } catch (err) {
    const msg = err instanceof Error ? err.message : String(err)
    await showText(`Deneb · 상태\n\n오류: ${msg}\n\n탭=페이지로`)
  } finally {
    busy = false
  }
}

/**
 * refreshGlance pulls a new glance and redraws.
 *
 * `silent` marks a BACKGROUND poll (the timer). A background poll must be
 * invisible: no loading text, no redraw when nothing changed, no error screen.
 * The old loop wrote "불러오는 중…" over the display every 45 seconds — in the
 * wearer's field of view, and on top of a detail they were reading — then
 * rebuilt the container even when the payload was byte-identical.
 */
async function refreshGlance(fresh: boolean, silent = false): Promise<void> {
  if (busy) return
  busy = true
  const keepId = currentPageId()
  const keepDetailId = screen === 'detail' && detailIndex >= 0 ? items[detailIndex]?.id : ''
  try {
    settings = await resolveSettings()
    if (needsSetup(settings)) {
      if (screen !== 'setup') {
        screen = 'setup'
        await showText(setupCopy())
      }
      return
    }
    if (!silent) {
      if (screen !== 'detail') screen = 'page'
      await showText('Deneb\n\n불러오는 중…')
    }
    const payload = await fetchGlance(settings, { fresh })
    consecutiveFailures = 0

    // Re-read the LIVE screen, not the snapshot taken before the request. The
    // wearer can tap or swipe while a poll is in flight — now that navigation
    // is no longer blocked by `busy`, that is expected — and a background poll
    // must land on where they are NOW, not where they were 10 seconds ago.
    const liveDetailId = screen === 'detail' && detailIndex >= 0 ? items[detailIndex]?.id : ''
    const anchorId = currentPageId() || keepId
    const anchorDetailId = liveDetailId || keepDetailId

    const signature = payloadSignature(payload)
    const unchanged = signature === lastSignature
    lastSignature = signature
    applyPayload(payload, anchorId)

    // Nothing moved and nobody asked — leave the screen exactly as it is.
    // This is what makes the background poll invisible.
    if (silent && unchanged) return

    if (anchorDetailId) {
      const idx = items.findIndex((it) => it.id === anchorDetailId)
      if (idx >= 0) {
        detailIndex = idx
        screen = 'detail'
        await showText(`Deneb · 상세\n\n${formatAlertDetail(items[idx])}`)
        return
      }
      // The alert being read is gone. On a background poll, say so instead of
      // silently swapping the wearer's screen for a list they did not ask for.
      if (silent) {
        await showText('Deneb · 상세\n\n이 알림은 처리됐습니다.\n\n탭=목록으로')
        screen = 'detail'
        detailIndex = -1
        return
      }
    }
    screen = 'page'
    detailIndex = -1
    await renderCurrentPage()
  } catch (err) {
    consecutiveFailures++
    // A background failure stays off the display: the wearer did not ask, and
    // an unreachable gateway would otherwise flash an error every cycle while
    // they are simply out of network range.
    if (silent) return
    const msg = err instanceof Error ? err.message : String(err)
    await showText(`Deneb\n\n오류: ${msg}\n\n탭=재시도 / ↓다음`)
  } finally {
    busy = false
  }
}

function applyPayload(payload: GlancePayload, keepId: string): void {
  // Which alert the cursor was on, by id — a poll must not move it under the
  // wearer just because the gateway reordered or dropped something above it.
  const cursorId = items[clampCursor()]?.id

  pages = sortPages(payload.pages)
  if (pages.length === 0) {
    pages = [{ id: 'home', title: '알림', text: payload.text }]
  }
  items = payload.items || []
  lastGenerated = payload.generated || ''
  lastCached = !!payload.cached
  const idx = pages.findIndex((p) => p.id === keepId)
  pageIndex = idx >= 0 ? idx : 0

  const movedTo = cursorId ? items.findIndex((it) => it.id === cursorId) : -1
  // Gone (handled/expired) → back to the top, which is the highest priority.
  listCursor = movedTo >= 0 ? movedTo : 0
}

function sortPages(raw: GlancePage[]): GlancePage[] {
  const byId = new Map(raw.map((p) => [p.id, p]))
  const ordered: GlancePage[] = []
  for (const id of PAGE_ORDER) {
    const p = byId.get(id)
    if (p) ordered.push(p)
  }
  for (const p of raw) {
    if (!PAGE_ORDER.includes(p.id as (typeof PAGE_ORDER)[number])) {
      ordered.push(p)
    }
  }
  return ordered
}

function currentPageId(): string {
  return pages[pageIndex]?.id || 'home'
}

async function renderCurrentPage(): Promise<void> {
  const page = pages[pageIndex] || {
    id: 'home',
    title: '알림',
    text: '새 알림 없음',
  }
  const title = page.title || pageTitle(page.id)
  if (LIST_PAGE_IDS.has(page.id) && items.length > 0) {
    await showAlertList(title)
    return
  }
  const stamp = formatGeneratedLabel(lastGenerated, lastCached)
  const nav = `${pageIndex + 1}/${Math.max(pages.length, 1)}`
  const footer = stamp
    ? `↓다음 · 탭새로고침 · ${stamp}`
    : `↓다음(빈칸건너뜀) · ${nav}`
  await showText(`Deneb · ${title}\n\n${page.text}\n\n${footer}`)
}

/**
 * showAlertList draws the alerts on a plain TEXT container with a cursor the
 * app controls, rather than handing the items to a host list container.
 *
 * The host list looked like the obvious fit and was the wrong choice on both
 * counts it was picked for:
 *
 *   - it never told the app which item was highlighted (`listEvent` carried no
 *     `currentSelectItemIndex`), so a tap always opened alert #1 no matter what
 *     the wearer had scrolled to — they read something they did not choose;
 *   - it kept the scroll to itself, emitting nothing at the end of the list, so
 *     the cal/todo pages could not be reached at all while alerts existed.
 *
 * Text containers do deliver scroll (`textEvent{eventType:2}`) and taps, so
 * moving the cursor into the app fixes both — and makes them assertable in the
 * smoke harness instead of being host behaviour nobody can verify.
 */
async function showAlertList(title: string): Promise<void> {
  const stamp = formatGeneratedLabel(lastGenerated, lastCached)
  const cursor = clampCursor()
  const lines = items.map((it, i) => `${i === cursor ? '▸' : ' '}${listLabel(it)}`)
  const position = items.length > 1 ? ` (${cursor + 1}/${items.length})` : ''
  await showText(
    [
      `Deneb · ${title}${position}`,
      stamp ? `${items.length}건 · ${stamp}` : `${items.length}건`,
      '',
      ...lines,
      '',
      items.length > 1 ? '탭=상세 · ↓다음알림' : '탭=상세 · ↓다음페이지',
    ].join('\n'),
  )
}

/** clampCursor is the app-state view of the pure guard in refresh.ts. */
function clampCursor(): number {
  return clampCursorTo(listCursor, items.length)
}

/** onAlertPage reports whether the current page draws the alert cursor. */
function onAlertPage(): boolean {
  return LIST_PAGE_IDS.has(currentPageId()) && items.length > 0
}

/**
 * showText updates the single text container the app draws everything into.
 *
 * There is exactly one container for the whole app now that alerts are text
 * too, so this is always an in-place upgrade — no rebuild, which is what keeps
 * a background poll from flickering the wearer's view.
 */
async function showText(content: string): Promise<void> {
  await bridge.textContainerUpgrade(
    new TextContainerUpgrade({
      containerID: 1,
      containerName: 'main',
      content,
    }),
  )
}

/**
 * scheduleRefresh drives the background poll as a self-rescheduling timeout
 * rather than a fixed interval, so the delay can grow while the gateway is
 * unreachable and the whole loop can be cancelled with one handle.
 */
function scheduleRefresh(): void {
  if (stopped || paused) return
  if (refreshTimer !== undefined) clearTimeout(refreshTimer)
  refreshTimer = setTimeout(() => {
    refreshTimer = undefined
    void runScheduledRefresh()
  }, nextDelayMs(consecutiveFailures))
}

async function runScheduledRefresh(): Promise<void> {
  if (stopped || paused) return
  // The status screen is a deliberate read; polling under it would swap the
  // wearer's screen out from under them.
  if (!busy && screen !== 'status') {
    if (needsSetup(settings)) {
      // Setup polling only re-reads local settings (the QR seed lands in
      // localStorage), so it must not count as a network failure.
      settings = await resolveSettings()
      if (!needsSetup(settings)) {
        screen = 'page'
        pageIndex = 0
        await refreshGlance(false)
      }
    } else {
      await refreshGlance(false, true)
    }
  }
  scheduleRefresh()
}

function isPageEmpty(p: GlancePage | undefined): boolean {
  if (!p) return true
  if (p.id === 'home') return false
  return !!p.empty
}
