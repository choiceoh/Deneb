import { fetchStatus } from './deneb'
import { loadStoredSettings, normalizeSettings, resolveSettings, saveSettings, type GlanceSettings } from './settings'

// The phone half of the plugin.
//
// An Even Hub plugin is a WebView the phone app hosts; whatever it draws into
// the DOM is what the operator sees on the phone, while the glasses get the
// bridge containers. This file had no counterpart for a long time — `index.html`
// shipped an empty <body> — so the plugin looked broken on the phone and the
// ONLY way to reconfigure it was to mint a fresh QR seed or repack the .ehpk.
// When the gateway moves (Tailscale addresses do), that is a bad place to be.
//
// Text entry belongs here and nowhere else: the glasses have four gestures and
// no keyboard, so a settings screen on the HUD was never an option.

type Status = {
  /** What the glasses loop is doing, in the operator's words. */
  line: string
  tone: 'ok' | 'warn' | 'bad'
}

let statusEl: HTMLElement | null = null
let settingsSavedCb: ((next: GlanceSettings) => void) | null = null
let interpretCb: ((on: boolean, lang: string) => void) | null = null
let imuCb: ((label: string) => void) | null = null

/** onImuCapture wires the phone's labelled-motion buttons to the glasses. */
export function onImuCapture(cb: (label: string) => void): void {
  imuCb = cb
}

/** onInterpretToggle wires the phone's start/stop to the glasses loop. */
export function onInterpretToggle(cb: (on: boolean, lang: string) => void): void {
  interpretCb = cb
}

/** onSettingsSaved lets the glasses loop pick up an edit without a restart. */
export function onSettingsSaved(cb: (next: GlanceSettings) => void): void {
  settingsSavedCb = cb
}

/** setPhoneStatus mirrors the glasses loop's state onto the phone. */
export function setPhoneStatus(status: Status): void {
  if (!statusEl) return
  statusEl.textContent = status.line
  statusEl.dataset.tone = status.tone
}

const CSS = `
  :root { color-scheme: dark; }
  body {
    margin: 0; padding: 20px 16px 40px;
    background: #0b0d10; color: #e8ecf1;
    font: 15px/1.55 -apple-system, BlinkMacSystemFont, "Apple SD Gothic Neo", "Noto Sans KR", sans-serif;
  }
  h1 { font-size: 19px; margin: 0 0 4px; letter-spacing: -0.01em; }
  .sub { color: #8b97a6; font-size: 13px; margin: 0 0 20px; }
  .status {
    border-radius: 10px; padding: 12px 14px; margin-bottom: 22px;
    background: #16202b; border-left: 3px solid #3d566e; font-size: 14px;
  }
  .status[data-tone="ok"]  { border-left-color: #2ecc71; }
  .status[data-tone="warn"]{ border-left-color: #e6b422; }
  .status[data-tone="bad"] { border-left-color: #e05263; }
  label { display: block; font-size: 13px; color: #a9b6c4; margin: 0 0 6px; }
  .field { margin-bottom: 16px; }
  input {
    width: 100%; box-sizing: border-box; padding: 12px 13px;
    border-radius: 9px; border: 1px solid #2a3543; background: #121820;
    color: #e8ecf1; font-size: 16px; /* 16px: iOS zooms the page below this */
  }
  input:focus { outline: none; border-color: #4a90d9; }
  .row { display: flex; gap: 10px; }
  button {
    flex: 1; padding: 13px 12px; border-radius: 9px; border: 0;
    font-size: 15px; font-weight: 600; cursor: pointer;
  }
  .primary { background: #2f6fed; color: #fff; }
  .ghost { background: #1c2733; color: #cbd6e2; }
  button:disabled { opacity: 0.5; }
  .result { margin-top: 14px; font-size: 13px; min-height: 18px; white-space: pre-wrap; }
  .ok { color: #2ecc71; } .bad { color: #e05263; }
  .hint { margin-top: 26px; color: #6f7c8c; font-size: 12px; line-height: 1.7; }
  code { background: #16202b; padding: 1px 5px; border-radius: 4px; }
`

/**
 * renderPhoneUI draws the phone screen.
 *
 * Called BEFORE the glasses bridge is awaited on purpose. `waitForEvenAppBridge`
 * is a top-level await, so anything after it is dead until the glasses show up
 * — and a phone screen that stays blank whenever the glasses are not connected
 * is exactly useless at the moment the operator needs to fix the connection.
 */
