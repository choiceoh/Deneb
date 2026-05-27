// views/person_detail.ts — Full-screen profile for a single person.
//
// Reached by tapping a card in 더보기 > 👤 사람들. Composes three
// existing data sources into one view so the user can see "everything
// Deneb knows about this counterparty" in one place:
//
//   1. Header card — name + email + a quick "최근 30일 N건 · 마지막 N일 전"
//   2. Wiki facts (graphify CLI snapshot, free-form)
//   3. Wiki hits — hand-curated wiki pages mentioning this person/org
//   4. Recent messages — tap a row to open the existing mail detail
//
// Backend reuse: no new RPCs. `miniapp.gmail.sender_context` fans out
// to Gmail + wiki + graphify already; `miniapp.gmail.list_recent` with
// a `from:<email>` query gives the per-message rows. Both fire in
// parallel with Promise.all so the page paints once instead of in
// stages — the body of work is bounded (3-second sender_context + a
// single Gmail Search call).

import { listRecent, senderContext, type GmailMessageRow, type SenderContext } from '../gmail';
import { formatRpcError, relativeTime, shortFrom } from '../format';
import { isCurrentHash, navigate } from '../router';
import { buildErrorBanner, buildLoadingNode, buildViewHeader } from './ui';

const recentMessagesLimit = 30;

export async function renderPersonDetail(
  root: HTMLElement,
  initData: string,
  email: string,
): Promise<void> {
  const expectedHash = location.hash;
  root.innerHTML = '';

  root.appendChild(
    buildViewHeader({
      title: 'person',
      left: { label: '← people', onClick: () => navigate({ name: 'people' }) },
      right: {
        label: 'refresh',
        onClick: () => void renderPersonDetail(root, initData, email),
      },
    }),
  );

  const status = buildLoadingNode('정보 모으는 중…');
  root.appendChild(status);

  try {
    // sender_context already does Gmail-search + wiki + graphify
    // internally and shouldn't fail catastrophically (each subsource
    // surfaces via Notices). list_recent is the per-message rows we
    // want to render as clickable cards. Both fire in parallel.
    const fromQuery = `from:${quoteEmail(email)}`;
    const [context, recent] = await Promise.all([
      senderContext(initData, email),
      listRecent(initData, { query: fromQuery, limit: recentMessagesLimit }),
    ]);
    if (!isCurrentHash(expectedHash)) return;
    status.remove();
    paint(root, email, context, recent.messages);
  } catch (err) {
    if (!isCurrentHash(expectedHash)) return;
    status.remove();
    root.appendChild(buildErrorBanner(`사람 정보 로드 실패: ${formatRpcError(err)}`));
  }
}

// quoteEmail wraps the email in quotes so any operator characters in
// the local part (`-`, `:`, spaces in display-name encodings) are
// treated as part of the address rather than Gmail search syntax.
// Mirrors what the backend does in sender_context.go before its own
// Search call.
function quoteEmail(email: string): string {
  return `"${email}"`;
}

function paint(
  root: HTMLElement,
  email: string,
  context: SenderContext,
  recent: GmailMessageRow[],
): void {
  root.appendChild(buildIdentCard(email, context));

  // Notices from the backend (wiki disabled, gmail unavailable, etc.).
  // These surface here as a single subtle banner so the user knows
  // partial data is partial.
  if (context.notices && context.notices.length > 0) {
    const note = document.createElement('div');
    note.className = 'muted person-detail-notices';
    note.textContent = context.notices.join(' · ');
    root.appendChild(note);
  }

  if (context.wikiFacts && context.wikiFacts.trim()) {
    root.appendChild(buildFactsCard(context.wikiFacts));
  }

  if (context.wikiHits && context.wikiHits.length > 0) {
    root.appendChild(buildWikiHits(context.wikiHits));
  }

  root.appendChild(buildRecentMessages(recent, context.recent?.windowDays ?? 30));
}

