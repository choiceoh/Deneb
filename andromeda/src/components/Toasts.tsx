// Toast stack (bottom-center). Undo-bearing toasts linger longer; clicking
// 실행취소 runs the action and dismisses. Renders once in App.
import { useEffect, useRef, useState } from "react";
import { onToast, type ToastItem } from "@/toast";

const PLAIN_MS = 4000;
const UNDO_MS = 7000;

export function Toasts() {
  const [toasts, setToasts] = useState<ToastItem[]>([]);
  const timers = useRef(new Map<number, number>());

  useEffect(() => {
    const off = onToast((t) => {
      setToasts((prev) => [...prev.slice(-2), t]); // keep the stack shallow
      const ttl = t.onUndo ? UNDO_MS : PLAIN_MS;
      timers.current.set(
        t.id,
        window.setTimeout(() => dismiss(t.id), ttl),
      );
    });
    const map = timers.current;
    return () => {
      off();
      for (const id of map.values()) window.clearTimeout(id);
    };
  }, []);

  function dismiss(id: number) {
    const timer = timers.current.get(id);
    if (timer !== undefined) window.clearTimeout(timer);
    timers.current.delete(id);
    setToasts((prev) => prev.filter((t) => t.id !== id));
  }

  if (toasts.length === 0) return null;
  return (
    <div className="toast-stack" role="status" aria-live="polite">
      {toasts.map((t) => (
        <div key={t.id} className="toast">
          <span className="toast-msg">{t.message}</span>
          {t.onUndo && (
            <button
              type="button"
              className="toast-undo"
              onClick={() => {
                t.onUndo?.();
                dismiss(t.id);
              }}
            >
              {t.undoLabel}
            </button>
          )}
          <button type="button" className="toast-close" onClick={() => dismiss(t.id)} aria-label="알림 닫기">
            ×
          </button>
        </div>
      ))}
    </div>
  );
}
