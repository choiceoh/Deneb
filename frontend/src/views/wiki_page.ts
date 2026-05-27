// views/wiki_page.ts — full wiki page render with inline edit.
//
// Reached by tapping a memory search hit, a sender-context chip in
// mail detail, or any other navigate({name:'wikiPage'}) call. Shows
// frontmatter meta in a sidebar-ish row, then the Markdown body via
// the in-house renderer.
//
// Edit mode is a per-render flag toggled by the "수정" header button.
// In edit mode the meta card is replaced with editable inputs (title,
// summary, tags) and the body card is replaced with a textarea
// holding the raw markdown. Save POSTs everything in one
// miniapp.memory.write_page call; the response is the updated page,
// so we re-render in view mode with the fresh content (including the
// bumped 갱신 date) and no extra round-trip.
//
// Category is intentionally read-only in edit mode — changing the
// category would require moving the file on disk, which is a "create
// new page" operation (not an edit).

import { getPage, writePage, type MemoryPage } from '../memory';
import { errorMessage, formatRpcError } from '../format';
import { isCurrentHash, navigate } from '../router';
import { renderMarkdown } from '../markdown';
import { buildViewHeader, renderErrorView } from './ui';

export async function renderWikiPage(
  root: HTMLElement,
  initData: string,
  path: string,
): Promise<void> {
  const expectedHash = location.hash;
  root.innerHTML = '<div class="loading">위키 페이지 불러오는 중…</div>';

  try {
    const page = await getPage(initData, path);
    if (!isCurrentHash(expectedHash)) return;
    paint(root, initData, page, false);
  } catch (err) {
    if (!isCurrentHash(expectedHash)) return;
    renderErrorView(root, `위키 페이지 로드 실패: ${formatRpcError(err)}`, {
      label: '← 메모리 검색으로',
      onClick: () => navigate({ name: 'memory' }),
    });
  }
}

// paint renders `page` either in view mode (rendered markdown body)
// or edit mode (textarea + save/cancel). The header right slot
// swaps between "수정" and "취소" so the user always has a clear
// way back to view mode.
function paint(
  root: HTMLElement,
  initData: string,
  page: MemoryPage,
  editing: boolean,
): void {
  root.innerHTML = '';

  root.appendChild(
    buildViewHeader({
      title: editing ? 'wiki · edit' : 'wiki',
      left: { label: '← search', onClick: () => navigate({ name: 'memory' }) },
      right: editing
        ? { label: 'cancel', onClick: () => paint(root, initData, page, false) }
        : { label: 'edit', onClick: () => paint(root, initData, page, true) },
    }),
  );

  if (editing) {
    // Edit mode replaces both the meta card and the body card with
    // form fields. They share a doSave call (one RPC roundtrip) so
    // the user can change everything in one save.
    root.appendChild(buildEditor(root, initData, page));
  } else {
    root.appendChild(buildMetaCard(page));
    const body = document.createElement('div');
    body.className = 'card wiki-body';
    body.innerHTML = renderMarkdown(page.body || '(본문 없음)');
    root.appendChild(body);
  }

  if (page.related && page.related.length > 0) {
    const related = document.createElement('div');
    related.className = 'card wiki-related';
    const label = document.createElement('div');
    label.className = 'wiki-related-label';
    label.textContent = '관련 페이지';
    related.appendChild(label);
    for (const r of page.related) {
      const chip = document.createElement('button');
      chip.className = 'wiki-related-chip';
      chip.textContent = r;
      chip.addEventListener('click', () => navigate({ name: 'wikiPage', path: r }));
      related.appendChild(chip);
    }
    root.appendChild(related);
  }

  const footer = document.createElement('div');
  footer.className = 'muted';
  footer.textContent = page.path;
  root.appendChild(footer);
}

function buildMetaCard(page: MemoryPage): HTMLElement {
  const meta = document.createElement('div');
  meta.className = 'card wiki-meta';
  const title = document.createElement('div');
  title.className = 'wiki-title';
  title.textContent = page.title || page.path;
  meta.appendChild(title);
  if (page.summary) {
    const sub = document.createElement('div');
    sub.className = 'wiki-summary';
    sub.textContent = page.summary;
    meta.appendChild(sub);
  }

  // Metadata rows — the .row > .label + .value pattern is the canonical
  // detail-view idiom used by calendar_event and mail detail. Concatenated
  // "갱신 X · 생성 Y · #tag1 · #tag2" strings read as an afterthought; rows
  // line the fields up the same way the rest of the app does.
  if (page.category) appendMetaRow(meta, '카테고리', `#${page.category}`);
  if (page.tags && page.tags.length > 0) {
    appendMetaRow(meta, '태그', page.tags.map((t) => `#${t}`).join(' '));
  }
  if (page.updated) appendMetaRow(meta, '갱신', page.updated);
  if (page.created) appendMetaRow(meta, '생성', page.created);
  if (page.due) appendMetaRow(meta, '마감', page.due);
  return meta;
}

