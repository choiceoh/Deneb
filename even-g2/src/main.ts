import {
  waitForEvenAppBridge,
  TextContainerProperty,
  TextContainerUpgrade,
  CreateStartUpPageContainer,
  OsEventTypeList,
} from '@evenrealities/even_hub_sdk'

import { loadSettings, type GlanceSettings } from './settings'
import { fetchGlance } from './deneb'

const bridge = await waitForEvenAppBridge()

let settings = loadSettings()
let mode: 'glance' | 'setup' = needsSetup(settings) ? 'setup' : 'glance'
let busy = false

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
  content: initialContent(mode),
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

if (mode === 'glance') {
  void refreshGlance()
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
  }
})

function needsSetup(s: GlanceSettings): boolean {
  return !s.baseUrl || !s.token
}

function initialContent(m: typeof mode): string {
  if (m === 'setup') {
    return [
      'Deneb Glance',
      '',
      '설정 필요',
      'WebView 콘솔에서:',
      "localStorage.setItem('deneb.even.g2.settings',",
      " JSON.stringify({baseUrl:'http://…',token:'…'}))",
      '',
      '탭=재확인 / 더블탭=종료',
    ].join('\n')
  }
  return 'Deneb\n\n불러오는 중…\n탭=새로고침 / 더블탭=종료'
}

async function onTap(): Promise<void> {
  if (mode === 'setup') {
    settings = loadSettings()
    if (!needsSetup(settings)) {
      mode = 'glance'
      await refreshGlance()
      return
    }
    await show(initialContent('setup'))
    return
  }
  await refreshGlance()
}

async function refreshGlance(): Promise<void> {
  if (busy) return
  busy = true
  try {
    settings = loadSettings()
    if (needsSetup(settings)) {
      mode = 'setup'
      await show(initialContent('setup'))
      return
    }
    await show('Deneb\n\n불러오는 중…')
    const text = await fetchGlance(settings)
    await show(`Deneb\n\n${text}\n\n탭=새로고침`)
  } catch (err) {
    const msg = err instanceof Error ? err.message : String(err)
    await show(`Deneb\n\n오류: ${msg}\n\n탭=재시도 / 더블탭=종료`)
  } finally {
    busy = false
  }
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
