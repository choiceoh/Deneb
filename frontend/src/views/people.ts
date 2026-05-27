// views/people.ts — Counterparty directory from recent mail traffic.
//
// "Who's in motion right now" — Gmail senders over the last 30 days,
// sorted by message volume. Each card shows: display name + email,
// message count badge, last subject preview, last seen relative time.
// Tap a card to drill into the per-person detail view (wiki facts +
// recent messages from that sender).

import { listPeople, type PersonRow } from '../people';
import { formatRpcError, relativeTime } from '../format';
import { isCurrentHash, navigate } from '../router';
import { buildErrorBanner, buildLoadingNode, buildViewHeader } from './ui';

export async function renderPeople(
  root: HTMLElement,
  initData: string,
): Promise<void> {
  const expectedHash = location.hash;
  root.innerHTML = '';

  root.appendChild(
    buildViewHeader({
      title: 'people',
      left: { label: '← more', onClick: () => navigate({ name: 'more' }) },
      right: { label: 'refresh', onClick: () => void renderPeople(root, initData) },
    }),
  );

  const status = buildLoadingNode('메일 발신자 집계 중…');
  root.appendChild(status);

  try {
    const { people, windowDays, scannedCount } = await listPeople(initData);
    if (!isCurrentHash(expectedHash)) return;
    status.remove();

    const summary = document.createElement('div');
    summary.className = 'muted people-summary';
    summary.textContent = `최근 ${windowDays}일 · 메일 ${scannedCount.toLocaleString(
      'ko-KR',
    )}건 스캔 · 발신자 ${people.length.toLocaleString('ko-KR')}명`;
    root.appendChild(summary);

    if (people.length === 0) {
      const empty = document.createElement('div');
      empty.className = 'empty-state';
      empty.textContent = '최근 메일이 없거나 발신자를 식별할 수 없습니다';
      root.appendChild(empty);
      return;
    }
    for (const person of people) {
      root.appendChild(buildPersonCard(person));
    }
  } catch (err) {
    if (!isCurrentHash(expectedHash)) return;
    status.remove();
    root.appendChild(buildErrorBanner(`사람들 로드 실패: ${formatRpcError(err)}`));
  }
}

function buildPersonCard(p: PersonRow): HTMLElement {
  const card = document.createElement('button');
  card.type = 'button';
  card.className = 'person-card';
  card.addEventListener('click', () =>
    navigate({ name: 'personDetail', email: p.email }),
  );

  const top = document.createElement('div');
  top.className = 'person-card-top';

  const ident = document.createElement('div');
  ident.className = 'person-card-ident';

  const name = document.createElement('div');
  name.className = 'person-card-name';
  name.textContent = p.name || p.email;
  ident.appendChild(name);

  if (p.name && p.name !== p.email) {
    const emailEl = document.createElement('div');
    emailEl.className = 'person-card-email';
    emailEl.textContent = p.email;
    ident.appendChild(emailEl);
  }
  top.appendChild(ident);

  const badge = document.createElement('span');
  badge.className = 'person-card-count';
  badge.textContent = `${p.messageCount.toLocaleString('ko-KR')}건`;
  top.appendChild(badge);
  card.appendChild(top);

  if (p.lastSubject) {
    const sub = document.createElement('div');
    sub.className = 'person-card-subject';
    sub.textContent = p.lastSubject;
    card.appendChild(sub);
  }

  if (p.lastSeen) {
    const foot = document.createElement('div');
    foot.className = 'person-card-foot';
    foot.textContent = `마지막 ${relativeTime(p.lastSeen)}`;
    card.appendChild(foot);
  }

  return card;
}
