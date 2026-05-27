// views/wiki_page.ts — full wiki page render with inline edit.
//
// Reached by tapping a memory search hit, a sender-context chip in
// mail detail, or any other navigate({name:'wikiPage'}) call. Shows
// frontmatter meta in a sidebar-ish row, then the Markdown body via
// the in-house renderer.
//
// Edit mode is a per-render flag toggled by the "수정" header button.
// In edit mode the body card is replaced with a <textarea> holding
// the raw markdown plus a small action bar (저장 / 취소). Save POSTs
// to miniapp.memory.write_page; the response is the updated page, so
// we re-render in view mode with the fresh content (including the
// bumped 갱신 date) and no extra round-trip.

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

  // Meta card — same in both modes (title + summary + tags + dates).
  // Frontmatter editing is out of scope for v1; the user can still
  // tweak title/tags by hand-editing the markdown file outside the
  // Mini App.
  root.appendChild(buildMetaCard(page));

  if (editing) {
    root.appendChild(buildEditor(root, initData, page));
  } else {
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
  const tagsLine = document.createElement('div');
  tagsLine.className = 'wiki-meta-tags';
  const tagParts: string[] = [];
  if (page.category) tagParts.push(`#${page.category}`);
  if (page.tags) tagParts.push(...page.tags.map((t) => `#${t}`));
  tagsLine.textContent = tagParts.join(' · ');
  if (tagsLine.textContent) meta.appendChild(tagsLine);

  const dateLine = document.createElement('div');
  dateLine.className = 'wiki-meta-dates';
  const dateParts: string[] = [];
  if (page.updated) dateParts.push(`갱신 ${page.updated}`);
  if (page.created) dateParts.push(`생성 ${page.created}`);
  if (page.due) dateParts.push(`📅 ${page.due}`);
  dateLine.textContent = dateParts.join(' · ');
  if (dateLine.textContent) meta.appendChild(dateLine);
  return meta;
}

// buildEditor returns a card containing a textarea pre-populated with
// the page's raw markdown body plus a 저장 / 취소 action row. Save
// disables the button mid-flight to prevent double-fire; failure
// surfaces inline so the textarea contents aren't lost.
function buildEditor(root: HTMLElement, initData: string, page: MemoryPage): HTMLElement {
  const wrap = document.createElement('div');
  wrap.className = 'card wiki-edit';

  const textarea = document.createElement('textarea');
  textarea.className = 'wiki-edit-textarea';
  textarea.value = page.body || '';
  textarea.rows = 18;
  textarea.spellcheck = false;
  wrap.appendChild(textarea);

  const flash = document.createElement('div');
  flash.className = 'wiki-edit-flash';
  // Stays empty until a save attempt actually fails. CSS hides it
  // when blank so the action bar sits flush against the textarea.
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
    void doSave(root, initData, page, textarea, saveBtn, cancelBtn, flash);
  });

  actions.appendChild(cancelBtn);
  actions.appendChild(saveBtn);
  wrap.appendChild(actions);

  // Defer focus until after the new DOM is attached. setTimeout(0)
  // beats requestAnimationFrame here because some Telegram WebView
  // builds throw away rAF callbacks during navigation animations.
  setTimeout(() => textarea.focus(), 0);

  return wrap;
}

async function doSave(
  root: HTMLElement,
  initData: string,
  page: MemoryPage,
  textarea: HTMLTextAreaElement,
  saveBtn: HTMLButtonElement,
  cancelBtn: HTMLButtonElement,
  flash: HTMLElement,
): Promise<void> {
  saveBtn.disabled = true;
  cancelBtn.disabled = true;
  flash.textContent = '';
  saveBtn.textContent = '저장 중…';

  try {
    const updated = await writePage(initData, page.path, textarea.value);
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
