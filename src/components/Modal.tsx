// Reusable floating overlay for detail views and edit forms — the vehicle for the
// "선택 항목 상세" work area (DESIGN §4) that the flat grids lacked. Backdrop click
// and Esc close it; the panel follows the warm-Zen floating aesthetic (.panel).
// Rendered inline: position:fixed escapes the work-area scroll container, so no
// portal/root is needed and it stays trivially testable.
import { useEffect, type ReactNode } from "react";
import { color, line } from "@/theme";
import { Icon } from "./Icon";

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
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose]);

  return (
    // mousedown (not click) so a text drag-selection ending on the backdrop doesn't close it.
    <div className="modal-backdrop" onMouseDown={onClose} role="presentation">
      <div
        className="panel modal-panel"
        style={{ width, maxWidth: "calc(100vw - 48px)" }}
        role="dialog"
        aria-modal="true"
        onMouseDown={(e) => e.stopPropagation()}
      >
        <header style={{ display: "flex", alignItems: "center", gap: 8, padding: "14px 16px", borderBottom: line }}>
          <h3 style={{ flex: 1, minWidth: 0, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
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
