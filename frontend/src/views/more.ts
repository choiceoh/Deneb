// views/more.ts — Profile / settings hub.
//
// Mira's bottom-tab Profile page inspired this view: a stack of titled
// card sections, each containing icon-prefixed rows that either show a
// read-only value or navigate to a sub-view. Deneb's single-user /
// single-device model means most rows are status-only; the actionable
// rows are workspace shortcuts (calendar, gmail, memory, sessions).
//
// Layout matches `home.ts` so the user can hop between the two tabs and
// the visual grammar stays consistent.

import { ping, whoami, RpcError, type PingResult, type WhoamiResult } from '../rpc';
import { isCurrentHash, navigate, type Route } from '../router';

export async function renderMore(root: HTMLElement, initData: string): Promise<void> {
  const expectedHash = location.hash;
  root.innerHTML = '<div class="loading">로딩 중…</div>';
  try {
    const t0 = performance.now();
    const [user, pingResult] = await Promise.all([whoami(initData), ping(initData)]);
    if (!isCurrentHash(expectedHash)) return;
    const latencyMs = Math.round(performance.now() - t0);
    paint(root, user, pingResult, latencyMs);
  } catch (err) {
    if (!isCurrentHash(expectedHash)) return;
    const msg =
      err instanceof RpcError
        ? `${err.code} — ${err.message}`
        : err instanceof Error
          ? err.message
          : '알 수 없는 오류';
    paintError(root, `백엔드 호출 실패: ${msg}`);
  }
}

function paint(
  root: HTMLElement,
  user: WhoamiResult,
  pingResult: PingResult,
  latencyMs: number,
): void {
  root.innerHTML = '';

  const header = document.createElement('div');
  header.className = 'brand-header';
  header.innerHTML = `
    <span class="brand-name">Deneb</span>
    <span class="brand-badge" title="비서실장형 단일 에이전트">✓</span>
  `;
  root.appendChild(header);

  // Profile card — who you are + what's running.
  const profileLabel = document.createElement('div');
  profileLabel.className = 'section-label';
  profileLabel.textContent = '프로필';
  root.appendChild(profileLabel);

  const userLabel =
    [user.firstName, user.lastName].filter(Boolean).join(' ') ||
    (user.username ? `@${user.username}` : `id=${user.id}`);
  const profile = document.createElement('div');
  profile.className = 'section-card';
  profile.appendChild(
    buildInfoRow('icon-tile-violet', '👤', '사용자', userLabel),
  );
  profile.appendChild(
    buildInfoRow('icon-tile-pink', '🧠', 'LLM 모델', pingResult.model || '—'),
  );
  profile.appendChild(
    buildInfoRow('icon-tile-purple', '🌐', '언어', '한국어'),
  );
  root.appendChild(profile);

  // Workspace shortcuts — domain destinations that used to live on home.
  const workspaceLabel = document.createElement('div');
  workspaceLabel.className = 'section-label';
  workspaceLabel.textContent = '워크스페이스';
  root.appendChild(workspaceLabel);

  const workspace = document.createElement('div');
  workspace.className = 'section-card';
  workspace.appendChild(
    buildNavRow('icon-tile-blue', '📅', '일정', '다가오는 회의', {
      name: 'calendar',
    }),
  );
  workspace.appendChild(
    buildNavRow('icon-tile-red', '📧', 'Gmail 트리아지', '미처리 메일', {
      name: 'inbox',
    }),
  );
  workspace.appendChild(
    buildNavRow('icon-tile-amber', '🧩', '메모리 검색', '위키 / 메모리', {
      name: 'memory',
    }),
  );
  workspace.appendChild(
    buildNavRow('icon-tile-teal', '🗂', '최근 세션', '실행 중 / 완료', {
      name: 'sessions',
    }),
  );
  root.appendChild(workspace);

  // Status / about — diagnostics + branding.
  const aboutLabel = document.createElement('div');
  aboutLabel.className = 'section-label';
  aboutLabel.textContent = '상태';
  root.appendChild(aboutLabel);

  const about = document.createElement('div');
  about.className = 'section-card';
  about.appendChild(
    buildInfoRow('icon-tile-slate', '⚙️', '버전', `v${pingResult.version || '?'}`),
  );
  about.appendChild(
    buildInfoRow('icon-tile-green', '📡', '응답 시간', `${latencyMs}ms`),
  );
  root.appendChild(about);
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
  target: Route,
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
  btn.addEventListener('click', () => navigate(target));
  return btn;
}

function paintError(root: HTMLElement, message: string): void {
  root.innerHTML = '';
  const banner = document.createElement('div');
  banner.className = 'error';
  banner.textContent = message;
  root.appendChild(banner);
}
