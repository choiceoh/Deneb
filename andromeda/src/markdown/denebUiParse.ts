// deneb-ui parsing + tree helpers — the pure (non-React) half of rendering
// agent-drawn UI. Extracted from components/DenebUi.tsx so the parser, the form
// coercion, and the input-seeding tree walk can be unit-tested in isolation and
// the React component file holds only rendering.
//
// Nodes are AI-produced (a dynamic boundary), so they're typed loosely on
// purpose. Schema mirrors the gateway's denebui.go and the native parser.

import { parseDenebUiHtml } from "./denebUiHtml";

// Nodes are AI-produced (a dynamic boundary) — typed loosely on purpose.
export type Node = any;

// Lenient opener: the fence normally occupies its own line, but models
// sometimes glue it to the tail of a prose sentence ("…가져올게요.```deneb-ui")
// or run straight into the first tag ("…```deneb-ui<column>"). Group 1
// captures the prose prefix so it stays markdown; group 2 captures a glued
// body start — accepted only when it begins with "<", so mid-sentence
// mentions of the fence stay prose.
const FENCE_OPEN = /^(.*?)`{3,}\s*deneb-ui\s*(<.*)?$/i;
const FENCE_CLOSE = /^`{3,}\s*$/;
// A ``` run glued into an HTML body line closes the fence: HTML bodies escape
// backticks as &#96; per the authoring contract, so a raw run can only be the
// close. Legacy JSON bodies keep the strict own-line close (their string
// values may carry ```).
const CLOSE_RUN = /`{3,}/;

export type UiSegment =
  | { kind: "md"; text: string }
  | { kind: "ui"; body: string }
  // Unclosed trailing fence mid-stream; body is the partial content so HTML
  // blocks can render progressively (EOF auto-close makes partials parseable).
  | { kind: "ui-pending"; body: string };

// Split a (possibly mid-stream) assistant text part into Markdown spans and
// deneb-ui blocks. An unclosed trailing fence → a pending placeholder.
export function splitDenebUi(text: string): UiSegment[] {
  if (!text.includes("```")) return text ? [{ kind: "md", text }] : [];
  const lines = text.split("\n");
  const segs: UiSegment[] = [];
  let md: string[] = [];
  const flush = () => {
    const t = md.join("\n");
    if (t.trim()) segs.push({ kind: "md", text: t });
    md = [];
  };
  for (let i = 0; i < lines.length; i++) {
    const open = FENCE_OPEN.exec(lines[i]);
    if (open) {
      if (open[1].trim()) md.push(open[1]);
      flush();
      const body: string[] = [];
      let closed = false;
      let isHtml = open[2] != null;
      let htmlDecided = isHtml;
      // Consumes one body line; splits a glued ``` close out of HTML bodies.
      const consume = (line: string): boolean => {
        if (FENCE_CLOSE.test(line.trim())) return true;
        if (!htmlDecided && line.trim()) {
          isHtml = line.trimStart().startsWith("<");
          htmlDecided = true;
        }
        if (isHtml) {
          const run = CLOSE_RUN.exec(line);
          if (run) {
            const pre = line.slice(0, run.index);
            if (pre.trim()) body.push(pre);
            const suffix = line.slice(run.index + run[0].length);
            if (suffix.trim()) md.push(suffix); // prose after the glued close
            return true;
          }
        }
        body.push(line);
        return false;
      };
      if (open[2] != null) closed = consume(open[2]);
      for (; !closed && i + 1 < lines.length;) {
        i++;
        closed = consume(lines[i]);
      }
      if (!closed) {
        segs.push({ kind: "ui-pending", body: body.join("\n") });
        return segs; // streaming: nothing useful after an open block yet
      }
      segs.push({ kind: "ui", body: body.join("\n") });
    } else {
      md.push(lines[i]);
    }
  }
  flush();
  return segs;
}

// Parse a fence body into a node tree. Bodies starting with `<` use the
// labeled-HTML wire format (v2 — the authoring format since 2026-07); `{`/`[`
// bodies take the legacy strict-JSON path (single object, bare array → column,
// NDJSON → column), kept for old transcripts.
export function parseDenebUi(body: string): Node | null {
  const t = body.trim();
  if (!t) return null;
  if (t.startsWith("<")) return parseDenebUiHtml(t);
  try {
    const v = JSON.parse(t);
    if (Array.isArray(v)) return { type: "column", children: v };
    return v && typeof v === "object" ? v : null;
  } catch {
    const nodes: Node[] = [];
    for (const ln of t.split("\n")) {
      const s = ln.trim();
      if (!s) continue;
      try {
        nodes.push(JSON.parse(s));
      } catch {
        return null;
      }
    }
    return nodes.length ? { type: "column", children: nodes } : null;
  }
}

