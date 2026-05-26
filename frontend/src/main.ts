// main.ts — Deneb Mini App entry. PoC: prove the Telegram launch + auth +
// gateway round-trip path works, then render a card showing the
// authenticated user and backend health. Domain features (Gmail triage,
// memory search, etc.) layer on top in later PRs.

import './styles.css';
import { ping, whoami, RpcError, type PingResult, type WhoamiResult } from './rpc';

const root = document.getElementById('app')!;

function applyThemeFromTelegram(tg: WebApp): void {
  const params = tg.themeParams;
  const map: Record<string, string | undefined> = {
    '--tg-bg': params.bg_color,
    '--tg-text': params.text_color,
    '--tg-hint': params.hint_color,
    '--tg-link': params.link_color,
    '--tg-button': params.button_color,
    '--tg-button-text': params.button_text_color,
    '--tg-secondary-bg': params.secondary_bg_color,
  };
  const docStyle = document.documentElement.style;
  for (const [name, value] of Object.entries(map)) {
    if (value) docStyle.setProperty(name, value);
  }
}

function renderError(message: string): void {
  root.innerHTML = '';
  const banner = document.createElement('div');
  banner.className = 'error';
  banner.textContent = message;
  root.appendChild(banner);
  const muted = document.createElement('div');
  muted.className = 'muted';
  muted.textContent = 'Deneb Mini App — open me from Telegram';
  root.appendChild(muted);
}

function renderReady(user: WhoamiResult, pingResult: PingResult, latencyMs: number): void {
  root.innerHTML = '';
  const h1 = document.createElement('h1');
  h1.textContent = 'Deneb';
  root.appendChild(h1);

  const auth = document.createElement('div');
  auth.className = 'card';
  auth.innerHTML = `
    <div class="row"><span class="label">Authenticated</span><span class="value ok">✓</span></div>
    <div class="row"><span class="label">Name</span><span class="value"></span></div>
    <div class="row"><span class="label">Username</span><span class="value"></span></div>
  `;
  const nameRow = auth.querySelectorAll('.value')[1] as HTMLElement;
  const userRow = auth.querySelectorAll('.value')[2] as HTMLElement;
  nameRow.textContent =
    [user.firstName, user.lastName].filter(Boolean).join(' ') || `id=${user.id}`;
  userRow.textContent = user.username ? `@${user.username}` : '—';
  root.appendChild(auth);

  const backend = document.createElement('div');
  backend.className = 'card';
  backend.innerHTML = `
    <div class="row"><span class="label">Backend</span><span class="value ok">ok</span></div>
    <div class="row"><span class="label">Version</span><span class="value"></span></div>
    <div class="row"><span class="label">Latency</span><span class="value"></span></div>
  `;
  const valueCells = backend.querySelectorAll('.value');
  (valueCells[1] as HTMLElement).textContent = pingResult.version || '(none)';
  (valueCells[2] as HTMLElement).textContent = `${latencyMs} ms`;
  root.appendChild(backend);

  const button = document.createElement('button');
  button.className = 'primary';
  button.textContent = '새로고침';
  button.addEventListener('click', () => {
    void boot();
  });
  root.appendChild(button);

  const muted = document.createElement('div');
  muted.className = 'muted';
  muted.textContent = `query=${(window.Telegram?.WebApp?.initData ?? '').length}B · 인증=${new Date(user.authDateMs).toLocaleString()}`;
  root.appendChild(muted);
}

async function boot(): Promise<void> {
  const tg = window.Telegram?.WebApp;
  if (!tg) {
    renderError(
      '이 페이지는 Telegram 클라이언트 안에서 열어야 합니다. 봇 메뉴 버튼을 눌러주세요.',
    );
    return;
  }

  tg.ready();
  applyThemeFromTelegram(tg);

  const initData = tg.initData;
  if (!initData) {
    renderError(
      'Telegram 이 launch 데이터를 보내지 않았습니다. 메뉴 버튼을 다시 눌러보세요.',
    );
    return;
  }

  try {
    const t0 = performance.now();
    const [user, pingResult] = await Promise.all([whoami(initData), ping(initData)]);
    const latencyMs = Math.round(performance.now() - t0);
    renderReady(user, pingResult, latencyMs);
  } catch (err) {
    const msg =
      err instanceof RpcError
        ? `${err.code} — ${err.message}`
        : err instanceof Error
          ? err.message
          : '알 수 없는 오류';
    renderError(`백엔드 호출 실패: ${msg}`);
  }
}

void boot();
