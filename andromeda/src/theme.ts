// Typed mirror of the design tokens defined in styles.css (the :root CSS vars).
// Components reference these for INLINE styles; stateful/structural styling
// (hover, focus, scrollbars, the lifting nav tab) lives in styles.css classes.
// Rule: no raw hex in components — only these tokens or a styles.css class.
import type { CSSProperties } from "react";

export const color = {
  text: "var(--ink)",
  text2: "var(--ink-2)",
  muted: "var(--muted)",
  faint: "var(--faint)",
  line: "var(--line)",
  field: "var(--panel)",
  active: "var(--accent-soft)",
  accent: "var(--accent)",
  online: "var(--online)",
  danger: "var(--due)",
};

export const line = `1px solid var(--line)`;

// Unified app typeface (also set on body in styles.css; kept for inline use).
export const font = '"Pretendard Variable", Pretendard, system-ui, "Segoe UI", "Malgun Gothic", sans-serif';

// Inner padding for a panel's scrollable content area.
export const pane: CSSProperties = { padding: 18, boxSizing: "border-box", overflow: "auto" };

export const muted: CSSProperties = { color: "var(--muted-2)" };

// One-line ellipsis truncation for tight grid cells.
export function ellipsis(maxWidth: number): CSSProperties {
  return { whiteSpace: "nowrap", overflow: "hidden", textOverflow: "ellipsis", maxWidth };
}
