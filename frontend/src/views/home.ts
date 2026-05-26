// views/home.ts — landing screen: auth + backend health + entry points to
// domain views (currently just Gmail triage).

import { ping, whoami, RpcError, type PingResult, type WhoamiResult } from '../rpc';
import { navigate } from '../router';

export async function renderHome(root: HTMLElement, initData: string): Promise<void> {
  root.innerHTML = '<div class="loading">로딩 중…</div>';
  try {
    const t0 = performance.now();
    const [user, pingResult] = await Promise.all([whoami(initData), ping(initData)]);
    const latencyMs = Math.round(performance.now() - t0);
    paint(root, initData, user, pingResult, latencyMs);
  } catch (err) {
    const msg =
      err instanceof RpcError
        ? `${err.code} — ${err.message}`
        : err instanceof Error
          ? err.message
          : '알 수 없는 오류';
    renderHomeError(root, `백엔드 호출 실패: ${msg}`);
  }
}

function paint(
  root: HTMLElement,
  initData: string,
  user: WhoamiResult,
  pingResult: PingResult,
  latencyMs: number,
): void {
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
  const valueCells = auth.querySelectorAll('.value');
  (valueCells[1] as HTMLElement).textContent =
    [user.firstName, user.lastName].filter(Boolean).join(' ') || `id=${user.id}`;
  (valueCells[2] as HTMLElement).textContent = user.username ? `@${user.username}` : '—';
  root.appendChild(auth);

  const backend = document.createElement('div');
  backend.className = 'card';
  backend.innerHTML = `
    <div class="row"><span class="label">Backend</span><span class="value ok">ok</span></div>
    <div class="row"><span class="label">Version</span><span class="value"></span></div>
    <div class="row"><span class="label">Latency</span><span class="value"></span></div>
  `;
  const backendCells = backend.querySelectorAll('.value');
  (backendCells[1] as HTMLElement).textContent = pingResult.version || '(none)';
  (backendCells[2] as HTMLElement).textContent = `${latencyMs} ms`;
  root.appendChild(backend);

  // Entry point for the Gmail triage flow. Tap → navigate('inbox').
  const gmail = document.createElement('button');
  gmail.className = 'entry-card';
  gmail.innerHTML = `
    <span class="entry-emoji">📧</span>
    <span class="entry-text">
      <span class="entry-title">Gmail 트리아지</span>
      <span class="entry-sub">최근 미처리 메일 확인 · 읽음/보관</span>
    </span>
    <span class="entry-chevron">›</span>
  `;
  gmail.addEventListener('click', () => navigate({ name: 'inbox' }));
  root.appendChild(gmail);

  const refresh = document.createElement('button');
  refresh.className = 'primary';
  refresh.textContent = '새로고침';
  refresh.addEventListener('click', () => {
    void renderHome(root, initData);
  });
  root.appendChild(refresh);

  const muted = document.createElement('div');
  muted.className = 'muted';
  muted.textContent = `query=${initData.length}B · 인증=${new Date(user.authDateMs).toLocaleString('ko-KR')}`;
  root.appendChild(muted);
}

function renderHomeError(root: HTMLElement, message: string): void {
  root.innerHTML = '';
  const banner = document.createElement('div');
  banner.className = 'error';
  banner.textContent = message;
  root.appendChild(banner);
  const muted = document.createElement('div');
  muted.className = 'muted';
  muted.textContent = 'Deneb Mini App';
  root.appendChild(muted);
}
