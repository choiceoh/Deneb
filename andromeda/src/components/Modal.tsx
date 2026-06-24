// Reusable floating overlay for detail views and edit forms — the vehicle for the
// "선택 항목 상세" work area (DESIGN §4) that the flat grids lacked. Backdrop click
// and Esc close it; the panel follows the warm-Zen floating aesthetic (.panel).
// Rendered inline: position:fixed escapes the work-area scroll container, so no
// portal/root is needed and it stays trivially testable.
import { useEffect, useId, useRef, type ReactNode } from "react";
import { color, line } from "@/theme";
import { Icon } from "./Icon";

// Tabbable elements inside the dialog — used to keep keyboard focus trapped within.
const FOCUSABLE =
  'a[href],button:not([disabled]),input:not([disabled]),textarea:not([disabled]),select:not([disabled]),[tabindex]:not([tabindex="-1"])';

export function Modal({
  title,
  onClose,
  children,
  footer,
  width = 520,
}: {
  title: ReactNode;
  onClose: () => void;
  children: ReactNode;
  footer?: ReactNode;
  width?: number;
}) {
  const titleId = useId();
  const panelRef = useRef<HTMLDivElement>(null);

  // Move focus into the dialog on open (unless an autoFocus field already took it)
  // and return it to the trigger on close — the baseline a11y contract for a modal.
  useEffect(() => {
    const prev = document.activeElement as HTMLElement | null;
    const panel = panelRef.current;
    if (panel && !panel.contains(document.activeElement)) panel.focus();
    return () => prev?.focus?.();
  }, []);

  // Esc closes; Tab is trapped so keyboard focus can't wander to the page behind.
  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      if (e.key === "Escape") return onClose();
      if (e.key !== "Tab") return;
      const panel = panelRef.current;
      if (!panel) return;
      const f = panel.querySelectorAll<HTMLElement>(FOCUSABLE);
      if (f.length === 0) return;
      const first = f[0];
      const last = f[f.length - 1];
      const active = document.activeElement;
      if (e.shiftKey && (active === first || active === panel)) {
        e.preventDefault();
        last.focus();
      } else if (!e.shiftKey && active === last) {
        e.preventDefault();
        first.focus();
      }
    }
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose]);

  return (
    // mousedown (not click) so a text drag-selection ending on the backdrop doesn't close it.
    <div className="modal-backdrop" onMouseDown={onClose} role="presentation">
      <div
        ref={panelRef}
        className="panel modal-panel"
        style={{ width, maxWidth: "calc(100vw - 48px)" }}
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        tabIndex={-1}
        onMouseDown={(e) => e.stopPropagation()}
      >
        <header style={{ display: "flex", alignItems: "center", gap: 8, padding: "14px 16px", borderBottom: line }}>
          <h3
            id={titleId}
            style={{ flex: 1, minWidth: 0, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}
          >
            {title}
          </h3>
          <button className="row-btn" onClick={onClose} aria-label="닫기" title="닫기">
            <Icon name="close" size={16} />
          </button>
        </header>
        <div style={{ padding: 16, overflow: "auto" }}>{children}</div>
        {footer && (
          <footer
            style={{ display: "flex", justifyContent: "flex-end", gap: 8, padding: "12px 16px", borderTop: line }}
          >
            {footer}
          </footer>
        )}
      </div>
    </div>
  );
}

// Labeled vertical form field — shared layout for the edit/create forms in panes.
export function Field({ label, children }: { label: string; children: ReactNode }) {
  return (
    <label style={{ display: "flex", flexDirection: "column", gap: 5, marginBottom: 12 }}>
      <span style={{ fontSize: 12, color: color.muted }}>{label}</span>
      {children}
    </label>
  );
}

// Read-only label/value row — shared layout for the detail modals (mail, person, event).
export function Detail({ label, value, multiline }: { label: string; value: ReactNode; multiline?: boolean }) {
  return (
    <div style={{ marginBottom: 12 }}>
      <div style={{ fontSize: 12, color: color.muted, marginBottom: 3 }}>{label}</div>
      <div
        style={{
          fontSize: 14,
          color: color.text2,
          whiteSpace: multiline ? "pre-wrap" : "normal",
          lineHeight: 1.55,
          wordBreak: "break-word",
        }}
      >
        {value}
      </div>
    </div>
  );
}

// The standard modal footer: a optional left-aligned status line, a 취소 button,
// and an accent action button that stays disabled until `canSubmit`. Extracted so
// every form modal (OneFieldModal, TodoModal, NotebookPane's modals…) renders the
// identical footer instead of re-laying it out each time.
export function ModalFooter({
  action,
  canSubmit = true,
  status,
  onClose,
  onSubmit,
}: {
  action: string;
  canSubmit?: boolean;
  status?: string;
  onClose: () => void;
  onSubmit: () => void;
}) {
  return (
    <>
      {status && (
        <span className="pane-status" style={{ marginRight: "auto" }}>
          {status}
        </span>
      )}
      <button className="btn" onClick={onClose}>
        취소
      </button>
      <button className="btn btn-accent" onClick={onSubmit} disabled={!canSubmit}>
        {action}
      </button>
    </>
  );
}