// Node types a user can act on. Progressive streaming render paints partial
// display-only trees live but holds a placeholder for these — a half-built
// form must not accept clicks mid-stream. Mirrors the native client's
// DenebUiInteractivity.kt.
const INTERACTIVE_TYPES = new Set([
  "button",
  "text_input",
  "date_input",
  "time_input",
  "checkbox",
  "select",
  "switch",
  "slider",
  "radio_group",
  "chip_group",
  "countdown",
]);

// True when the tree contains any interactive node (see INTERACTIVE_TYPES).
export function hasInteractiveNode(n: Node): boolean {
  if (!n || typeof n !== "object") return false;
  if (Array.isArray(n)) return n.some(hasInteractiveNode);
  if (INTERACTIVE_TYPES.has(n.type)) return true;
  if (hasInteractiveNode(n.children) || hasInteractiveNode(n.items)) return true;
  return Array.isArray(n.tabs) && n.tabs.some((t: Node) => hasInteractiveNode(t?.children));
}

// Coerce a form value to the string a callback sends (native dataAsStrings).
export function coerce(v: unknown): string {
  if (v == null) return "";
  if (Array.isArray(v)) return v.join(", ");
  if (typeof v === "boolean") return v ? "true" : "false";
  return String(v);
}

// Walk the tree once to seed input defaults and the required-id set.
export function collectInputs(root: Node): { initial: Record<string, unknown>; required: Set<string> } {
  const initial: Record<string, unknown> = {};
  const required = new Set<string>();
  const walk = (n: Node) => {
    if (!n || typeof n !== "object") return;
    if (Array.isArray(n)) return n.forEach(walk);
    const { type, id } = n;
    if (typeof id === "string" && id) {
      if (type === "text_input" || type === "date_input" || type === "time_input") initial[id] = n.value ?? "";
      else if (type === "select" || type === "radio_group") initial[id] = n.selected ?? "";
      else if (type === "checkbox" || type === "switch") initial[id] = n.checked === true;
      else if (type === "slider") initial[id] = n.value ?? n.min ?? 0;
      else if (type === "chip_group") initial[id] = n.selection === "multi" ? [] : "";
      if (n.required === true) required.add(id);
    }
    walk(n.children);
    walk(n.items);
    if (Array.isArray(n.tabs)) n.tabs.forEach((t: Node) => walk(t?.children));
  };
  walk(root);
  return { initial, required };
}

// --- deneb-ui round-trip user messages -------------------------------------
// A card submission comes back as a machine-shaped user message ("Pressed:
// <event>" / "Responded with: k: v, …" — composed in DenebUi.dispatch and the
// native ChatViewModel.submitUiCallback). Parse it back so the transcript can
// present the answer as a human card-reply instead of the raw wire text; this
// also covers transcript reloads, where only the raw text survives.
export interface UiSubmission {
  /** callback event name — "" when collected values were submitted */
  event: string;
  /** ordered k/v pairs from "Responded with:" — empty for a bare press */
  values: [string, string][];
}

const PRESSED_RE = /^Pressed: (\S+)$/;
const RESPONDED_PREFIX = "Responded with: ";
// A pair starts at a space-free key followed by ": ". Segments that fail this
// (a comma run inside a value) merge back into the previous value.
const PAIR_START_RE = /^(\S+): ([\s\S]*)$/;

export function parseUiSubmission(text: string): UiSubmission | null {
  const t = text.trim();
  const pressed = PRESSED_RE.exec(t);
  if (pressed) return { event: pressed[1], values: [] };
  if (!t.startsWith(RESPONDED_PREFIX)) return null;
  const values: [string, string][] = [];
  for (const seg of t.slice(RESPONDED_PREFIX.length).split(", ")) {
    const m = PAIR_START_RE.exec(seg);
    if (m) values.push([m[1], m[2]]);
    else if (values.length > 0) values[values.length - 1][1] += ", " + seg;
    else return null; // first segment isn't "k: v" — an ordinary message
  }
  return values.length > 0 ? { event: "", values } : null;
}

export const TEXT_STYLE: Record<string, React.CSSProperties> = {
  // Editorial hero voice (parity with the native 28sp Light headline,
  // scaled for desktop panel density).
  headline: { fontSize: 24, fontWeight: 300, letterSpacing: "-0.015em", color: "var(--ink)" },
  title: { fontSize: 15, fontWeight: 600, color: "var(--ink)" },
  body: { fontSize: 13, color: "var(--ink-2)" },
  caption: { fontSize: 11, color: "var(--muted-2)" },
};
