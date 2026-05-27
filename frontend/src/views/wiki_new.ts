// views/wiki_new.ts — Create a new wiki page.
//
// Reached from memory search (header right "+ 새 페이지") and from the
// category explorer (per-category "+ 새 페이지" CTA, which seeds the
// category field). Inputs are title (required), category (required),
// summary (optional), tags (comma-separated), body (optional markdown).
//
// On submit we call miniapp.memory.create_page; backend computes the
// final path from <category>/<slugified-title>.md and returns the new
// page. The view then navigates directly into wikiPage at that path
// so the user lands on the rendered version (which they can edit
// further if they want).

import { createPage } from '../memory';
import { errorMessage } from '../format';
import { navigate } from '../router';
import { buildViewHeader } from './ui';

export function renderWikiNew(
  root: HTMLElement,
  initData: string,
  initialCategory: string,
): void {
  root.innerHTML = '';

  root.appendChild(
    buildViewHeader({
      title: 'new page',
      left: { label: '← back', onClick: () => history.back() },
    }),
  );

  const wrap = document.createElement('div');
  wrap.className = 'card wiki-edit';

  const titleInput = buildFieldInput(wrap, '제목 (필수)', '');
  const categoryInput = buildFieldInput(wrap, '카테고리 (필수)', initialCategory);
  const summaryInput = buildFieldInput(wrap, '요약', '');
  const tagsInput = buildFieldInput(wrap, '태그 (쉼표 구분)', '');

  const bodyLabel = document.createElement('div');
  bodyLabel.className = 'wiki-edit-field-label';
  bodyLabel.textContent = '본문 (Markdown)';
  wrap.appendChild(bodyLabel);

  const textarea = document.createElement('textarea');
  textarea.className = 'wiki-edit-textarea';
  textarea.rows = 12;
  textarea.spellcheck = false;
  textarea.placeholder = '# 제목\n\n첫 줄에 H1 헤더로 시작하는 게 관례입니다 (선택).';
  wrap.appendChild(textarea);

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
    void submit(initData, {
      title: titleInput.value,
      category: categoryInput.value,
      summary: summaryInput.value,
      tagsRaw: tagsInput.value,
      body: textarea.value,
    }, createBtn, cancelBtn, flash);
  });
  actions.appendChild(createBtn);
  wrap.appendChild(actions);

  root.appendChild(wrap);

  setTimeout(() => titleInput.focus(), 0);
}

// buildFieldInput is the same DOM shape as wiki_page.ts's helper. We
// duplicate it (rather than reach across files) because the two views
// are deliberately self-contained and a one-helper module for a 12-
// line widget would be overkill.
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

interface NewPageValues {
  title: string;
  category: string;
  summary: string;
  tagsRaw: string;
  body: string;
}

async function submit(
  initData: string,
  values: NewPageValues,
  createBtn: HTMLButtonElement,
  cancelBtn: HTMLButtonElement,
  flash: HTMLElement,
): Promise<void> {
  const title = values.title.trim();
  const category = values.category.trim();
  if (!title) {
    flash.textContent = '제목을 입력하세요.';
    return;
  }
  if (!category) {
    flash.textContent = '카테고리를 입력하세요.';
    return;
  }

  createBtn.disabled = true;
  cancelBtn.disabled = true;
  flash.textContent = '';
  createBtn.textContent = '만드는 중…';

  const tags = values.tagsRaw
    .split(',')
    .map((t) => t.trim())
    .filter(Boolean);

  try {
    const page = await createPage(initData, {
      title,
      category,
      summary: values.summary.trim(),
      tags,
      body: values.body,
    });
    navigate({ name: 'wikiPage', path: page.path });
  } catch (err) {
    createBtn.disabled = false;
    cancelBtn.disabled = false;
    createBtn.textContent = '만들기';
    flash.textContent = `생성 실패: ${errorMessage(err)}`;
  }
}
