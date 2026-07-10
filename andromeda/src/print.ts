// print.ts — 인쇄(브라우저/웹뷰 프린트 다이얼로그 → 프린터 또는 PDF 저장).
//
// Andromeda has no routes, so "print X" can't mean "navigate to a print page".
// Instead we mark one DOM subtree as the print region and tag its ancestor chain;
// the `@media print` block in styles.css then hides every off-path sibling so
// only that subtree reaches the page, and window.print() drives the OS dialog.
//
// The marker classes only bite under `@media print`, so a failed cleanup never
// changes the on-screen UI — at worst a later print re-scopes, which the
// defensive clear at the top of printElement() prevents.

const REGION = "deneb-print-region";
const ANCESTOR = "deneb-print-ancestor";
const PRINTING = "deneb-printing";

// Strip every print marker from the document (also the afterprint handler).
function clearMarks(): void {
  document.body.classList.remove(PRINTING);
  for (const el of document.querySelectorAll<HTMLElement>(`.${REGION}, .${ANCESTOR}`)) {
    el.classList.remove(REGION, ANCESTOR);
  }
}

// Print just this element's subtree. Null → print the whole window (fallback).
export function printElement(el: HTMLElement | null): void {
  if (!el) {
    window.print();
    return;
  }
  clearMarks(); // drop any markers a prior (uncleaned) print left behind
  el.classList.add(REGION);
  for (let p = el.parentElement; p && p !== document.body; p = p.parentElement) {
    p.classList.add(ANCESTOR);
  }
  document.body.classList.add(PRINTING);
  // afterprint fires when the dialog closes (print/cancel) — the only safe time
  // to un-mark, since window.print() may be async in the webview. A leaked
  // marker is harmless on screen but the pre-clear above still guards it.
  window.addEventListener("afterprint", clearMarks, { once: true });
  window.print();
}

// Convenience for repeated rows (e.g. chat turns): print the nearest ancestor of
// the clicked control that matches `selector`.
export function printClosest(target: EventTarget | null, selector: string): void {
  const el = target instanceof Element ? target.closest<HTMLElement>(selector) : null;
  printElement(el);
}