function buildIdentCard(email: string, context: SenderContext): HTMLElement {
  const card = document.createElement('div');
  card.className = 'card person-detail-ident';

  const name = document.createElement('div');
  name.className = 'person-detail-name';
  name.textContent = context.displayName || shortFrom(context.sender) || email;
  card.appendChild(name);

  const emailEl = document.createElement('div');
  emailEl.className = 'person-detail-email';
  emailEl.textContent = email;
  card.appendChild(emailEl);

  // Aggregate stat line — only render when sender_context found
  // something. Avoids a "최근 30일 0건" claim if the backend failed
  // to reach Gmail at all (in which case context.recent is omitted).
  if (context.recent) {
    const stat = document.createElement('div');
    stat.className = 'person-detail-stat';
    const parts: string[] = [];
    const countLabel = context.recent.count.toLocaleString('ko-KR');
    const truncated = (context.recent.count >= 50) ? '+' : '';
    parts.push(`최근 ${context.recent.windowDays}일 · ${countLabel}${truncated}건`);
    if (context.recent.lastReceivedAt) {
      parts.push(`마지막 ${relativeTime(context.recent.lastReceivedAt)}`);
    }
    stat.textContent = parts.join(' · ');
    card.appendChild(stat);
  }

  return card;
}

function buildFactsCard(facts: string): HTMLElement {
  const card = document.createElement('div');
  card.className = 'card person-detail-facts';

  const label = document.createElement('div');
  label.className = 'card-label';
  label.textContent = '위키 그래프 요약';
  card.appendChild(label);

  // Graphify output is structured but human-written — preserve
  // whitespace so the bullet structure stays readable. textContent
  // (not innerHTML) keeps it safe.
  const body = document.createElement('pre');
  body.className = 'person-detail-facts-body';
  body.textContent = facts.trim();
  card.appendChild(body);

  return card;
}

function buildWikiHits(hits: SenderContext['wikiHits']): HTMLElement {
  const wrap = document.createElement('div');

  const label = document.createElement('div');
  label.className = 'section-label';
  label.textContent = '관련 위키';
  wrap.appendChild(label);

  const card = document.createElement('div');
  card.className = 'section-card';
  for (const hit of hits) {
    const row = document.createElement('button');
    row.type = 'button';
    row.className = 'wiki-hit-row';

    const title = document.createElement('div');
    title.className = 'wiki-hit-title';
    title.textContent = hit.title || hit.path;
    row.appendChild(title);

    if (hit.summary) {
      const sum = document.createElement('div');
      sum.className = 'wiki-hit-summary';
      sum.textContent = hit.summary;
      row.appendChild(sum);
    }

    const meta = document.createElement('div');
    meta.className = 'wiki-hit-meta';
    const parts: string[] = [];
    if (hit.category) parts.push(`#${hit.category}`);
    parts.push(hit.path);
    meta.textContent = parts.join(' · ');
    row.appendChild(meta);

    row.addEventListener('click', () => navigate({ name: 'wikiPage', path: hit.path }));
    card.appendChild(row);
  }
  wrap.appendChild(card);
  return wrap;
}

function buildRecentMessages(rows: GmailMessageRow[], windowDays: number): HTMLElement {
  const wrap = document.createElement('div');

  const label = document.createElement('div');
  label.className = 'section-label';
  label.textContent = `최근 메일 (${windowDays}일)`;
  wrap.appendChild(label);

  if (rows.length === 0) {
    const empty = document.createElement('div');
    empty.className = 'empty-state';
    empty.textContent = '최근 메일이 없습니다';
    wrap.appendChild(empty);
    return wrap;
  }

  for (const row of rows) {
    wrap.appendChild(buildMessageRow(row));
  }
  return wrap;
}

function buildMessageRow(row: GmailMessageRow): HTMLElement {
  const card = document.createElement('button');
  card.type = 'button';
  card.className = 'person-msg-row';
  if (row.isUnread) card.classList.add('person-msg-row-unread');

  const top = document.createElement('div');
  top.className = 'person-msg-row-top';

  const subject = document.createElement('span');
  subject.className = 'person-msg-row-subject';
  subject.textContent = row.subject || '(제목 없음)';
  top.appendChild(subject);

  const time = document.createElement('span');
  time.className = 'person-msg-row-time';
  time.textContent = relativeTime(row.date);
  top.appendChild(time);
  card.appendChild(top);

  if (row.snippet) {
    const snippet = document.createElement('div');
    snippet.className = 'person-msg-row-snippet';
    snippet.textContent = row.snippet;
    card.appendChild(snippet);
  }

  card.addEventListener('click', () => navigate({ name: 'detail', messageId: row.id }));
  return card;
}
