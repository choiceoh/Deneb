// views/home.ts — landing screen: auth + backend health + entry points to
// domain views (currently just Gmail triage).

import { ping, whoami, type PingResult, type WhoamiResult } from '../rpc';
import { formatRpcError } from '../format';
import { isCurrentHash, navigate } from '../router';
import { readAppSettings } from '../app_settings';
import { buildErrorBanner } from './ui';

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
    renderHomeError(root, `백엔드 호출 실패: ${formatRpcError(err)}`);
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

  const header = document.createElement('div');
  header.className = 'brand-header';
  header.innerHTML = `
    <span class="brand-name">Deneb</span>
    <span class="brand-status" title="모든 서비스 정상" aria-label="online"></span>
  `;
  root.appendChild(header);

  const subtitle = document.createElement('div');
  subtitle.className = 'brand-subtitle';
  subtitle.textContent = '비서실장형 단일 에이전트';
  root.appendChild(subtitle);

  // Status: just the current model. Version/latency stay in the muted
  // footer so the visible status stays minimal.
  const statusLabel = document.createElement('div');
  statusLabel.className = 'section-label';
  statusLabel.textContent = '상태';
  root.appendChild(statusLabel);

  const status = document.createElement('div');
  status.className = 'section-card';
  status.appendChild(
    buildInfoRow('icon-tile-pink', '🧠', '모델', pingResult.model || '—'),
  );
  root.appendChild(status);

  // Domain entry cards. Order is intentional, by product priority:
  //   1) calendar — time-pressured (D-15 pushes need immediate eyeballs)
  //   2) Gmail — steady-state triage
  //   3) memory + sessions — reference
  // Chat lives in Telegram itself, not the Mini App, so it's not an entry
  // here.
  const shortcutsLabel = document.createElement('div');
  shortcutsLabel.className = 'section-label';
  shortcutsLabel.textContent = '바로가기';
  root.appendChild(shortcutsLabel);

  const shortcuts = document.createElement('div');
  shortcuts.className = 'section-card';
  shortcuts.appendChild(
    buildNavRow('icon-tile-blue', '📅', '일정', '다가오는 회의 · D-15분 알림', () =>
      navigate({ name: 'calendar' }),
    ),
  );
  shortcuts.appendChild(
    buildNavRow('icon-tile-red', '📧', 'Gmail 트리아지', '최근 미처리 메일', () =>
      navigate({ name: 'inbox' }),
    ),
  );
  shortcuts.appendChild(
    buildNavRow('icon-tile-amber', '🧩', '메모리 검색', '위키 / 메모리 빠른 검색', () =>
      navigate({ name: 'memory' }),
    ),
  );
  shortcuts.appendChild(
    buildNavRow('icon-tile-teal', '🗂', '최근 세션', '실행 중 / 완료', () =>
      navigate({ name: 'sessions' }),
    ),
  );
  root.appendChild(shortcuts);

  const refresh = document.createElement('button');
  refresh.className = 'primary';
  refresh.textContent = '새로고침';
  refresh.addEventListener('click', () => {
    void renderHome(root, initData);
  });
  root.appendChild(refresh);

  if (readAppSettings().showDiagnostics) {
    // Diagnostics live in the muted footer so the visible status stays minimal.
    const muted = document.createElement('div');
    muted.className = 'muted';
    const userLabel =
      [user.firstName, user.lastName].filter(Boolean).join(' ') ||
      (user.username ? `@${user.username}` : `id=${user.id}`);
    muted.textContent = `${userLabel} · v${pingResult.version || '?'} · ${latencyMs}ms`;
    root.appendChild(muted);
  }
}

function buildInfoRow(
  tileClass: string,
  emoji: string,
  label: string,
  value: string,
): HTMLElement {
  const row = document.createElement('div');
  row.className = 'profile-row';
  row.innerHTML = `
    <span class="icon-tile ${tileClass}"></span>
    <span class="profile-row-label"></span>
    <span class="profile-row-value"></span>
  `;
  (row.querySelector('.icon-tile') as HTMLElement).textContent = emoji;
  (row.querySelector('.profile-row-label') as HTMLElement).textContent = label;
  (row.querySelector('.profile-row-value') as HTMLElement).textContent = value;
  return row;
}

function buildNavRow(
  tileClass: string,
  emoji: string,
  label: string,
  sub: string,
  onClick: () => void,
): HTMLButtonElement {
  const btn = document.createElement('button');
  btn.type = 'button';
  btn.className = 'profile-row profile-row-nav';
  btn.innerHTML = `
    <span class="icon-tile ${tileClass}"></span>
    <span class="profile-row-text">
      <span class="profile-row-label"></span>
      <span class="profile-row-sub"></span>
    </span>
    <span class="profile-row-chevron">›</span>
  `;
  (btn.querySelector('.icon-tile') as HTMLElement).textContent = emoji;
  (btn.querySelector('.profile-row-label') as HTMLElement).textContent = label;
  (btn.querySelector('.profile-row-sub') as HTMLElement).textContent = sub;
  btn.addEventListener('click', onClick);
  return btn;
}

function renderHomeError(root: HTMLElement, message: string): void {
  root.innerHTML = '';
  root.appendChild(buildErrorBanner(message));
  const muted = document.createElement('div');
  muted.className = 'muted';
  muted.textContent = 'Deneb Mini App';
  root.appendChild(muted);
}
