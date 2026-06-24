// deneb-ui parsing + tree helpers — the pure (non-React) half of rendering
// agent-drawn UI. Extracted from components/DenebUi.tsx so the parser, the form
// coercion, and the input-seeding tree walk can be unit-tested in isolation and
// the React component file holds only rendering.
//
// Nodes are AI-produced JSON (a dynamic boundary), so they're typed loosely on
// purpose. Schema mirrors the gateway's denebui.go and the native parser.

// Nodes are AI-produced JSON (a dynamic boundary) — typed loosely on purpose.
export type Node = any;

const FENCE_OPEN = /^```\s*deneb-ui\s*$/i;
const FENCE_CLOSE = /^```\s*$/;

export type UiSegment = { kind: "md"; text: string } | { kind: "ui"; body: string } | { kind: "ui-pending" };

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
    if (FENCE_OPEN.test(lines[i].trim())) {
      flush();
      const body: string[] = [];
      let closed = false;
      for (i++; i < lines.length; i++) {
        if (FENCE_CLOSE.test(lines[i].trim())) {
          closed = true;
          break;
        }
        body.push(lines[i]);
      }
      if (!closed) {
        segs.push({ kind: "ui-pending" });
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

// Parse a fence body into a node tree. Tolerates a bare array (→ column) and
// NDJSON (one object per line → column), matching the native lenient parser.
export function parseDenebUi(body: string): Node | null {
  const t = body.trim();
  if (!t) return null;
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

export const TEXT_STYLE: Record<string, React.CSSProperties> = {
  headline: { fontSize: 18, fontWeight: 600, letterSpacing: "-0.01em", color: "var(--ink)" },
  title: { fontSize: 15, fontWeight: 600, color: "var(--ink)" },
  body: { fontSize: 13, color: "var(--ink-2)" },
  caption: { fontSize: 11, color: "var(--muted-2)" },
};
