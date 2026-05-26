// views/detail.ts — single-message view with [읽음] / [보관] / [닫기] actions.
//
// Auto-fires miniapp.gmail.mark_read on entry (fire-and-forget) so the
// message stops cluttering the inbox after the user opens it. Explicit
// [읽음] button is kept anyway — it's a no-op cost-wise and makes the
// model of "I've actioned this" visible.

import { archive, getMessage, markRead, type GmailMessageDetail } from '../gmail';
import { RpcError } from '../rpc';
import { navigate } from '../router';
import { humanSize, relativeTime } from '../format';

export async function renderDetail(
  root: HTMLElement,
  initData: string,
  messageId: string,
): Promise<void> {
  root.innerHTML = '<div class="loading">메일 불러오는 중…</div>';

  try {
    const msg = await getMessage(initData, messageId);
    paint(root, initData, msg);
    // Auto mark-read in the background. Ignore the result; if it fails
    // the row keeps its UNREAD style on next list refresh and the user
    // can hit [읽음] explicitly.
    void markRead(initData, messageId).catch(() => undefined);
  } catch (err) {
    const msgText =
      err instanceof RpcError
        ? `${err.code} — ${err.message}`
        : err instanceof Error
          ? err.message
          : '알 수 없는 오류';
    root.innerHTML = '';
    const banner = document.createElement('div');
    banner.className = 'error';
    banner.textContent = `메일 로드 실패: ${msgText}`;
    root.appendChild(banner);
    const back = document.createElement('button');
    back.className = 'primary';
    back.textContent = '← 받은 편지함으로';
    back.addEventListener('click', () => navigate({ name: 'inbox' }));
    root.appendChild(back);
  }
}

function paint(root: HTMLElement, initData: string, msg: GmailMessageDetail): void {
  root.innerHTML = '';

  const header = document.createElement('div');
  header.className = 'view-header';
  header.innerHTML = `
    <button class="link-button">← 받은 편지함</button>
    <span class="view-title">메일</span>
    <span></span>
  `;
  header.querySelector('button')!.addEventListener('click', () => navigate({ name: 'inbox' }));
  root.appendChild(header);

  const meta = document.createElement('div');
  meta.className = 'card email-meta';
  meta.innerHTML = `
    <div class="email-subject"></div>
    <div class="row"><span class="label">From</span><span class="value"></span></div>
    <div class="row"><span class="label">To</span><span class="value"></span></div>
    <div class="row"><span class="label">When</span><span class="value"></span></div>
  `;
  const subjectEl = meta.querySelector('.email-subject') as HTMLElement;
  subjectEl.textContent = msg.subject || '(제목 없음)';
  const valueCells = meta.querySelectorAll('.value');
  (valueCells[0] as HTMLElement).textContent = msg.from;
  (valueCells[1] as HTMLElement).textContent = msg.to || '—';
  (valueCells[2] as HTMLElement).textContent = relativeTime(msg.date);
  root.appendChild(meta);

  if (msg.attachments && msg.attachments.length > 0) {
    const attBox = document.createElement('div');
    attBox.className = 'card attachments';
    const label = document.createElement('div');
    label.className = 'label';
    label.textContent = '첨부';
    attBox.appendChild(label);
    for (const att of msg.attachments) {
      const chip = document.createElement('div');
      chip.className = 'attachment-chip';
      chip.innerHTML = `
        <span class="attachment-icon">📎</span>
        <span class="attachment-name"></span>
        <span class="attachment-size"></span>
      `;
      (chip.querySelector('.attachment-name') as HTMLElement).textContent =
        att.filename || '(이름없음)';
      (chip.querySelector('.attachment-size') as HTMLElement).textContent = humanSize(att.size);
      attBox.appendChild(chip);
    }
    root.appendChild(attBox);
  }

  const body = document.createElement('pre');
  body.className = 'email-body';
  body.textContent = msg.body || '(본문 없음)';
  root.appendChild(body);

  if (msg.bodyTotal > msg.body.length) {
    // Server truncated. Show the original length so the user knows.
    const note = document.createElement('div');
    note.className = 'muted';
    note.textContent = `(${msg.bodyTotal.toLocaleString('ko-KR')}자 중 일부만 표시)`;
    root.appendChild(note);
  }

  const actions = document.createElement('div');
  actions.className = 'action-bar';
  root.appendChild(actions);

  const readBtn = makeAction('📌 읽음', 'secondary', async () => {
    readBtn.disabled = true;
    try {
      await markRead(initData, msg.id);
      flash(actions, '✓ 읽음 처리');
    } catch (err) {
      flash(actions, `읽음 실패: ${err instanceof Error ? err.message : err}`);
      readBtn.disabled = false;
    }
  });
  actions.appendChild(readBtn);

  const archBtn = makeAction('📁 보관', 'secondary', async () => {
    archBtn.disabled = true;
    try {
      await archive(initData, msg.id);
      navigate({ name: 'inbox' });
    } catch (err) {
      flash(actions, `보관 실패: ${err instanceof Error ? err.message : err}`);
      archBtn.disabled = false;
    }
  });
  actions.appendChild(archBtn);

  const closeBtn = makeAction('← 닫기', 'primary', () => navigate({ name: 'inbox' }));
  actions.appendChild(closeBtn);
}

function makeAction(
  label: string,
  variant: 'primary' | 'secondary',
  onClick: () => void | Promise<void>,
): HTMLButtonElement {
  const btn = document.createElement('button');
  btn.className = `action-button action-${variant}`;
  btn.textContent = label;
  btn.addEventListener('click', () => {
    void onClick();
  });
  return btn;
}

function flash(host: HTMLElement, message: string): void {
  const existing = host.parentElement?.querySelector('.flash');
  if (existing) existing.remove();
  const f = document.createElement('div');
  f.className = 'flash';
  f.textContent = message;
  host.after(f);
  setTimeout(() => f.remove(), 2500);
}
