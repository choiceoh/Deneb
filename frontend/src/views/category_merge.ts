// views/category_merge.ts — the "두 프로젝트 병합" flow.
//
// Invoked from category_pages once exactly two pages are selected. Drives:
//   1. direction choice  — which page survives (target) vs is folded in (source)
//   2. final confirm     — native confirm warning that source will be deleted
//   3. execution         — miniapp.memory.merge (server synthesizes the body),
//                          with a progress pill while the LLM call runs
//
// Kept out of category_pages.ts so that view stays focused on listing +
// selection state. Returns true when a merge actually completed so the caller
// can refresh the list.

import { mergePages } from '../memory';
import { confirmAction } from '../dialog';
import { formatRpcError } from '../format';
import { showFlash } from './ui';

export interface MergeCandidate {
  path: string;
  title?: string;
}

// label prefers the human title, falling back to the path so the prompts are
// never blank for an untitled page.
function label(c: MergeCandidate): string {
  return (c.title ?? '').trim() || c.path;
}

export async function runMerge(
  initData: string,
  a: MergeCandidate,
  b: MergeCandidate,
): Promise<boolean> {
  const choice = await chooseMergeTarget(label(a), label(b));
  if (!choice) return false;

  const target = choice === 'a' ? a : b;
  const source = choice === 'a' ? b : a;

  const ok = await confirmAction(
    `「${label(source)}」를 「${label(target)}」에 병합합니다.\n\n` +
      `「${label(source)}」 페이지는 삭제되고, 이를 참조하던 링크는 ` +
      `「${label(target)}」로 옮겨집니다.\n\n` +
      `병합은 백그라운드에서 처리되고, 끝나면 알림이 와요. 계속할까요?`,
  );
  if (!ok) return false;

  // The merge runs in the background on the gateway (the slow part is the
  // model combining the two bodies), so this call returns right away — we just
  // confirm it started. Completion arrives as a Telegram notice, not here.
  try {
    await mergePages(initData, target.path, source.path);
    showFlash('병합을 시작했어요 · 끝나면 알림을 보낼게요', 'success', 4000);
    return true;
  } catch (err) {
    showFlash(`병합 시작 실패: ${formatRpcError(err)}`, 'error', 4000);
    return false;
  }
}

// chooseMergeTarget presents a bottom sheet asking which of the two pages to
// keep. Resolves 'a' / 'b' for the chosen survivor, or null if cancelled
// (tap a row, the cancel button, or the backdrop). Custom DOM rather than
// tg.showPopup so it renders identically in Telegram, the dev harness, and a
// plain browser, and so long titles aren't truncated by the native popup.
function chooseMergeTarget(
  titleA: string,
  titleB: string,
): Promise<'a' | 'b' | null> {
  return new Promise((resolve) => {
    const backdrop = document.createElement('div');
    backdrop.className = 'sheet-backdrop';

    const sheet = document.createElement('div');
    sheet.className = 'sheet';

    const title = document.createElement('div');
    title.className = 'sheet-title';
    title.textContent = '어느 프로젝트를 남길까요?';
    sheet.appendChild(title);

    const hint = document.createElement('div');
    hint.className = 'sheet-hint';
    hint.textContent = '선택한 쪽으로 합쳐지고, 나머지는 병합 후 삭제됩니다.';
    sheet.appendChild(hint);

    let settled = false;
    const close = (val: 'a' | 'b' | null): void => {
      if (settled) return;
      settled = true;
      backdrop.classList.remove('sheet-visible');
      // Remove after the slide-out transition; timeout backs up transitionend
      // in case it never fires (reduced motion / display change).
      let removed = false;
      const remove = (): void => {
        if (removed) return;
        removed = true;
        backdrop.remove();
      };
      backdrop.addEventListener('transitionend', remove, { once: true });
      window.setTimeout(remove, 400);
      resolve(val);
    };

    sheet.appendChild(buildOption(titleA, () => close('a')));
    sheet.appendChild(buildOption(titleB, () => close('b')));

    const cancel = document.createElement('button');
    cancel.type = 'button';
    cancel.className = 'sheet-cancel';
    cancel.textContent = '취소';
    cancel.addEventListener('click', () => close(null));
    sheet.appendChild(cancel);

    backdrop.addEventListener('click', (e) => {
      if (e.target === backdrop) close(null);
    });

    backdrop.appendChild(sheet);
    document.body.appendChild(backdrop);

    // Force reflow so the slide-up/​fade-in transition fires.
    void sheet.offsetHeight;
    backdrop.classList.add('sheet-visible');
  });
}

function buildOption(text: string, onClick: () => void): HTMLButtonElement {
  const btn = document.createElement('button');
  btn.type = 'button';
  btn.className = 'sheet-option';
  btn.textContent = text;
  btn.addEventListener('click', onClick);
  return btn;
}
