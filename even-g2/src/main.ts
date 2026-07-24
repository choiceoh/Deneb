import {
  waitForEvenAppBridge,
  TextContainerProperty,
  TextContainerUpgrade,
  CreateStartUpPageContainer,
  OsEventTypeList,
} from '@evenrealities/even_hub_sdk'

import { resolveSettings, type GlanceSettings } from './settings'
import { fetchGlance } from './deneb'

const bridge = await waitForEvenAppBridge()

let settings: GlanceSettings = { baseUrl: '', token: '' }
let mode: 'glance' | 'setup' = 'setup'
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
  }
})

void boot()

async function boot(): Promise<void> {
  settings = await resolveSettings()
  if (needsSetup(settings)) {
    mode = 'setup'
    await show(setupCopy())
    return
  }
  mode = 'glance'
  await refreshGlance()
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
  if (mode === 'setup') {
    settings = await resolveSettings()
    if (!needsSetup(settings)) {
      mode = 'glance'
      await refreshGlance()
      return
    }
    await show(setupCopy())
    return
  }
  await refreshGlance()
}

async function refreshGlance(): Promise<void> {
  if (busy) return
  busy = true
  try {
    settings = await resolveSettings()
    if (needsSetup(settings)) {
      mode = 'setup'
      await show(setupCopy())
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
