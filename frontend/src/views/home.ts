// views/home.ts — landing screen: auth + backend health + entry points to
// domain views (currently just Gmail triage).

import { ping, whoami, type PingResult, type WhoamiResult } from '../rpc';
import { formatRpcError } from '../format';
import { isCurrentHash, navigate } from '../router';
import { readAppSettings } from '../app_settings';
import { buildErrorBanner } from './ui';
import { icon, type IconName } from '../icons';

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

  // Replaces the old "비서실장형 단일 에이전트" tagline with a
  // greeting + live status read. Two short lines = identity + signal
  // without marketing copy.
  const subtitle = document.createElement('div');
  subtitle.className = 'brand-subtitle';
  subtitle.textContent = greeting(user.firstName);
  root.appendChild(subtitle);

  const meta = document.createElement('div');
  meta.className = 'brand-meta';
  const model = prettyModel(pingResult.model);
  meta.textContent = model ? `${model} · 정상` : '연결 안 됨';
  root.appendChild(meta);

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
    buildNavRow('icon-tile-blue', 'calendar', '일정', '다가오는 회의 · D-15분 알림', () =>
      navigate({ name: 'calendar' }),
    ),
  );
  shortcuts.appendChild(
    buildNavRow('icon-tile-red', 'mail', 'Gmail 트리아지', '최근 미처리 메일', () =>
      navigate({ name: 'inbox' }),
    ),
  );
  shortcuts.appendChild(
    buildNavRow('icon-tile-amber', 'memory', '메모리 검색', '위키 / 메모리 빠른 검색', () =>
      navigate({ name: 'memory' }),
    ),
  );
  shortcuts.appendChild(
    buildNavRow('icon-tile-teal', 'sessions', '최근 세션', '실행 중 / 완료', () =>
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

function buildNavRow(
  tileClass: string,
  iconName: IconName,
  label: string,
  sub: string,
  onClick: () => void,
): HTMLButtonElement {
  const btn = document.createElement('button');
  btn.type = 'button';
  btn.className = 'profile-row profile-row-nav';
  // icon() returns trusted SVG markup from our own registry — no user
  // input — so it's safe to put inside innerHTML.
  btn.innerHTML = `
    <span class="icon-tile ${tileClass}">${icon(iconName)}</span>
    <span class="profile-row-text">
      <span class="profile-row-label"></span>
      <span class="profile-row-sub"></span>
    </span>
    <span class="profile-row-chevron">›</span>
  `;
  (btn.querySelector('.profile-row-label') as HTMLElement).textContent = label;
  (btn.querySelector('.profile-row-sub') as HTMLElement).textContent = sub;
  btn.addEventListener('click', onClick);
  return btn;
}

// greeting picks a Korean phase-of-day phrase. Suffixes the user's first
// name when we have one so the screen reads as a personal landing rather
// than a generic dashboard.
function greeting(firstName?: string): string {
  const h = new Date().getHours();
  const phase = h < 5 ? '안녕하세요' : h < 12 ? '좋은 아침' : h < 18 ? '좋은 오후' : '좋은 저녁';
  const who = firstName?.trim();
  return who ? `${phase}, ${who}` : phase;
}

// prettyModel strips the provider prefix and uppercases the model name
// so e.g. "zai/glm-5.1" reads as "GLM-5.1" — the part the operator
// actually cares about. Returns "" when the gateway hasn't reported
// a model yet (caller falls back to "연결 안 됨").
function prettyModel(raw?: string): string {
  if (!raw) return '';
  const trimmed = raw.split('/').pop()?.trim() ?? '';
  return trimmed.toUpperCase();
}

function renderHomeError(root: HTMLElement, message: string): void {
  root.innerHTML = '';
  root.appendChild(buildErrorBanner(message));
  const muted = document.createElement('div');
  muted.className = 'muted';
  muted.textContent = 'Deneb Mini App';
  root.appendChild(muted);
}
