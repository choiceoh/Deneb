// main.ts — Deneb Mini App entry + router shell.
//
// All real rendering lives in views/*.ts. This file:
//   1. Boots Telegram WebApp SDK and applies its theme params.
//   2. Validates that initData is present (otherwise show a friendly
//      "open from Telegram" banner).
//   3. Routes the current hash to the right view module and listens for
//      hashchange to re-render on navigation.
//   4. Manages Telegram's BackButton so it mirrors browser history.

import './styles.css';
import { parseRoute, navigate, type Route } from './router';
import { renderHome } from './views/home';
import { renderList } from './views/list';
import { renderDetail } from './views/detail';
import { renderMemory } from './views/memory';
import { renderSessions } from './views/sessions';

const root = document.getElementById('app')!;
let cachedInitData: string | null = null;

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

function syncBackButton(route: Route): void {
  const back = window.Telegram?.WebApp?.BackButton;
  if (!back) return;
  if (route.name === 'home') {
    back.hide();
  } else {
    back.show();
  }
}

async function dispatch(route: Route): Promise<void> {
  if (!cachedInitData) return;
  syncBackButton(route);
  switch (route.name) {
    case 'home':
      await renderHome(root, cachedInitData);
      return;
    case 'inbox':
      await renderList(root, cachedInitData);
      return;
    case 'detail':
      await renderDetail(root, cachedInitData, route.messageId);
      return;
    case 'memory':
      renderMemory(root, cachedInitData);
      return;
    case 'sessions':
      await renderSessions(root, cachedInitData);
      return;
  }
}

function handleHashChange(): void {
  const route = parseRoute(location.hash);
  void dispatch(route);
}

function boot(): void {
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
  cachedInitData = initData;

  // Wire Telegram's BackButton to the router. The back arrow then pops
  // detail → inbox → home in the order the user navigated.
  tg.BackButton?.onClick(() => {
    const route = parseRoute(location.hash);
    if (route.name === 'detail') navigate({ name: 'inbox' });
    else navigate({ name: 'home' });
  });

  window.addEventListener('hashchange', handleHashChange);
  void dispatch(parseRoute(location.hash));
}

boot();
