import { type CSSProperties } from "react";

import { font } from "@/theme";

import { Markdown } from "./Markdown";

// Shared body of the doc/wiki editors: a Markdown textarea that swaps to a rendered
// preview. `preview` + the toggle button stay the parent's (their headers differ —
// 문서 h2 vs 위키 h3 + 저장), but this owns the textarea styling and the preview
// surface so both panes stop re-implementing them. `fill` grows to the flex
// container (위키); the default is a fixed 70vh (the scratch 문서).
export function MarkdownEditor({
  value,
  onChange,
  preview,
  disabled,
  placeholder,
  fill,
  ariaLabel,
}: {
  value: string;
  onChange: (v: string) => void;
  preview: boolean;
  disabled?: boolean;
  placeholder?: string;
  fill?: boolean;
  ariaLabel?: string;
}) {
  const size: CSSProperties = fill ? { flex: 1, minHeight: 0 } : { width: "100%", height: "70vh" };
  if (preview) {
    return (
      <div className="md-surface" style={{ ...size, overflow: "auto" }} aria-label={ariaLabel}>
        <Markdown text={value} />
      </div>
    );
  }
  return (
    <textarea
      value={value}
      onChange={(e) => onChange(e.target.value)}
      disabled={disabled}
      placeholder={placeholder}
      className="field"
      style={{ ...size, boxSizing: "border-box", resize: "none", fontFamily: font, fontSize: 13, lineHeight: 1.5 }}
    />
  );
}
