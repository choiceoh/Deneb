// Design tokens for the workstation. Centralized here so panes share one visual
// language (dark, dense, desktop-business feel) instead of scattering hex codes
// and paddings across components — the skeleton's single source of style truth.
import type { CSSProperties } from "react";

export const color = {
  text: "#e8e8e8",
  line: "#2a2a2a",
  field: "#1f1f1f",
  active: "#2a2a2a",
  accent: "#6aa0ff",
  danger: "#e0a0a0",
};

export const line = `1px solid ${color.line}`;

// Shared layout/element styles.
export const pane: CSSProperties = { padding: 14, boxSizing: "border-box", overflow: "auto" };
export const field: CSSProperties = {
  padding: 8,
  background: color.field,
  color: color.text,
  border: line,
  borderRadius: 4,
};
export const muted: CSSProperties = { opacity: 0.6 };
export const th: CSSProperties = { padding: "6px 8px" };
export const td: CSSProperties = { padding: "6px 8px" };
export const kbd: CSSProperties = { opacity: 0.4, fontSize: 11 };

export function navButton(active: boolean): CSSProperties {
  return {
    display: "flex",
    justifyContent: "space-between",
    width: "100%",
    textAlign: "left",
    padding: "6px 8px",
    marginBottom: 4,
    background: active ? color.active : "transparent",
    color: color.text,
    border: "none",
    borderRadius: 4,
    cursor: "pointer",
  };
}

// One-line ellipsis truncation for tight grid cells.
export function ellipsis(maxWidth: number): CSSProperties {
  return { whiteSpace: "nowrap", overflow: "hidden", textOverflow: "ellipsis", maxWidth };
}
