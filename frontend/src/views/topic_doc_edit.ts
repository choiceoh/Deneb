// views/topic_doc_edit.ts — edit an existing topic knowledge file or create a
// new one. Inline textarea editing + download (Blob) + upload (file → textarea,
// then explicit save). Mirrors wiki_new.ts's form idiom; storage is the raw
// miniapp.topicdocs.* RPCs (plain .md, no frontmatter).

import { readTopicFile, writeTopicFile } from '../topicdocs';
import { errorMessage } from '../format';
import { navigate, isCurrentHash } from '../router';
import { buildViewHeader, buildLoadingNode, renderErrorView, showFlash } from './ui';

interface DocState {
  name: string;
  content: string;
  isNew: boolean;
}

export function renderTopicDocNew(root: HTMLElement, initData: string): void {
  paint(root, initData, { name: '', content: '', isNew: true });
}

export async function renderTopicDocEdit(
  root: HTMLElement,
  initData: string,
  file: string,
): Promise<void> {
  const expectedHash = location.hash;
  root.innerHTML = '';
  root.appendChild(
    buildViewHeader({ title: 'topic doc', left: { label: '← back', onClick: () => history.back() } }),
  );
  root.appendChild(buildLoadingNode('불러오는 중…'));

  try {
    const doc = await readTopicFile(initData, file);
    if (!isCurrentHash(expectedHash) || !root.isConnected) return;
    paint(root, initData, { name: doc.name, content: doc.content, isNew: false });
  } catch (err) {
    if (!isCurrentHash(expectedHash) || !root.isConnected) return;
    renderErrorView(root, `파일 로드 실패: ${errorMessage(err)}`, {
      label: '← back',
      onClick: () => history.back(),
    });
  }
}

function paint(root: HTMLElement, initData: string, st: DocState): void {
  root.innerHTML = '';
  root.appendChild(
    buildViewHeader({
      title: st.isNew ? 'new topic doc' : st.name,
      left: { label: '← back', onClick: () => history.back() },
    }),
  );

  const wrap = document.createElement('div');
  wrap.className = 'card wiki-edit';

  // File name: editable only for a new file; read-only otherwise.
  let nameInput: HTMLInputElement | null = null;
  if (st.isNew) {
    const label = document.createElement('label');
    label.className = 'wiki-edit-field';
    const span = document.createElement('span');
    span.className = 'wiki-edit-field-label';
    span.textContent = '파일명 (예: research.md)';
    label.appendChild(span);
    nameInput = document.createElement('input');
    nameInput.className = 'wiki-edit-input';
    nameInput.type = 'text';
    nameInput.placeholder = 'research.md';
    nameInput.value = st.name;
    label.appendChild(nameInput);
    wrap.appendChild(label);
  }

  const bodyLabel = document.createElement('div');
  bodyLabel.className = 'wiki-edit-field-label';
  bodyLabel.textContent = '본문 (Markdown)';
  wrap.appendChild(bodyLabel);

  const textarea = document.createElement('textarea');
  textarea.className = 'wiki-edit-textarea';
  textarea.rows = 18;
  textarea.spellcheck = false;
  textarea.value = st.content;
  wrap.appendChild(textarea);

  const flash = document.createElement('div');
  flash.className = 'wiki-edit-flash';
  wrap.appendChild(flash);

  const currentName = (): string =>
    (st.isNew && nameInput ? nameInput.value : st.name).trim();

  const actions = document.createElement('div');
  actions.className = 'wiki-edit-actions';

  const saveBtn = document.createElement('button');
  saveBtn.type = 'button';
  saveBtn.className = 'action-button action-primary';
  saveBtn.textContent = '저장';
  saveBtn.addEventListener('click', () => {
    void save(initData, currentName(), textarea.value, st.isNew, saveBtn, flash);
  });
  actions.appendChild(saveBtn);

  const downloadBtn = document.createElement('button');
  downloadBtn.type = 'button';
  downloadBtn.className = 'action-button action-secondary';
  downloadBtn.textContent = '다운로드';
  downloadBtn.addEventListener('click', () => download(currentName() || 'topic.md', textarea.value));
  actions.appendChild(downloadBtn);

  // Upload fills the textarea (does NOT auto-save) so the operator reviews
  // before committing the replacement.
  const uploadBtn = document.createElement('button');
  uploadBtn.type = 'button';
  uploadBtn.className = 'action-button action-secondary';
  uploadBtn.textContent = '업로드';
  const fileInput = document.createElement('input');
  fileInput.type = 'file';
  fileInput.accept = '.md,text/markdown,text/plain';
  fileInput.style.display = 'none';
  fileInput.addEventListener('change', () => {
    const f = fileInput.files?.[0];
    if (!f) return;
    f.text()
      .then((text) => {
        textarea.value = text;
        flash.textContent = `'${f.name}' 내용을 불러왔습니다. 검토 후 저장하세요.`;
      })
      .catch(() => {
        flash.textContent = '파일을 읽지 못했습니다.';
      })
      .finally(() => {
        fileInput.value = '';
      });
  });
  uploadBtn.addEventListener('click', () => fileInput.click());
  actions.appendChild(uploadBtn);
  actions.appendChild(fileInput);

  wrap.appendChild(actions);
  root.appendChild(wrap);

  // Telegram WebView drops rAF callbacks fired during a nav animation;
  // setTimeout(0) lands the focus after the transition settles.
  setTimeout(() => (st.isNew && nameInput ? nameInput : textarea).focus(), 0);
}

async function save(
  initData: string,
  name: string,
  content: string,
  isNew: boolean,
  btn: HTMLButtonElement,
  flash: HTMLElement,
): Promise<void> {
  if (!name) {
    flash.textContent = '파일명을 입력하세요.';
    return;
  }
  btn.disabled = true;
  const orig = btn.textContent ?? '저장';
  btn.textContent = '저장 중…';
  flash.textContent = '';
  try {
    const res = await writeTopicFile(initData, name, content, isNew);
    showFlash(`저장됨: ${res.name}`, 'success');
    if (isNew) {
      navigate({ name: 'topicDocEdit', file: res.name });
      return;
    }
    btn.disabled = false;
    btn.textContent = orig;
  } catch (err) {
    btn.disabled = false;
    btn.textContent = orig;
    flash.textContent = `저장 실패: ${errorMessage(err)}`;
  }
}

function download(name: string, content: string): void {
  const blob = new Blob([content], { type: 'text/markdown;charset=utf-8' });
  const url = URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = url;
  a.download = name.endsWith('.md') ? name : `${name}.md`;
  document.body.appendChild(a);
  a.click();
  a.remove();
  setTimeout(() => URL.revokeObjectURL(url), 0);
}
