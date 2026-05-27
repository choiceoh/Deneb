// views/topic_new.ts — Create a new Telegram forum topic.
//
// Reached from the topics list (header right "+ 새 토픽"). Single text
// input for the topic name. On submit we call miniapp.topics.create
// against the active forum supergroup (set via /use-forum) and pop back
// to the topics list so the user can see the new thread surface on
// their next inbound message.
//
// Mirrors wiki_new.ts in shape — same field/flash/actions row idiom —
// so the two creation flows feel like the same control surface.

import { createTopic } from '../topics';
import { errorMessage } from '../format';
import { navigate } from '../router';
import { buildViewHeader } from './ui';

const MAX_NAME_RUNES = 128;

export function renderTopicNew(root: HTMLElement, initData: string): void {
  root.innerHTML = '';

  root.appendChild(
    buildViewHeader({
      title: 'new topic',
      left: { label: '← topics', onClick: () => navigate({ name: 'sessions' }) },
    }),
  );

  const wrap = document.createElement('div');
  wrap.className = 'card wiki-edit';

  const nameInput = buildFieldInput(wrap, '토픽 이름 (필수)', '');
  nameInput.maxLength = MAX_NAME_RUNES;
  nameInput.placeholder = '예: 주간 회고';

  const flash = document.createElement('div');
  flash.className = 'wiki-edit-flash';
  wrap.appendChild(flash);

  const actions = document.createElement('div');
  actions.className = 'wiki-edit-actions';

  const cancelBtn = document.createElement('button');
  cancelBtn.type = 'button';
  cancelBtn.className = 'action-button action-secondary';
  cancelBtn.textContent = '취소';
  cancelBtn.addEventListener('click', () => navigate({ name: 'sessions' }));
  actions.appendChild(cancelBtn);

  const createBtn = document.createElement('button');
  createBtn.type = 'button';
  createBtn.className = 'action-button action-primary';
  createBtn.textContent = '만들기';
  createBtn.addEventListener('click', () => {
    void submit(initData, nameInput.value, createBtn, cancelBtn, flash);
  });
  actions.appendChild(createBtn);
  wrap.appendChild(actions);

  // Enter on the name field is the natural submit shortcut for a
  // single-input form; matches Telegram's own "new topic" sheet.
  nameInput.addEventListener('keydown', (e) => {
    if (e.key === 'Enter') {
      e.preventDefault();
      createBtn.click();
    }
  });

  root.appendChild(wrap);

  setTimeout(() => nameInput.focus(), 0);
}

function buildFieldInput(wrap: HTMLElement, label: string, value: string): HTMLInputElement {
  const labelEl = document.createElement('label');
  labelEl.className = 'wiki-edit-field';

  const textEl = document.createElement('span');
  textEl.className = 'wiki-edit-field-label';
  textEl.textContent = label;
  labelEl.appendChild(textEl);

  const input = document.createElement('input');
  input.className = 'wiki-edit-input';
  input.type = 'text';
  input.value = value;
  labelEl.appendChild(input);

  wrap.appendChild(labelEl);
  return input;
}

async function submit(
  initData: string,
  rawName: string,
  createBtn: HTMLButtonElement,
  cancelBtn: HTMLButtonElement,
  flash: HTMLElement,
): Promise<void> {
  const name = rawName.trim();
  if (!name) {
    flash.textContent = '토픽 이름을 입력하세요.';
    return;
  }

  createBtn.disabled = true;
  cancelBtn.disabled = true;
  flash.textContent = '';
  createBtn.textContent = '만드는 중…';

  try {
    await createTopic(initData, { name });
    // Pop straight back to the topics list. The new topic won't appear
    // there until the user (or bot) sends a message into it — sessions
    // are populated by inbound traffic, not topic creation alone — so
    // we don't try to deep-link into a per-topic transcript here.
    navigate({ name: 'sessions' });
  } catch (err) {
    createBtn.disabled = false;
    cancelBtn.disabled = false;
    createBtn.textContent = '만들기';
    flash.textContent = `생성 실패: ${errorMessage(err)}`;
  }
}
