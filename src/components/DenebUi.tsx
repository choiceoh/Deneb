// deneb-ui — the agent draws interactive UI by emitting a ```deneb-ui fenced JSON
// node tree in its reply. We render it as real controls and round-trip button
// callbacks as new chat turns, exactly like the native client:
//   data → "Responded with: k: v, …"   ·   no data → "Pressed: <event>"
// Schema mirrors the gateway's denebui.go and the native DenebUiNode.kt / UiAction.kt.
// Nodes are AI-produced (a dynamic boundary), so they're typed loosely.
import { type ReactNode, useMemo, useState } from "react";
import { Markdown } from "./Markdown";

// Nodes are AI-produced JSON (a dynamic boundary) — typed loosely on purpose.
type Node = any;

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
function coerce(v: unknown): string {
  if (v == null) return "";
  if (Array.isArray(v)) return v.join(", ");
  if (typeof v === "boolean") return v ? "true" : "false";
  return String(v);
}

// Walk the tree once to seed input defaults and the required-id set.
function collectInputs(root: Node): { initial: Record<string, unknown>; required: Set<string> } {
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

const TEXT_STYLE: Record<string, React.CSSProperties> = {
  headline: { fontSize: 18, fontWeight: 600, letterSpacing: "-0.01em", color: "var(--ink)" },
  title: { fontSize: 15, fontWeight: 600, color: "var(--ink)" },
  body: { fontSize: 13, color: "var(--ink-2)" },
  caption: { fontSize: 11, color: "var(--muted-2)" },
};

// Render one agent-drawn UI block. Owns form + accordion-toggle state so a
// callback's collectFrom can gather live input values.
export function DenebUi({ spec, onSubmit, busy }: { spec: Node; onSubmit: (msg: string) => void; busy?: boolean }) {
  const { initial, required } = useMemo(() => collectInputs(spec), [spec]);
  const [form, setForm] = useState<Record<string, unknown>>(initial);
  const [toggles, setToggles] = useState<Record<string, boolean>>({});
  const setField = (id: string, v: unknown) => setForm((f) => ({ ...f, [id]: v }));

  function dispatch(action: Node) {
    if (!action || busy) return;
    switch (action.type) {
      case "open_url": {
        const u = String(action.url || "");
        if (/^https?:\/\//i.test(u)) window.open(u, "_blank", "noopener,noreferrer");
        return;
      }
      case "copy_to_clipboard":
        void navigator.clipboard?.writeText(String(action.text || "")).catch(() => {});
        return;
      case "toggle": {
        const id = String(action.targetId || "");
        if (id) setToggles((t) => ({ ...t, [id]: !(t[id] ?? false) }));
        return;
      }
      case "callback": {
        const from: string[] = Array.isArray(action.collectFrom) ? action.collectFrom : [];
        if (from.some((id) => required.has(id) && coerce(form[id]) === "")) return; // required gate
        const data: Record<string, string> = {};
        const stat = action.data && typeof action.data === "object" ? action.data : {};
        for (const k of Object.keys(stat)) data[k] = coerce(stat[k]);
        for (const id of from) data[id] = coerce(form[id]);
        const msg = Object.keys(data).length
          ? "Responded with: " +
            Object.entries(data)
              .map(([k, v]) => `${k}: ${v}`)
              .join(", ")
          : `Pressed: ${String(action.event || "")}`;
        onSubmit(msg);
        return;
      }
    }
  }

  function kids(n: Node, key: string): ReactNode {
    return (Array.isArray(n?.children) ? n.children : []).map((c: Node, i: number) => render(c, `${key}.${i}`));
  }

  function render(n: Node, key: string): ReactNode {
    if (!n || typeof n !== "object") return null;
    const id: string = typeof n.id === "string" ? n.id : "";
    switch (n.type) {
      // --- layout ---
      case "column":
        return (
          <div key={key} className="dui-col">
            {kids(n, key)}
          </div>
        );
      case "row":
        return (
          <div key={key} className="dui-row">
            {kids(n, key)}
          </div>
        );
      case "card":
        return (
          <div key={key} className="dui-card">
            {kids(n, key)}
          </div>
        );
      case "box":
        return (
          <div key={key} className="dui-col">
            {kids(n, key)}
          </div>
        );
      case "divider":
        return <hr key={key} className="dui-divider" />;
      case "accordion": {
        const open = toggles[id || key] ?? n.expanded === true;
        return (
          <div key={key} className="dui-card">
            <button className="dui-accordion-head" onClick={() => setToggles((t) => ({ ...t, [id || key]: !open }))}>
              <span>{String(n.title || "")}</span>
              <span className="dui-accordion-caret">{open ? "▾" : "▸"}</span>
            </button>
            {open && <div className="dui-col">{kids(n, key)}</div>}
          </div>
        );
      }
      case "list": {
        const items: Node[] = Array.isArray(n.items) ? n.items : [];
        const Tag = n.ordered ? "ol" : "ul";
        return (
          <Tag key={key} className="dui-list">
            {items.map((it, i) => (
              <li key={i}>{render(it, `${key}.${i}`)}</li>
            ))}
          </Tag>
        );
      }
      case "tabs": {
        const tabs: Node[] = Array.isArray(n.tabs) ? n.tabs : [];
        const sel = toggles[`tab:${key}`] !== undefined ? Number(toggles[`tab:${key}`]) : Number(n.selectedIndex ?? 0);
        return (
          <div key={key} className="dui-col">
            <div className="dui-tabs">
              {tabs.map((t, i) => (
                <button
                  key={i}
                  className={"dui-tab" + (i === sel ? " active" : "")}
                  onClick={() => setToggles((s) => ({ ...s, [`tab:${key}`]: i as unknown as boolean }))}
                >
                  {String(t?.label || `탭 ${i + 1}`)}
                </button>
              ))}
            </div>
            <div className="dui-col">
              {(Array.isArray(tabs[sel]?.children) ? tabs[sel].children : []).map((c: Node, i: number) =>
                render(c, `${key}.t${i}`),
              )}
            </div>
          </div>
        );
      }
      // --- content ---
      case "text":
        return (
          <div
            key={key}
            style={{
              ...(TEXT_STYLE[n.style as string] ?? TEXT_STYLE.body),
              ...(n.bold ? { fontWeight: 600 } : null),
              ...(n.italic ? { fontStyle: "italic" } : null),
              lineHeight: 1.5,
            }}
          >
            {String(n.value || "")}
          </div>
        );
      case "markdown":
        return <Markdown key={key} text={String(n.value || "")} />;
      case "code":
        return (
          <pre key={key} className="dui-code">
            <code>{String(n.code || "")}</code>
          </pre>
        );
      case "quote":
        return (
          <blockquote key={key} className="dui-quote">
            {String(n.text || "")}
            {n.source ? <footer className="dui-quote-src">— {String(n.source)}</footer> : null}
          </blockquote>
        );
      case "badge":
        return (
          <span key={key} className="dui-badge">
            {String(n.value || "")}
          </span>
        );
      case "stat":
        return (
          <div key={key} className="dui-stat">
            <div className="dui-stat-value">{String(n.value || "")}</div>
            <div className="dui-stat-label">{String(n.label || "")}</div>
            {n.description ? <div className="dui-stat-desc">{String(n.description)}</div> : null}
          </div>
        );
      case "image":
        return /^https?:\/\//i.test(String(n.url || "")) ? (
          <img key={key} className="dui-image" src={String(n.url)} alt={String(n.alt || "")} loading="lazy" />
        ) : null;
      case "avatar": {
        const nm = String(n.name || "");
        return (
          <div key={key} className="dui-avatar" title={nm}>
            {/^https?:\/\//i.test(String(n.imageUrl || "")) ? (
              <img src={String(n.imageUrl)} alt={nm} />
            ) : (
              (nm.trim()[0] ?? "?").toUpperCase()
            )}
          </div>
        );
      }
      case "table": {
        const headers: string[] = Array.isArray(n.headers) ? n.headers : [];
        const rows: string[][] = Array.isArray(n.rows) ? n.rows : [];
        return (
          <table key={key} className="md-table">
            <thead>
              <tr>
                {headers.map((h, i) => (
                  <th key={i}>{String(h)}</th>
                ))}
              </tr>
            </thead>
            <tbody>
              {rows.map((r, ri) => (
                <tr key={ri}>
                  {(Array.isArray(r) ? r : []).map((c, ci) => (
                    <td key={ci}>{String(c)}</td>
                  ))}
                </tr>
              ))}
            </tbody>
          </table>
        );
      }
      case "chart": {
        const labels: string[] = Array.isArray(n.labels) ? n.labels : [];
        const values: number[] = Array.isArray(n.values) ? n.values : [];
        const max = Math.max(1, ...values.map((v) => Number(v) || 0));
        return (
          <div key={key} className="dui-chart">
            {n.label ? <div className="dui-stat-label">{String(n.label)}</div> : null}
            {labels.map((l, i) => (
              <div key={i} className="dui-bar-row">
                <span className="dui-bar-label">{String(l)}</span>
                <span className="dui-bar-track">
                  <span className="dui-bar-fill" style={{ width: `${((Number(values[i]) || 0) / max) * 100}%` }} />
                </span>
                <span className="dui-bar-val">{String(values[i] ?? "")}</span>
              </div>
            ))}
          </div>
        );
      }
      // --- feedback ---
      case "alert":
        return (
          <div key={key} className={`dui-alert ${String(n.severity || "info")}`}>
            {n.title ? <div className="dui-alert-title">{String(n.title)}</div> : null}
            <div>{String(n.message || "")}</div>
          </div>
        );
      case "progress": {
        const v = typeof n.value === "number" ? Math.max(0, Math.min(1, n.value)) : null;
        return (
          <div key={key} className="dui-col">
            {n.label ? <div className="dui-stat-label">{String(n.label)}</div> : null}
            <span className="dui-progress">
              <span
                className="dui-progress-fill"
                style={v == null ? { width: "40%", opacity: 0.5 } : { width: `${v * 100}%` }}
              />
            </span>
          </div>
        );
      }
      case "countdown":
        return (
          <div key={key} className="dui-stat-label">
            {String(n.label || "")} {n.seconds ? `· ${Number(n.seconds)}s` : ""}
          </div>
        );
      // --- interactive ---
      case "button": {
        const variant = String(n.variant || "filled");
        const accent = variant === "filled" || variant === "tonal";
        return (
          <button
            key={key}
            className={"btn" + (accent ? " btn-accent" : "")}
            disabled={busy || n.enabled === false}
            onClick={() => dispatch(n.action)}
          >
            {String(n.label || "")}
          </button>
        );
      }
      case "text_input": {
        const common = {
          className: "field",
          placeholder: String(n.placeholder || ""),
          value: String(form[id] ?? ""),
          disabled: busy,
          onChange: (e: React.ChangeEvent<HTMLInputElement | HTMLTextAreaElement>) => setField(id, e.target.value),
        };
        return (
          <label key={key} className="dui-field">
            {n.label ? <span className="dui-label">{String(n.label)}</span> : null}
            {n.multiline ? (
              <textarea {...common} rows={3} style={{ resize: "vertical" }} />
            ) : (
              <input {...common} type={n.keyboard === "number" || n.keyboard === "decimal" ? "number" : "text"} />
            )}
          </label>
        );
      }
      case "date_input":
      case "time_input":
        return (
          <label key={key} className="dui-field">
            {n.label ? <span className="dui-label">{String(n.label)}</span> : null}
            <input
              className="field"
              type={n.type === "date_input" ? "date" : "time"}
              value={String(form[id] ?? "")}
              disabled={busy}
              onChange={(e) => setField(id, e.target.value)}
            />
          </label>
        );
      case "checkbox":
      case "switch":
        return (
          <label key={key} className="dui-check">
            <input
              type="checkbox"
              checked={form[id] === true}
              disabled={busy}
              onChange={(e) => setField(id, e.target.checked)}
            />
            <span>{String(n.label || "")}</span>
          </label>
        );
      case "select":
        return (
          <label key={key} className="dui-field">
            {n.label ? <span className="dui-label">{String(n.label)}</span> : null}
            <select
              className="field"
              value={String(form[id] ?? "")}
              disabled={busy}
              onChange={(e) => setField(id, e.target.value)}
            >
              <option value="">{String(n.placeholder || "선택…")}</option>
              {(Array.isArray(n.options) ? n.options : []).map((o: string, i: number) => (
                <option key={i} value={String(o)}>
                  {String(o)}
                </option>
              ))}
            </select>
          </label>
        );
      case "radio_group":
        return (
          <div key={key} className="dui-field" role="radiogroup">
            {n.label ? <span className="dui-label">{String(n.label)}</span> : null}
            {(Array.isArray(n.options) ? n.options : []).map((o: string, i: number) => (
              <label key={i} className="dui-check">
                <input
                  type="radio"
                  name={id || key}
                  checked={String(form[id] ?? "") === String(o)}
                  disabled={busy}
                  onChange={() => setField(id, String(o))}
                />
                <span>{String(o)}</span>
              </label>
            ))}
          </div>
        );
      case "slider": {
        const min = Number(n.min ?? 0);
        const max = Number(n.max ?? 100);
        return (
          <label key={key} className="dui-field">
            {n.label ? (
              <span className="dui-label">
                {String(n.label)} · {String(form[id] ?? min)}
              </span>
            ) : null}
            <input
              type="range"
              min={min}
              max={max}
              step={Number(n.step ?? 1)}
              value={Number(form[id] ?? min)}
              disabled={busy}
              onChange={(e) => setField(id, Number(e.target.value))}
            />
          </label>
        );
      }
      case "chip_group": {
        const chips: Node[] = Array.isArray(n.chips) ? n.chips : [];
        const multi = n.selection === "multi";
        const cur = form[id];
        const isOn = (val: string) => (multi ? Array.isArray(cur) && cur.includes(val) : cur === val);
        const toggle = (val: string) => {
          if (n.selection === "none") return;
          if (multi) {
            const arr = Array.isArray(cur) ? [...cur] : [];
            const at = arr.indexOf(val);
            if (at >= 0) arr.splice(at, 1);
            else arr.push(val);
            setField(id, arr);
          } else {
            setField(id, cur === val ? "" : val);
          }
        };
        return (
          <div key={key} className="dui-chips">
            {chips.map((c, i) => {
              const val = String(c?.value ?? c?.label ?? "");
              return (
                <button
                  key={i}
                  className={"chip" + (isOn(val) ? " on" : "")}
                  disabled={busy || n.selection === "none"}
                  onClick={() => toggle(val)}
                >
                  {String(c?.label ?? val)}
                </button>
              );
            })}
          </div>
        );
      }
      default:
        return null; // unknown node → render nothing (lenient, matches gateway tolerance)
    }
  }

  return <div className="dui">{render(spec, "n")}</div>;
}

// Render a streamed assistant text part: Markdown spans interleaved with
// agent-drawn deneb-ui blocks (a pending block shows a quiet placeholder).
export function AssistantText({
  text,
  onUiSubmit,
  busy,
}: {
  text: string;
  onUiSubmit: (msg: string) => void;
  busy?: boolean;
}) {
  const segments = splitDenebUi(text);
  return (
    <>
      {segments.map((seg, i) => {
        if (seg.kind === "md") return <Markdown key={i} text={seg.text} />;
        if (seg.kind === "ui-pending")
          return (
            <div key={i} className="dui-pending">
              UI 생성 중…
            </div>
          );
        const spec = parseDenebUi(seg.body);
        return spec ? (
          <DenebUi key={i} spec={spec} onSubmit={onUiSubmit} busy={busy} />
        ) : (
          <pre key={i} className="dui-code">
            <code>{seg.body}</code>
          </pre>
        );
      })}
    </>
  );
}
