import {
  waitForEvenAppBridge,
  TextContainerProperty,
  TextContainerUpgrade,
  CreateStartUpPageContainer,
  OsEventTypeList,
} from '@evenrealities/even_hub_sdk'

import { resolveSettings, type GlanceSettings } from './settings'
import {
  fetchGlance,
  fetchStatus,
  formatGeneratedLabel,
  pageTitle,
  type GlancePage,
  type GlancePayload,
} from './deneb'

const bridge = await waitForEvenAppBridge()

type Screen = 'setup' | 'page' | 'status'

const PAGE_ORDER = ['home', 'cal', 'urgent', 'todo'] as const

let settings: GlanceSettings = { baseUrl: '', token: '' }
let screen: Screen = 'setup'
let busy = false
let pages: GlancePage[] = []
let pageIndex = 0
let lastGenerated = ''
let lastCached = false

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
  const textEvent = event.textEvent
  if (!textEvent || textEvent.containerID !== 1) return

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

async function boot(): Promise<void> {
  settings = await resolveSettings()
  if (needsSetup(settings)) {
    screen = 'setup'
    await show(setupCopy())
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
    await show(setupCopy())
    return
  }
  if (screen === 'status') {
    screen = 'page'
    await renderCurrentPage()
    return
  }
  await refreshGlance(true)
}

async function onSwipeNext(): Promise<void> {
  if (busy) return
  if (screen === 'setup') return
  if (screen === 'status') {
    screen = 'page'
    pageIndex = 0
    await renderCurrentPage()
    return
  }
  if (pageIndex < pages.length - 1) {
    pageIndex += 1
    await renderCurrentPage()
    return
  }
  await showStatus()
}

async function onSwipePrev(): Promise<void> {
  if (busy) return
  if (screen === 'setup') return
  if (screen === 'status') {
    screen = 'page'
    pageIndex = Math.max(0, pages.length - 1)
    await renderCurrentPage()
    return
  }
  if (pageIndex > 0) {
    pageIndex -= 1
    await renderCurrentPage()
    return
  }
  // wrap: first page + swipe up → status (optional) — plan says prev goes previous;
  // at home, stay on home.
  await renderCurrentPage()
}

async function showStatus(): Promise<void> {
  if (busy) return
  if (needsSetup(settings)) {
    screen = 'setup'
    await show(setupCopy())
    return
  }
  busy = true
  screen = 'status'
  try {
    const st = await fetchStatus(settings)
    const host = settings.baseUrl.replace(/^https?:\/\//, '')
    await show(
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
    await show(`Deneb · 상태\n\n오류: ${msg}\n\n탭=페이지로`)
  } finally {
    busy = false
  }
}

async function refreshGlance(fresh: boolean): Promise<void> {
  if (busy) return
  busy = true
  const keepId = currentPageId()
  try {
    settings = await resolveSettings()
    if (needsSetup(settings)) {
      screen = 'setup'
      await show(setupCopy())
      return
    }
    screen = 'page'
    await show('Deneb\n\n불러오는 중…')
    const payload = await fetchGlance(settings, { fresh })
    applyPayload(payload, keepId)
    await renderCurrentPage()
  } catch (err) {
    const msg = err instanceof Error ? err.message : String(err)
    await show(`Deneb\n\n오류: ${msg}\n\n탭=재시도 / ↓다음`)
  } finally {
    busy = false
  }
}

function applyPayload(payload: GlancePayload, keepId: string): void {
  pages = sortPages(payload.pages)
  if (pages.length === 0) {
    pages = [{ id: 'home', title: '오늘', text: payload.text }]
  }
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
    title: '오늘',
    text: '지금 볼 일정·긴급·할 일은 없어요.',
  }
  const title = page.title || pageTitle(page.id)
  const stamp = formatGeneratedLabel(lastGenerated, lastCached)
  const nav = `${pageIndex + 1}/${Math.max(pages.length, 1)}`
  const footer = stamp
    ? `↓다음 · 탭새로고침 · ${stamp}`
    : `↓다음 · ↑이전 · ${nav}`
  await show(`Deneb · ${title}\n\n${page.text}\n\n${footer}`)
}

async function show(content: string): Promise<void> {
  await bridge.textContainerUpgrade(
    new TextContainerUpgrade({
      containerID: 1,
      containerName: 'main',
      content,
    }),
  )
}