// views/topic_new.ts — Create a new Telegram forum topic.
//
// Reached from the home menu's "new topic" item. Single text field
// (topic name). On submit we call miniapp.topics.create, which routes
// to the Bot API's createForumTopic against the chat the user previously
// bound via /use-forum. Telegram owns the topic data — once created it
// shows up in the supergroup's topic list automatically — so on success
// we just bounce to the topics view rather than maintain any local state.
//
// Failure paths:
//  - VALIDATION_FAILED (no active home) → inline error with /use-forum hint
//  - DEPENDENCY_FAILED (bot lost Manage Topics) → inline error with
//    Telegram-side fix instructions
//  - Any other RPC error → bare error message, user can retry

import { createTopic } from '../topics';
import { errorMessage, formatRpcError } from '../format';
import { navigate } from '../router';
import { buildViewHeader } from './ui';

export function renderTopicNew(root: HTMLElement, initData: string): void {
  root.innerHTML = '';

  root.appendChild(
    buildViewHeader({
      title: 'new topic',
      left: { label: '← back', onClick: () => history.back() },
    }),
  );

  const wrap = document.createElement('div');
  wrap.className = 'card wiki-edit';

  const nameInput = buildField(wrap, '토픽 이름 (필수)', '');

  const flash = document.createElement('div');
  flash.className = 'wiki-edit-flash';
  wrap.appendChild(flash);

  const actions = document.createElement('div');
  actions.className = 'wiki-edit-actions';

  const cancelBtn = document.createElement('button');
  cancelBtn.type = 'button';
  cancelBtn.className = 'action-button action-secondary';
  cancelBtn.textContent = '취소';
  cancelBtn.addEventListener('click', () => history.back());
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

  root.appendChild(wrap);

  // Submit on Enter for the one-field-form convenience.
  nameInput.addEventListener('keydown', (e) => {
    if (e.key === 'Enter') {
      e.preventDefault();
      void submit(initData, nameInput.value, createBtn, cancelBtn, flash);
    }
  });

  setTimeout(() => nameInput.focus(), 0);
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
    flash.classList.add('error');
    return;
  }
  flash.textContent = '';
  flash.classList.remove('error');
  createBtn.disabled = true;
  cancelBtn.disabled = true;
  createBtn.textContent = '만드는 중…';
  try {
    await createTopic(initData, name);
    // Telegram is the source of truth — the new topic will appear in
    // the supergroup's topic list automatically. Bounce to the topics
    // view so the user can see / open it.
    navigate({ name: 'sessions' });
  } catch (err) {
    flash.textContent = explainError(err);
    flash.classList.add('error');
    createBtn.disabled = false;
    cancelBtn.disabled = false;
    createBtn.textContent = '만들기';
  }
}

// explainError translates the two failure codes that have actionable
// user fixes into Korean guidance; falls back to the raw RPC error
// for anything we don't know about (so the user at least sees the
// upstream message).
function explainError(err: unknown): string {
  const raw = formatRpcError(err);
  if (raw.includes('VALIDATION_FAILED') || raw.includes('use-forum')) {
    return '아직 supergroup 으로 이전하지 않았습니다. 텔레그램에서 supergroup 에 들어가 /use-forum 을 먼저 입력해주세요.';
  }
  if (raw.includes('DEPENDENCY_FAILED') || raw.includes('rights')) {
    return '봇이 "Manage Topics" 권한을 잃었습니다. supergroup admin 설정에서 다시 활성화한 후 시도해주세요.';
  }
  return `토픽 생성 실패: ${errorMessage(err)}`;
}

function buildField(wrap: HTMLElement, label: string, value: string): HTMLInputElement {
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
