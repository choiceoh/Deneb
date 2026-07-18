// Tiny toast bus: fire-and-forget notices with an optional undo action —
// frontier delete UX (no blocking confirm; act, then offer 실행취소). Module
// bus (not context) so non-React code paths can toast too.
export interface ToastItem {
  id: number;
  message: string;
  undoLabel: string;
  onUndo?: () => void;
}

let seq = 0;
const listeners = new Set<(t: ToastItem) => void>();

export function showToast(message: string, opts?: { undo?: () => void; undoLabel?: string }): void {
  const toast: ToastItem = {
    id: ++seq,
    message,
    undoLabel: opts?.undoLabel ?? "실행취소",
    onUndo: opts?.undo,
  };
  for (const l of listeners) l(toast);
}

export function onToast(listener: (t: ToastItem) => void): () => void {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}
