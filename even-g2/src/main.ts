import {
  waitForEvenAppBridge,
  TextContainerProperty,
  TextContainerUpgrade,
  CreateStartUpPageContainer,
  RebuildPageContainer,
  ListContainerProperty,
  ListItemContainerProperty,
  OsEventTypeList,
} from '@evenrealities/even_hub_sdk'

import { resolveSettings, type GlanceSettings } from './settings'
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
const AUTO_REFRESH_MS = 45_000
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
let uiMode: 'text' | 'list' = 'text'

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
  if (event.listEvent) {
    const le = event.listEvent
    switch (le.eventType) {
      case OsEventTypeList.CLICK_EVENT:
      case undefined:
        void openDetail(le.currentSelectItemIndex ?? 0)
        break
      case OsEventTypeList.DOUBLE_CLICK_EVENT:
        bridge.shutDownPageContainer(1)
        break
      case OsEventTypeList.SCROLL_BOTTOM_EVENT:
        void onSwipeNext()
        break
      case OsEventTypeList.SCROLL_TOP_EVENT:
        void onSwipePrev()
        break
    }
    return
  }

  const textEvent = event.textEvent
  if (!textEvent) return
  // Accept either main text (id 1) or header (id 2) when mixed layouts exist.
  if (textEvent.containerID !== 1 && textEvent.containerID !== 2) return

  switch (textEvent.eventType) {
    case OsEventTypeList.CLICK_EVENT:
    case undefined:
      void onTap()
      break
    case OsEventTypeList.DOUBLE_CLICK_EVENT:
      bridge.shutDownPageContainer(1)
      break
    case OsEventTypeList.SCROLL_BOTTOM_EVENT:
      void onSwipeNext()
      break
    case OsEventTypeList.SCROLL_TOP_EVENT:
      void onSwipePrev()
      break
  }
})

void boot()
startAutoRefresh()

async function boot(): Promise<void> {
  settings = await resolveSettings()
  if (needsSetup(settings)) {
    screen = 'setup'
    await showText(setupCopy())
    return
  }
  screen = 'page'
  pageIndex = 0
  await refreshGlance(false)
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
  // On list pages, CLICK is handled by listEvent. Text tap = refresh.
  await refreshGlance(true)
}

async function openDetail(index: number): Promise<void> {
  if (busy) return
  if (!LIST_PAGE_IDS.has(currentPageId())) return
  if (!items.length) return
  const i = Math.max(0, Math.min(items.length - 1, index | 0))
  detailIndex = i
  screen = 'detail'
  await showText(`Deneb · 상세\n\n${formatAlertDetail(items[i])}`)
}

async function onSwipeNext(): Promise<void> {
  if (busy) return
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
  let found = -1
  for (let i = pageIndex + 1; i < pages.length; i++) {
    if (!isPageEmpty(pages[i])) {
      found = i
      break
    }
  }
  if (found >= 0) {
    pageIndex = found
    await renderCurrentPage()
    return
  }
  await showStatus()
}

async function onSwipePrev(): Promise<void> {
  if (busy) return
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
  let found = -1
  for (let i = pageIndex - 1; i >= 0; i--) {
    if (!isPageEmpty(pages[i])) {
      found = i
      break
    }
  }
  if (found >= 0) {
    pageIndex = found
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

async function refreshGlance(fresh: boolean): Promise<void> {
  if (busy) return
  busy = true
  const keepId = currentPageId()
  const keepDetailId = screen === 'detail' && detailIndex >= 0 ? items[detailIndex]?.id : ''
  try {
    settings = await resolveSettings()
    if (needsSetup(settings)) {
      screen = 'setup'
      await showText(setupCopy())
      return
    }
    if (screen !== 'detail') screen = 'page'
    await showText('Deneb\n\n불러오는 중…')
    const payload = await fetchGlance(settings, { fresh })
    applyPayload(payload, keepId)
    if (keepDetailId) {
      const idx = items.findIndex((it) => it.id === keepDetailId)
      if (idx >= 0) {
        detailIndex = idx
        screen = 'detail'
        await showText(`Deneb · 상세\n\n${formatAlertDetail(items[idx])}`)
        return
      }
    }
    screen = 'page'
    detailIndex = -1
    await renderCurrentPage()
  } catch (err) {
    const msg = err instanceof Error ? err.message : String(err)
    await showText(`Deneb\n\n오류: ${msg}\n\n탭=재시도 / ↓다음`)
  } finally {
    busy = false
  }
}

function applyPayload(payload: GlancePayload, keepId: string): void {
  pages = sortPages(payload.pages)
  if (pages.length === 0) {
    pages = [{ id: 'home', title: '알림', text: payload.text }]
  }
  items = payload.items || []
  lastGenerated = payload.generated || ''
  lastCached = !!payload.cached
  const idx = pages.findIndex((p) => p.id === keepId)
  pageIndex = idx >= 0 ? idx : 0
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

async function showAlertList(title: string): Promise<void> {
  const labels = items.map(listLabel)
  const stamp = formatGeneratedLabel(lastGenerated, lastCached)
  const header = [
    `Deneb · ${title}`,
    `${items.length}건 · 탭=상세`,
    stamp ? stamp : '↓다음페이지',
  ].join('\n')

  const headerText = new TextContainerProperty({
    xPosition: 0,
    yPosition: 0,
    width: 576,
    height: 72,
    borderWidth: 0,
    borderColor: 5,
    paddingLength: 4,
    containerID: 2,
    containerName: 'header',
    content: header,
    isEventCapture: 0,
    zOrderIndex: 0,
  })
  const list = new ListContainerProperty({
    xPosition: 0,
    yPosition: 72,
    width: 576,
    height: 216,
    borderWidth: 0,
    borderColor: 5,
    paddingLength: 2,
    containerID: 1,
    containerName: 'alerts',
    isEventCapture: 1,
    zOrderIndex: 1,
    itemContainer: new ListItemContainerProperty({
      itemCount: labels.length,
      itemWidth: 560,
      isItemSelectBorderEn: 1,
      itemName: labels,
    }),
  })
  uiMode = 'list'
  const ok = await bridge.rebuildPageContainer(
    new RebuildPageContainer({
      containerTotalNum: 2,
      textObject: [headerText],
      listObject: [list],
    }),
  )
  if (!ok) {
    // Fallback: text list if list rebuild fails on host.
    await showText(
      `Deneb · ${title}\n\n${labels.join('\n')}\n\n탭=새로고침 · ↓다음`,
    )
  }
}

async function showText(content: string): Promise<void> {
  if (uiMode === 'list') {
    uiMode = 'text'
    await bridge.rebuildPageContainer(
      new RebuildPageContainer({
        containerTotalNum: 1,
        textObject: [
          new TextContainerProperty({
            xPosition: 0,
            yPosition: 0,
            width: 576,
            height: 288,
            borderWidth: 0,
            borderColor: 5,
            paddingLength: 4,
            containerID: 1,
            containerName: 'main',
            content,
            isEventCapture: 1,
          }),
        ],
      }),
    )
    return
  }
  uiMode = 'text'
  await bridge.textContainerUpgrade(
    new TextContainerUpgrade({
      containerID: 1,
      containerName: 'main',
      content,
    }),
  )
}

function startAutoRefresh(): void {
  setInterval(() => {
    if (busy || screen === 'setup') return
    if (screen === 'status') return
    void refreshGlance(false)
  }, AUTO_REFRESH_MS)
}

function isPageEmpty(p: GlancePage | undefined): boolean {
  if (!p) return true
  if (p.id === 'home') return false
  return !!p.empty
}
