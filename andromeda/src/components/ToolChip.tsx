// A single tool-call chip in the AI transcript: the visible half of two-way
// collaboration. A `started` frame shows a spinner; the matching `completed`
// frame (same toolUseId) flips it to a ✓ (or ✕ on error) and reveals the
// gateway's one-line `detail` (e.g. "메일 3건"). Mirrors the native client's
// inline tool rows.
import { useState } from "react";
import type { ToolPart } from "@/hooks";
import { Icon } from "./Icon";

// Humanize a raw tool id ("gmail.list_recent" → "gmail list recent") so the
// chip reads without a per-tool label table we don't have client-side.
function toolLabel(tool: string): string {
  return tool.replace(/[._]/g, " ").replace(/\s+/g, " ").trim();
}

// Classify one preview line for display tinting. Diff/patch output is the
// bread-and-butter of the companion's coding use — tint added/removed/hunk
// lines so a diff reads as a diff. Display-only: the gateway owns the text.
export function previewLineClass(line: string): string {
  if (line.startsWith("@@")) return "hunk";
  if (line.startsWith("+") && !line.startsWith("+++")) return "add";
  if (line.startsWith("-") && !line.startsWith("---")) return "del";
  return "";
}

// How long a finished call took, in the units a coding surface reads fastest:
// milliseconds below a second (a 40ms cache hit and a 900ms one are different
// facts), seconds above it. Exported for its unit test.
export function formatElapsed(ms: number): string {
  if (!Number.isFinite(ms) || ms < 0) return "";
  if (ms < 1000) return `${Math.round(ms)}ms`;
  if (ms < 60_000) return `${(ms / 1000).toFixed(ms < 10_000 ? 1 : 0)}초`;
  const m = Math.floor(ms / 60_000);
  const s = Math.round((ms % 60_000) / 1000);
  return s ? `${m}분 ${s}초` : `${m}분`;
}

function PreviewBody({ text }: { text: string }) {
  const [copied, setCopied] = useState(false);
  const lines = text.split("\n");
  const isDiff = lines.some((l) => previewLineClass(l) !== "");
  return (
    <div className="tool-chip-preview-wrap">
      <button
        type="button"
        className="tool-chip-copy"
        title="출력 복사"
        onClick={(e) => {
          e.preventDefault(); // keep the disclosure open state untouched
          void navigator.clipboard?.writeText(text).catch(() => {});
          setCopied(true);
          window.setTimeout(() => setCopied(false), 1200);
        }}
      >
        {copied ? "복사됨" : "복사"}
      </button>
      <pre className="tool-chip-preview">
        {isDiff
          ? lines.map((l, i) => {
              const k = previewLineClass(l);
              return (
                <span key={i} className={k ? `pv-${k}` : undefined}>
                  {l}
                  {i < lines.length - 1 ? "\n" : ""}
                </span>
              );
            })
          : text}
      </pre>
    </div>
  );
}

export function ToolChip({ part }: { part: ToolPart }) {
  const done = part.state === "completed";
  const cls = "tool-chip" + (part.isError ? " error" : done ? " done" : " running");
  // The icon alone carries the running/done/error state visually — label it so
  // assistive tech announces it too (otherwise the chip reads as just a name).
  const stateText = part.isError ? "실패" : done ? "완료" : "실행 중";
  const row = (
    <>
      <span className="tool-chip-ico" role="img" aria-label={stateText}>
        {!done ? (
          <span className="tool-spin" />
        ) : part.isError ? (
          <Icon name="close" size={12} />
        ) : (
          <Icon name="check" size={12} />
        )}
      </span>
      <span className="tool-chip-name">{toolLabel(part.tool)}</span>
      {part.detail ? <span className="tool-chip-detail">{part.detail}</span> : null}
      {/* What the call produced — the gateway authors this line so every client
          shows the same wording. Started chips have none yet. */}
      {part.resultSummary ? <span className="tool-chip-result">{part.resultSummary}</span> : null}
      {/* What the step cost, trailing the row. Only on finished calls we timed
          ourselves — a restored transcript carries no duration and must not
          invent one. */}
      {done && part.elapsedMs !== undefined ? (
        <span className="tool-chip-elapsed">{formatElapsed(part.elapsedMs)}</span>
      ) : null}
    </>
  );
  // Without a preview the chip stays a plain row; with one it becomes a
  // disclosure whose summary IS that row, so the closed state looks identical.
  if (!part.resultPreview) return <div className={cls}>{row}</div>;
  return (
    <details className={cls + " tool-chip-expandable"}>
      <summary>{row}</summary>
      <PreviewBody text={part.resultPreview} />
    </details>
  );
}
