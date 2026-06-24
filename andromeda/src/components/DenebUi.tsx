// deneb-ui — the agent draws interactive UI by emitting a ```deneb-ui fenced JSON
// node tree in its reply. We render it as real controls and round-trip button
// callbacks as new chat turns, exactly like the native client:
//   data → "Responded with: k: v, …"   ·   no data → "Pressed: <event>"
// Schema mirrors the gateway's denebui.go and the native DenebUiNode.kt / UiAction.kt.
// Nodes are AI-produced (a dynamic boundary), so they're typed loosely.
//
// The pure parser + tree helpers live in markdown/denebUiParse.ts; this file
// holds the React rendering (DenebUi component + AssistantText stream wrapper).
import { type ReactNode, useMemo, useState } from "react";
import { type Node, coerce, collectInputs, parseDenebUi, splitDenebUi, TEXT_STYLE } from "@/markdown/denebUiParse";
import { Markdown } from "./Markdown";

export type { Node } from "@/markdown/denebUiParse";
export type { UiSegment } from "@/markdown/denebUiParse";
// Re-export the pure parser/helpers so existing imports from "./DenebUi" keep
// working (tests, etc.). Their real home is markdown/denebUiParse.ts.
export { coerce, collectInputs, parseDenebUi, splitDenebUi } from "@/markdown/denebUiParse";

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
    <div className="assistant-text">
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
    </div>
  );
}
