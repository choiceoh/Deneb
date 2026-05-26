// views/home.ts — landing screen: auth + backend health + entry points to
// domain views (currently just Gmail triage).

import { ping, whoami, RpcError, type PingResult, type WhoamiResult } from '../rpc';
import { isCurrentHash, navigate } from '../router';

export async function renderHome(root: HTMLElement, initData: string): Promise<void> {
  const expectedHash = location.hash;
  root.innerHTML = '<div class="loading">로딩 중…</div>';
  try {
    const t0 = performance.now();
    const [user, pingResult] = await Promise.all([whoami(initData), ping(initData)]);
    if (!isCurrentHash(expectedHash)) return;
    const latencyMs = Math.round(performance.now() - t0);
    paint(root, initData, user, pingResult, latencyMs);
  } catch (err) {
    if (!isCurrentHash(expectedHash)) return;
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

  // Status: just the current model. Auth/version/latency stay in the
  // muted footer below so they're available for diagnostics without
  // dominating the screen.
  const status = document.createElement('div');
  status.className = 'card';
  status.innerHTML = `<div class="row"><span class="label">모델</span><span class="value"></span></div>`;
  const modelCell = status.querySelector('.value') as HTMLElement;
  modelCell.textContent = pingResult.model || '—';
  root.appendChild(status);

  // Domain entry cards. Order matters — fastest-cadence at the top.
  root.appendChild(
    buildEntryCard('📧', 'Gmail 트리아지', '최근 미처리 메일 · 읽음/보관', () =>
      navigate({ name: 'inbox' }),
    ),
  );
  root.appendChild(
    buildEntryCard('🧠', '메모리 검색', '위키 / 메모리에서 빠른 검색', () =>
      navigate({ name: 'memory' }),
    ),
  );
  root.appendChild(
    buildEntryCard('🗂', '최근 세션', '실행 중 / 완료된 에이전트 세션', () =>
      navigate({ name: 'sessions' }),
    ),
  );

  const refresh = document.createElement('button');
  refresh.className = 'primary';
  refresh.textContent = '새로고침';
  refresh.addEventListener('click', () => {
    void renderHome(root, initData);
  });
  root.appendChild(refresh);

  // Diagnostics live in the muted footer so the visible status stays minimal.
  const muted = document.createElement('div');
  muted.className = 'muted';
  const userLabel =
    [user.firstName, user.lastName].filter(Boolean).join(' ') ||
    (user.username ? `@${user.username}` : `id=${user.id}`);
  muted.textContent = `${userLabel} · v${pingResult.version || '?'} · ${latencyMs}ms`;
  root.appendChild(muted);
}

function buildEntryCard(
  emoji: string,
  title: string,
  sub: string,
  onClick: () => void,
): HTMLButtonElement {
  const card = document.createElement('button');
  card.className = 'entry-card';
  card.innerHTML = `
    <span class="entry-emoji"></span>
    <span class="entry-text">
      <span class="entry-title"></span>
      <span class="entry-sub"></span>
    </span>
    <span class="entry-chevron">›</span>
  `;
  (card.querySelector('.entry-emoji') as HTMLElement).textContent = emoji;
  (card.querySelector('.entry-title') as HTMLElement).textContent = title;
  (card.querySelector('.entry-sub') as HTMLElement).textContent = sub;
  card.addEventListener('click', onClick);
  return card;
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