export function renderPhoneUI(): void {
  if (typeof document === 'undefined' || document.getElementById('deneb-phone')) return

  const style = document.createElement('style')
  style.textContent = CSS
  document.head.appendChild(style)

  const root = document.createElement('div')
  root.id = 'deneb-phone'
  root.innerHTML = `
    <h1>Deneb Glance</h1>
    <p class="sub">안경 HUD가 읽어올 게이트웨이 설정</p>
    <div class="status" id="dn-status" data-tone="warn">상태 확인 중…</div>
    <div class="field">
      <label for="dn-base">게이트웨이 주소</label>
      <input id="dn-base" type="url" inputmode="url" autocomplete="off"
             autocapitalize="off" spellcheck="false" placeholder="http://100.x.x.x:18789" />
    </div>
    <div class="field">
      <label for="dn-token">브리지 토큰</label>
      <input id="dn-token" type="password" autocomplete="off"
             autocapitalize="off" spellcheck="false" placeholder="DENEB_EVEN_G2_BRIDGE_TOKEN" />
    </div>
    <div class="row">
      <button class="primary" id="dn-save">저장</button>
      <button class="ghost" id="dn-test">연결 테스트</button>
    </div>
    <div class="result" id="dn-result"></div>
    <h2 style="font-size:15px;margin:26px 0 8px">통역</h2>
    <p class="sub" style="margin:0 0 10px">
      안경 마이크를 열어 상대 말을 자막으로 띄웁니다. 제스처 예산이 없어 시작·중지는 여기에 둡니다.
    </p>
    <div class="field">
      <label for="dn-lang">번역 대상 언어</label>
      <input id="dn-lang" value="ko" autocapitalize="off" spellcheck="false" />
    </div>
    <div class="row">
      <button class="primary" id="dn-interp-on">통역 시작</button>
      <button class="ghost" id="dn-interp-off">중지</button>
    </div>
    <details style="margin-top:26px">
      <summary style="color:#8b97a6;font-size:13px;cursor:pointer">IMU 기록 (헤드 제스처 개발용)</summary>
      <p class="sub" style="margin:10px 0">
        안경을 쓴 채 버튼을 누르고 3초간 해당 동작을 하세요. SDK 가 축의 단위도 규약도 문서화하지 않아,
        검출기는 실제 기록을 모은 뒤에야 쓸 수 있습니다.
      </p>
      <div class="row" id="dn-imu"></div>
    </details>
    <p class="hint">
      QR 시드(<code>?seed=</code>)로 처음 넣은 값이 여기 그대로 보입니다.
      게이트웨이 주소가 바뀌면 QR을 다시 만들지 말고 여기서 고치세요 — 저장하면 안경이 바로 새 주소로 다시 읽습니다.
    </p>
  `
  document.body.appendChild(root)

  statusEl = root.querySelector<HTMLElement>('#dn-status')
  const baseInput = root.querySelector<HTMLInputElement>('#dn-base')!
  const tokenInput = root.querySelector<HTMLInputElement>('#dn-token')!
  const saveBtn = root.querySelector<HTMLButtonElement>('#dn-save')!
  const testBtn = root.querySelector<HTMLButtonElement>('#dn-test')!
  const result = root.querySelector<HTMLElement>('#dn-result')!

  // Whatever the QR seed already stored is what the operator should see and be
  // able to correct — an empty form would look like nothing was ever set.
  void resolveSettings().then((s) => {
    const current = s.baseUrl || s.token ? s : loadStoredSettings()
    baseInput.value = current.baseUrl
    tokenInput.value = current.token
  })

  const readForm = (): GlanceSettings =>
    normalizeSettings({ baseUrl: baseInput.value, token: tokenInput.value })

  saveBtn.addEventListener('click', () => {
    const next = readForm()
    if (!next.baseUrl || !next.token) {
      result.className = 'result bad'
      result.textContent = '주소와 토큰을 모두 입력하세요.'
      return
    }
    saveSettings(next)
    baseInput.value = next.baseUrl
    result.className = 'result ok'
    result.textContent = '저장했습니다. 안경이 새 설정으로 다시 읽습니다.'
    settingsSavedCb?.(next)
  })

  const langInput = root.querySelector<HTMLInputElement>('#dn-lang')!
  root.querySelector<HTMLButtonElement>('#dn-interp-on')!.addEventListener('click', () => {
    interpretCb?.(true, langInput.value.trim() || 'ko')
    result.className = 'result ok'
    result.textContent = '통역을 시작했습니다. 안경에서 탭하면 중지됩니다.'
  })
  root.querySelector<HTMLButtonElement>('#dn-interp-off')!.addEventListener('click', () => {
    interpretCb?.(false, langInput.value.trim() || 'ko')
    result.className = 'result'
    result.textContent = '통역을 중지했습니다.'
  })

  const imuRow = root.querySelector<HTMLElement>('#dn-imu')!
  for (const label of ['정지', '끄덕임', '도리도리', '걷기']) {
    const b = document.createElement('button')
    b.className = 'ghost'
    b.textContent = label
    b.addEventListener('click', () => {
      imuCb?.(label)
      result.className = 'result'
      result.textContent = `"${label}" 3초 기록 중…`
    })
    imuRow.appendChild(b)
  }

  testBtn.addEventListener('click', () => {
    const probe = readForm()
    if (!probe.baseUrl || !probe.token) {
      result.className = 'result bad'
      result.textContent = '주소와 토큰을 모두 입력하세요.'
      return
    }
    testBtn.disabled = true
    result.className = 'result'
    result.textContent = '확인 중…'
    // Deliberately tests the values IN THE FORM, not the saved ones, so a wrong
    // address can be checked before it is committed.
    void fetchStatus(probe)
      .then((st) => {
        result.className = 'result ok'
        result.textContent = [
          st.ok ? '브리지 OK' : '브리지 이상',
          st.chatReady ? '챗 준비됨' : '챗 미준비',
          `세션 ${st.session || 'glasses:main'}`,
        ].join(' · ')
      })
      .catch((err: unknown) => {
        result.className = 'result bad'
        result.textContent = `실패: ${err instanceof Error ? err.message : String(err)}`
      })
      .finally(() => {
        testBtn.disabled = false
      })
  })
}