function appendMetaRow(card: HTMLElement, label: string, value: string): void {
  const row = document.createElement('div');
  row.className = 'row';
  const l = document.createElement('span');
  l.className = 'label';
  l.textContent = label;
  const v = document.createElement('span');
  v.className = 'value';
  v.textContent = value;
  row.appendChild(l);
  row.appendChild(v);
  card.appendChild(row);
}

// buildEditor renders a single card with input rows for title /
// summary / tags, a textarea for the body, an inline flash for save
// errors, and the 저장 / 취소 action row. One `doSave` packs all
// fields into a single write_page call so the user doesn't have to
// save in stages.
function buildEditor(root: HTMLElement, initData: string, page: MemoryPage): HTMLElement {
  const wrap = document.createElement('div');
  wrap.className = 'card wiki-edit';

  const titleInput = buildFieldInput(wrap, '제목', page.title || '');
  const summaryInput = buildFieldInput(wrap, '요약', page.summary || '');
  const tagsInput = buildFieldInput(
    wrap,
    '태그 (쉼표로 구분)',
    (page.tags || []).join(', '),
  );

  // Category is read-only — the on-disk path encodes it. Show it as
  // a static line so the user understands what's locked.
  if (page.category) {
    const catRow = document.createElement('div');
    catRow.className = 'wiki-edit-static';
    catRow.innerHTML = `<span class="wiki-edit-static-label">카테고리</span><span class="wiki-edit-static-value"></span>`;
    (catRow.querySelector('.wiki-edit-static-value') as HTMLElement).textContent =
      `#${page.category}`;
    wrap.appendChild(catRow);
  }

  const bodyLabel = document.createElement('div');
  bodyLabel.className = 'wiki-edit-field-label';
  bodyLabel.textContent = '본문 (Markdown)';
  wrap.appendChild(bodyLabel);

  const textarea = document.createElement('textarea');
  textarea.className = 'wiki-edit-textarea';
  textarea.value = page.body || '';
  textarea.rows = 14;
  textarea.spellcheck = false;
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
  cancelBtn.addEventListener('click', () => paint(root, initData, page, false));

  const saveBtn = document.createElement('button');
  saveBtn.type = 'button';
  saveBtn.className = 'action-button action-primary';
  saveBtn.textContent = '저장';
  saveBtn.addEventListener('click', () => {
    void doSave(root, initData, page, {
      title: titleInput.value,
      summary: summaryInput.value,
      tags: tagsInput.value,
      body: textarea.value,
    }, saveBtn, cancelBtn, flash);
  });

  actions.appendChild(cancelBtn);
  actions.appendChild(saveBtn);
  wrap.appendChild(actions);

  // Defer focus until after the new DOM is attached. setTimeout(0)
  // beats requestAnimationFrame here because some Telegram WebView
  // builds throw away rAF callbacks during navigation animations.
  setTimeout(() => titleInput.focus(), 0);

  return wrap;
}

// buildFieldInput appends a labeled single-line input to `wrap` and
// returns the input element. Shared by the three frontmatter fields
// so they all align visually. Uses <label> with a nested <span> +
// <input> so tapping the label text focuses the input.
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

interface EditFormValues {
  title: string;
  summary: string;
  tags: string; // comma-separated raw input
  body: string;
}

async function doSave(
  root: HTMLElement,
  initData: string,
  page: MemoryPage,
  values: EditFormValues,
  saveBtn: HTMLButtonElement,
  cancelBtn: HTMLButtonElement,
  flash: HTMLElement,
): Promise<void> {
  saveBtn.disabled = true;
  cancelBtn.disabled = true;
  flash.textContent = '';
  saveBtn.textContent = '저장 중…';

  // Parse tags from the comma-separated input. Backend trims + drops
  // blanks, but doing it here too gives the user a quick visual sense
  // of what's about to be saved (we don't show that, but the backend
  // result we re-render with does).
  const tags = values.tags
    .split(',')
    .map((t) => t.trim())
    .filter(Boolean);

  try {
    const updated = await writePage(initData, page.path, values.body, {
      title: values.title.trim(),
      summary: values.summary.trim(),
      tags,
    });
    // Re-render in view mode. The backend returned the updated page
    // with bumped 갱신 date, so the meta card refreshes too.
    paint(root, initData, updated, false);
  } catch (err) {
    saveBtn.disabled = false;
    cancelBtn.disabled = false;
    saveBtn.textContent = '저장';
    flash.textContent = `저장 실패: ${errorMessage(err)}`;
  }
}
