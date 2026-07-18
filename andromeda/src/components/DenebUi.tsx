// deneb-ui — the agent draws interactive UI by emitting a ```deneb-ui fenced JSON
// node tree in its reply. We render it as real controls and round-trip button
// callbacks as new chat turns, exactly like the native client:
//   data → "Responded with: k: v, …"   ·   no data → "Pressed: <event>"
// Schema mirrors the gateway's denebui.go and the native DenebUiNode.kt / UiAction.kt.
// Nodes are AI-produced (a dynamic boundary), so they're typed loosely.
//
// The pure parser + tree helpers live in markdown/denebUiParse.ts; this file
// holds the React rendering (DenebUi component + AssistantText stream wrapper).
import { type ReactNode, useEffect, useMemo, useState } from "react";
import {
  type Node,
  coerce,
  collectInputs,
  hasInteractiveNode,
  parseDenebUi,
  splitDenebUi,
  TEXT_STYLE,
} from "@/markdown/denebUiParse";
import { Icon, type IconName } from "./Icon";
import { CodeBlock, Markdown } from "./Markdown";
import { renderInline } from "./renderInline";

// --- deneb-ui text conventions (parity with the native renderer) ---

/** Supported badge tint classes — attr values are model-authored, so gate
 * them against class injection into global CSS (review catch on #3235). */
const BADGE_TINTS = new Set(["success", "warning", "error", "primary", "secondary"]);

/** text/icon status tints (same gate rationale as BADGE_TINTS). Mirrors the
 * native renderer's color mapping so a status-toned line reads identically
 * on phone and desktop. */
const TEXT_TINTS = new Set(["primary", "secondary", "error", "success", "warning"]);

/** Alert severities with dedicated styling — anything else renders as info. */
const ALERT_SEVERITIES = new Set(["info", "success", "warning", "error"]);

/** box contentAlignment → flex placement (native RenderBox parity). Layout only
 * (no color), so inline style is fine; unknown values fall back to a plain column. */
const BOX_ALIGN: Record<string, React.CSSProperties> = {
  center: { alignItems: "center", justifyContent: "center" },
  top: { alignItems: "center", justifyContent: "flex-start" },
  bottom: { alignItems: "center", justifyContent: "flex-end" },
  start: { alignItems: "flex-start" },
  end: { alignItems: "flex-end" },
  top_start: { alignItems: "flex-start", justifyContent: "flex-start" },
  top_end: { alignItems: "flex-end", justifyContent: "flex-start" },
  bottom_start: { alignItems: "flex-start", justifyContent: "flex-end" },
  bottom_end: { alignItems: "flex-end", justifyContent: "flex-end" },
};

/** deneb-ui icon vocabulary (the Material-ish names the authoring contract
 * teaches) → the andromeda line-icon catalog. Covers the section-header set
 * the letters/briefings actually emit; unknown names fall back to emoji text
 * or nothing (native parity). */
const ICON_GLYPHS: Record<string, IconName> = {
  calendar: "calendar",
  date_range: "calendar",
  schedule: "calendar",
  mail: "mail",
  email: "mail",
  sunny: "today",
  weather: "today",
  alarm: "crons",
  clock: "crons",
  access_time: "crons",
  timer: "crons",
  person: "people",
  group: "people",
  account_circle: "people",
  search: "search",
  settings: "settings",
  check: "check",
  done: "check",
  check_circle: "check",
  task: "check",
  task_alt: "check",
  warning: "warning",
  payments: "coin",
  credit_card: "coin",
  savings: "coin",
  money: "coin",
  bar_chart: "progress",
  chart: "progress",
  analytics: "progress",
  dashboard: "progress",
  trending_up: "workfeed",
  show_chart: "workfeed",
  notifications: "bell",
  code: "code",
  terminal: "code",
  attach_file: "attach",
  attachment: "attach",
  add: "plus",
  delete: "trash",
  refresh: "refresh",
  sync: "refresh",
  send: "send",
  reply: "send",
  forward: "send",
  work: "projects",
  business: "projects",
  // Broadened coverage — more of the Material-ish vocab the authoring contract
  // teaches, mapped to the closest existing andromeda glyph so section-header
  // icons stop vanishing on desktop (full icon parity would need new vectors).
  description: "files",
  article: "files",
  folder: "files",
  folder_open: "files",
  assignment: "todo",
  checklist: "todo",
  list: "todo",
  list_alt: "todo",
  event: "calendar",
  event_note: "calendar",
  done_all: "check",
  verified: "check",
  notifications_active: "bell",
  notification_important: "bell",
  campaign: "bell",
  trending_down: "progress",
  insights: "progress",
  delete_forever: "trash",
  delete_outline: "trash",
  forum: "chat",
  comment: "chat",
  message: "chat",
  chat_bubble: "chat",
  update: "history",
  restore: "history",
  print: "printer",
  add_circle: "plus",
  cancel: "close",
  groups: "people",
  autorenew: "refresh",
  cached: "refresh",
};

/** "HH:MM — 제목" schedule item — such lists render as a timeline. */
const TIMELINE_RE = /^\d{1,2}:\d{2}\s*—\s*.+$/;
/** Short "키 — 내용" lead (time, sender) rendered as a bold scan point. */
const KEY_DASH_RE = /^(.{1,14}?) — ([\s\S]*)$/;

/** First numeric run (commas/decimal allowed) inside a stat value. */
const STAT_NUM_RE = /\d[\d,]*(?:\.\d+)?/;

/** One frame of the stat count-up: the numeric run scaled by an eased
 * [progress], prefix/suffix intact, decimal width and comma grouping matching
 * the target. progress ≥ 1 returns the ORIGINAL string so exact metrics
 * ("12.45%", 2-decimal FX) keep their full precision (native-parity rule). */
export function statCountUpFrame(value: string, progress: number): string {
  if (progress >= 1) return value;
  const m = STAT_NUM_RE.exec(value);
  if (!m) return value;
  const target = Number(m[0].replace(/,/g, ""));
  if (!Number.isFinite(target)) return value;
  const decimals = m[0].includes(".") ? m[0].length - m[0].indexOf(".") - 1 : 0;
  const eased = 1 - Math.pow(1 - Math.max(0, progress), 3); // fast-out-slow-in
  const v = target * eased;
  const s = m[0].includes(",")
    ? v.toLocaleString("ko-KR", { minimumFractionDigits: decimals, maximumFractionDigits: decimals })
    : v.toFixed(decimals);
  return value.slice(0, m.index) + s + value.slice(m.index + m[0].length);
}

// Count-up entrance for stat values (native parity): the numeric run rolls
// 0→target once (~600ms, rAF); tabular-nums in CSS keeps digits from
// jittering. Static wherever motion is reduced or matchMedia is absent
// (jsdom) — the same "pin motion off in static contexts" rule as the native
// LocalDenebUiMotion switch.
function StatValue({ value }: { value: string }) {
  const motion =
    typeof window.matchMedia === "function" && !window.matchMedia("(prefers-reduced-motion: reduce)").matches;
  const [display, setDisplay] = useState(() => (motion ? statCountUpFrame(value, 0) : value));
  useEffect(() => {
    if (!motion) {
      setDisplay(value);
      return;
    }
    const t0 = performance.now();
    let raf = requestAnimationFrame(function frame(now: number) {
      const p = Math.min(1, (now - t0) / 600);
      setDisplay(statCountUpFrame(value, p));
      if (p < 1) raf = requestAnimationFrame(frame);
    });
    return () => cancelAnimationFrame(raf);
  }, [value, motion]);
  return <div className="dui-stat-value">{display}</div>;
}

// Live countdown readout — parity with the native RenderCountdown: ticks every
// second to H:MM:SS (or MM:SS under an hour) as a large tabular-nums number, with
// an expiry tint at zero. The native also fires the node's action on expiry; the
// desktop keeps the visual tick only (a proactive card's countdown is a display
// element, and the feed renders cards non-interactively). Date.now lives in the
// effect, never at render, so it stays purity-lint clean.
function Countdown({ seconds, label }: { seconds: number; label: string }) {
  const [remaining, setRemaining] = useState(() => Math.max(0, Math.floor(seconds)));
  useEffect(() => {
    const target = Date.now() + Math.max(0, Math.floor(seconds)) * 1000;
    const tick = () => setRemaining(Math.max(0, Math.round((target - Date.now()) / 1000)));
    tick();
    const id = setInterval(tick, 1000);
    return () => clearInterval(id);
  }, [seconds]);
  const two = (x: number) => String(x).padStart(2, "0");
  const h = Math.floor(remaining / 3600);
  const m = Math.floor((remaining % 3600) / 60);
  const s = remaining % 60;
  const formatted = h > 0 ? `${h}:${two(m)}:${two(s)}` : `${two(m)}:${two(s)}`;
  return (
    <div className="dui-countdown">
      {label ? <div className="dui-stat-label">{label}</div> : null}
      <div className={"dui-countdown-value" + (remaining <= 0 ? " expired" : "")}>{formatted}</div>
    </div>
  );
}

// Render one agent-drawn UI block. Owns form + accordion-toggle state so a
// callback's collectFrom can gather live input values.
export function DenebUi({ spec, onSubmit, busy }: { spec: Node; onSubmit: (msg: string) => void; busy?: boolean }) {
  const { initial, required } = useMemo(() => collectInputs(spec), [spec]);
  const [form, setForm] = useState<Record<string, unknown>>(initial);
  const [toggles, setToggles] = useState<Record<string, boolean>>({});
  // Required inputs a blocked submit flagged — cleared per field on edit.
  // Native parity (UiFormValidation): the old silent return made the button
  // feel dead when a required field was empty.
  const [invalid, setInvalid] = useState<Set<string>>(new Set());
  const setField = (id: string, v: unknown) => {
    setForm((f) => ({ ...f, [id]: v }));
    setInvalid((prev) => {
      if (!prev.has(id)) return prev;
      const next = new Set(prev);
      next.delete(id);
      return next;
    });
  };

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
        const missing = from.filter((id) => required.has(id) && coerce(form[id]) === "");
        if (missing.length > 0) {
          setInvalid((prev) => new Set([...prev, ...missing])); // required gate + visible flag
          return;
        }
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

  // Required-flag presentation shared by every input case.
  const fieldClass = (id: string) => "dui-field" + (invalid.has(id) ? " invalid" : "");
  const reqHint = (id: string) => (invalid.has(id) ? <span className="dui-req">필수 입력입니다</span> : null);

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
      case "card": {
        // Letter/briefing convention: an icon + caption first row is the
        // card's section label — give it the tracked label voice instead of
        // an ordinary caption line (parity with the native renderer).
        const ch: Node[] = Array.isArray(n.children) ? (n.children as Node[]) : [];
        const first = ch[0] as Node | undefined;
        const firstKids: Node[] = Array.isArray(first?.children) ? (first.children as Node[]) : [];
        const isHeader =
          first?.type === "row" &&
          firstKids.length === 2 &&
          firstKids[0]?.type === "icon" &&
          firstKids[1]?.type === "text" &&
          String(firstKids[1]?.style) === "caption";
        return (
          <div key={key} className="dui-card">
            {isHeader ? (
              <div className="dui-card-hd">
                {render(firstKids[0], `${key}.hicon`)}
                <span className="dui-card-hd-label">{String(firstKids[1].value || firstKids[1].text || "")}</span>
              </div>
            ) : null}
            {(isHeader ? ch.slice(1) : ch).map((c, i) => render(c, `${key}.${i}`))}
          </div>
        );
      }
      case "box":
        // Single-child alignment (native RenderBox contentAlignment); unknown /
        // absent → a plain column.
        return (
          <div key={key} className="dui-col" style={BOX_ALIGN[String(n.contentAlignment || "").toLowerCase()]}>
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
        // items may be deneb-ui nodes OR plain strings (a common model shorthand,
        // e.g. "items": ["a","b"]). Render strings as text so list rows never come
        // out as empty bullets when the model skips the {type:"text",...} wrapper.
        const items: unknown[] = Array.isArray(n.items) ? n.items : [];
        const itemText = (it: unknown): string | null => {
          if (typeof it === "string") return it;
          const node = it as Node;
          if (node && node.type === "text" && !node.style && !node.bold && !node.color) {
            return String(node.value || node.text || "");
          }
          return null;
        };
        // Schedule shape → timeline: right-aligned time, accent dot, title.
        if (!n.ordered && items.length > 0 && items.every((it) => TIMELINE_RE.test((itemText(it) ?? "").trim()))) {
          return (
            <div key={key} className="dui-timeline">
              {items.map((it, i) => {
                const v = (itemText(it) ?? "").trim();
                const dash = v.indexOf("—");
                const time = v.slice(0, dash).trim();
                const title = v.slice(dash + 1).trim();
                return (
                  <div key={i} className="dui-timeline-row">
                    <span className="dui-timeline-time">{time}</span>
                    <span className="dui-timeline-dot" />
                    <span className="dui-timeline-title">{renderInline(title, `${key}.${i}`)}</span>
                  </div>
                );
              })}
            </div>
          );
        }
        const Tag = n.ordered ? "ol" : "ul";
        return (
          <Tag key={key} className="dui-list">
            {items.map((it, i) => {
              const text = itemText(it);
              if (text !== null) {
                const m = KEY_DASH_RE.exec(text);
                return (
                  <li key={i}>
                    {m ? (
                      <>
                        {/* The key runs through inline rendering too — models
                            often **mark** it themselves and a raw render
                            showed the literal asterisks (2026-07-07 letter). */}
                        <strong>{renderInline(m[1], `${key}.${i}k`)}</strong>
                        {" — "}
                        {renderInline(m[2], `${key}.${i}`)}
                      </>
                    ) : (
                      renderInline(text, `${key}.${i}`)
                    )}
                  </li>
                );
              }
              return <li key={i}>{render(it as Node, `${key}.${i}`)}</li>;
            })}
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
      case "text": {
        const tint = TEXT_TINTS.has(String(n.color)) ? String(n.color) : "";
        const base: React.CSSProperties = { ...(TEXT_STYLE[n.style as string] ?? TEXT_STYLE.body) };
        if (tint) delete base.color; // the tint class owns the color
        return (
          <div
            key={key}
            className={"dui-text" + (tint ? ` ${tint}` : "")}
            style={{
              ...base,
              ...(n.bold ? { fontWeight: 600 } : null),
              ...(n.italic ? { fontStyle: "italic" } : null),
              lineHeight: 1.5,
            }}
          >
            {renderInline(String(n.value || n.text || ""), key)}
          </div>
        );
      }
      case "markdown":
        return <Markdown key={key} text={String(n.value || n.text || "")} />;
      case "code":
        // Same chrome as a chat-prose code fence (language bar + copy) — the
        // bespoke bare <pre> read visibly poorer than the identical fence in
        // prose, and dropped the language label entirely.
        return <CodeBlock key={key} lang={String(n.language || "")} text={String(n.code || "")} />;
      case "icon": {
        const nm = String(n.name || "");
        const glyph = ICON_GLYPHS[nm];
        const sz = Math.min(Math.max(Number(n.size) || 16, 12), 28);
        if (glyph) {
          const tint = TEXT_TINTS.has(String(n.color)) ? ` ${String(n.color)}` : "";
          return (
            <span key={key} className={"dui-icon" + tint}>
              <Icon name={glyph} size={sz} />
            </span>
          );
        }
        // Emoji "icon names" ("⚠️") have no vector — render the emoji itself;
        // unknown plain names render nothing (both native parity). Previously
        // EVERY icon fell through to null, so card section headers lost their
        // glyph on desktop.
        return /\p{Extended_Pictographic}/u.test(nm) ? (
          <span key={key} style={{ fontSize: sz, lineHeight: 1 }}>
            {nm}
          </span>
        ) : null;
      }
      case "quote":
        return (
          <blockquote key={key} className="dui-quote">
            {renderInline(String(n.text || ""), key)}
            {n.source ? <footer className="dui-quote-src">— {String(n.source)}</footer> : null}
          </blockquote>
        );
      case "badge":
        return (
          <span key={key} className={"dui-badge" + (BADGE_TINTS.has(String(n.color)) ? ` ${String(n.color)}` : "")}>
            {String(n.value || n.text || "")}
          </span>
        );
      case "stat": {
        // Signed descriptions ("+2.1%") are direction claims — arrow + tinted chip.
        const desc = n.description ? String(n.description).trim() : "";
        const pos = desc.startsWith("+") || desc.startsWith("▲");
        const neg = desc.startsWith("-") || desc.startsWith("−") || desc.startsWith("▼");
        const trend = pos ? `▲ ${desc.replace(/^[+▲]\s*/, "")}` : neg ? `▼ ${desc.replace(/^[-−▼]\s*/, "")}` : desc;
        // Unit-suffix typography: "381톤"/"68%"/"2.4억" → big number + a smaller
        // unit, so the metric reads as a value not a string. Only a trailing 1–2
        // char non-digit unit splits (a ratio like "7/10" or a bare "12" stays whole).
        const rawVal = String(n.value || "");
        const um = /^(-?[\d,]+(?:\.\d+)?)([^\d\s]{1,2})$/.exec(rawVal.trim());
        return (
          <div key={key} className="dui-stat">
            <div className="dui-stat-value-row">
              <StatValue value={um ? um[1] : rawVal} />
              {um ? <span className="dui-stat-unit">{um[2]}</span> : null}
            </div>
            <div className="dui-stat-label">{String(n.label || "")}</div>
            {desc ? <div className={"dui-stat-desc" + (pos ? " pos" : neg ? " neg" : "")}>{trend}</div> : null}
          </div>
        );
      }
      case "image": {
        const url = String(n.url || "");
        if (/^https?:\/\//i.test(url)) {
          return <img key={key} className="dui-image" src={url} alt={String(n.alt || "")} loading="lazy" />;
        }
        // Non-resolvable (relative/data) URL → a sized alt-text placeholder box
        // instead of a silent hole (native always renders a sized image slot).
        const alt = String(n.alt || "").trim();
        return alt ? (
          <div key={key} className="dui-image-alt" title={alt}>
            {alt}
          </div>
        ) : null;
      }
      case "avatar": {
        const nm = String(n.name || "");
        // Up to two-word initials ("John Doe" → "JD"), and a person icon — not a
        // literal "?" — when there is neither image nor name (native parity).
        const initials = nm
          .trim()
          .split(/\s+/)
          .filter(Boolean)
          .slice(0, 2)
          .map((w) => w[0])
          .join("")
          .toUpperCase();
        return (
          <div key={key} className="dui-avatar" title={nm}>
            {/^https?:\/\//i.test(String(n.imageUrl || "")) ? (
              <img src={String(n.imageUrl)} alt={nm} />
            ) : initials ? (
              initials
            ) : (
              <Icon name="people" size={16} />
            )}
          </div>
        );
      }
      case "table": {
        const headers: string[] = Array.isArray(n.headers) ? n.headers : [];
        const rows: string[][] = Array.isArray(n.rows) ? n.rows : [];
        // Numeric columns read best right-aligned: every non-empty cell
        // starting with a digit ("12", "7/10", "68%") marks the column.
        const colCount = Math.max(headers.length, ...rows.map((r) => (Array.isArray(r) ? r.length : 0)), 0);
        const numeric = Array.from({ length: colCount }, (_, i) => {
          const cells = rows.map((r) => String((Array.isArray(r) ? r : [])[i] ?? "").trim()).filter(Boolean);
          // Test past inline markers so a model-bolded number ("**12**") still
          // classifies its column as numeric (the cell renders bold below).
          return cells.length > 0 && cells.every((c) => /^[-−]?\d/.test(c.replace(/^[*_~`\s]+/, "")));
        });
        const align = (i: number): React.CSSProperties | undefined => (numeric[i] ? { textAlign: "right" } : undefined);
        const numClass = (i: number): string | undefined => (numeric[i] ? "md-num" : undefined);
        return (
          <table key={key} className={"md-table" + (colCount >= 3 ? " md-table-dense" : "")}>
            <thead>
              <tr>
                {headers.map((h, i) => (
                  <th key={i} className={numClass(i)} style={align(i)}>
                    {renderInline(String(h), `${key}.h${i}`)}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {rows.map((r, ri) => (
                <tr key={ri}>
                  {(Array.isArray(r) ? r : []).map((c, ci) => (
                    <td key={ci} className={numClass(ci)} style={align(ci)}>
                      {renderInline(String(c), `${key}.r${ri}.${ci}`)}
                    </td>
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
        const nums = values.map((v) => Number(v) || 0);
        // Scale bars from the real series max (native parity): flooring at 1
        // flattened fractional series like [0.12, 0.18] to near-empty bars,
        // while the phone filled them. `|| 1` only guards an all-zero series.
        const max = Math.max(0, ...nums) || 1;
        // Line variant: the authoring contract teaches type="line" for trends,
        // and the native client draws it — the desktop must not silently
        // degrade trends into bars (2026-07-07 review catch on #3229).
        // Min-padded scale matches the native renderer: points carry their
        // real value labels, so a non-zero baseline stays honest.
        if (String(n.chartType) === "line" && nums.length >= 2) {
          // Scale from the series' real extremes: the shared bar-width floor
          // of 1 flattens fractional series like [0.12, 0.18] (review catch
          // on #3233); the native renderer scales from the actual max.
          const lineMax = Math.max(...nums);
          const min = Math.min(...nums);
          const span = lineMax - min || lineMax || 1;
          // Floor the baseline at 0 for all-positive series, but let it dip
          // below zero when the data actually goes negative so those points
          // stay in-plot (native parity).
          const paddedLo = min - span * 0.15;
          const lo = min < 0 ? paddedLo : Math.max(0, paddedLo);
          const hi = lineMax + span * 0.05;
          const W = 320;
          const H = 92;
          const PAD = 10;
          const plotBottom = H - PAD;
          const xAt = (i: number) => PAD + (i * (W - 2 * PAD)) / (nums.length - 1);
          const yAt = (v: number) => plotBottom - ((v - lo) / (hi - lo)) * (H - 2 * PAD - 12);
          // Smooth cubic through the points (midpoint control handles — stays
          // inside the data envelope) with a soft area wash underneath and a
          // halo on the newest point, mirroring the native canvas chart. The
          // points stay honest: each carries its real value label.
          let d = `M${xAt(0)},${yAt(nums[0])}`;
          for (let i = 1; i < nums.length; i++) {
            const mx = (xAt(i - 1) + xAt(i)) / 2;
            d += ` C${mx},${yAt(nums[i - 1])} ${mx},${yAt(nums[i])} ${xAt(i)},${yAt(nums[i])}`;
          }
          const area = `${d} L${xAt(nums.length - 1)},${plotBottom} L${xAt(0)},${plotBottom} Z`;
          const midY = (yAt(hi) + plotBottom) / 2;
          const summary = nums.map((v, i) => `${labels[i] ?? i + 1} ${v}`).join(", ");
          return (
            <div key={key} className="dui-chart">
              {n.label ? <div className="dui-stat-label">{String(n.label)}</div> : null}
              <svg
                className="dui-line-svg"
                viewBox={`0 0 ${W} ${H}`}
                preserveAspectRatio="none"
                role="img"
                aria-label={`${String(n.label ?? "line chart")}: ${summary}`}
              >
                {/* Baseline + one mid gridline: enough scaffolding to read
                    scale without turning the card into a spreadsheet. */}
                <line className="dui-line-grid" x1={0} y1={plotBottom} x2={W} y2={plotBottom} />
                <line className="dui-line-grid" x1={0} y1={midY} x2={W} y2={midY} />
                <path className="dui-line-area" d={area} />
                {/* pathLength=1 normalizes the dash units so the CSS draw-in
                    (stroke-dashoffset 1→0) works for any path length. */}
                <path className="dui-line-path" d={d} pathLength={1} />
                {nums.map((v, i) => {
                  const last = i === nums.length - 1;
                  return (
                    <g key={i}>
                      {/* The newest point is the reader's answer — halo it. */}
                      {last ? <circle className="dui-line-halo" cx={xAt(i)} cy={yAt(v)} r={7} /> : null}
                      <circle className="dui-line-dot" cx={xAt(i)} cy={yAt(v)} r={last ? 4 : 3} />
                      <text className="dui-line-val" x={xAt(i)} y={yAt(v) - 7} textAnchor="middle">
                        {String(values[i] ?? "")}
                      </text>
                    </g>
                  );
                })}
              </svg>
              {labels.length > 0 ? (
                <div className="dui-line-axis">
                  {labels.map((l, i) => (
                    <span key={i}>{String(l)}</span>
                  ))}
                </div>
              ) : null}
            </div>
          );
        }
        // Vertical bar (column) chart — SVG parity with the native canvas:
        // columns from a zero baseline, value labels above each bar, x-axis
        // labels below. The old horizontal CSS bars read structurally different
        // from the phone's vertical columns for the same card.
        const W = 320;
        const H = 92;
        const PAD = 10;
        const plotBottom = H - PAD;
        const plotTop = PAD + 12; // headroom for the value labels above the bars
        const plotH = plotBottom - plotTop;
        const count = nums.length;
        const gap = count > 0 ? Math.max(4, (W - 2 * PAD) * 0.04) : 0;
        const barW = count > 0 ? Math.max(1, (W - 2 * PAD - gap * (count + 1)) / count) : 0;
        const barX = (i: number) => PAD + gap + i * (barW + gap);
        // Zero is a real claim → no bar (the value label still says 0); positive
        // values get a 2px legibility floor. Scaled from the real series max.
        const barH = (v: number) => (v > 0 ? Math.max(2, (v / max) * plotH) : 0);
        const summary = nums.map((v, i) => `${labels[i] ?? i + 1} ${v}`).join(", ");
        const barGradId = `dui-barg-${key}`;
        const midY = (plotTop + plotBottom) / 2;
        return (
          <div key={key} className="dui-chart">
            {n.label ? <div className="dui-stat-label">{String(n.label)}</div> : null}
            <svg
              className="dui-bar-svg"
              viewBox={`0 0 ${W} ${H}`}
              preserveAspectRatio="none"
              role="img"
              aria-label={`${String(n.label ?? "bar chart")}: ${summary}`}
            >
              <defs>
                {/* Vertical accent gradient (opaque top → faint base) gives the
                    columns depth instead of a flat fill. CSS-var stops stay
                    theme-aware; unique id per chart avoids cross-SVG collision. */}
                <linearGradient id={barGradId} x1="0" y1="0" x2="0" y2="1">
                  <stop className="dui-bar-grad-top" offset="0" />
                  <stop className="dui-bar-grad-bot" offset="1" />
                </linearGradient>
              </defs>
              <line className="dui-line-grid" x1={0} y1={plotBottom} x2={W} y2={plotBottom} />
              <line className="dui-line-grid dui-grid-mid" x1={0} y1={midY} x2={W} y2={midY} strokeDasharray="2 4" />
              {nums.map((v, i) => {
                const h = barH(v);
                const cx = barX(i) + barW / 2;
                return (
                  <g key={i}>
                    {h > 0 ? (
                      <rect
                        className="dui-bar-rect"
                        x={barX(i)}
                        y={plotBottom - h}
                        width={barW}
                        height={h}
                        rx={3}
                        fill={`url(#${barGradId})`}
                      />
                    ) : null}
                    <text className="dui-bar-svg-val" x={cx} y={plotBottom - h - 3} textAnchor="middle">
                      {String(values[i] ?? "")}
                    </text>
                  </g>
                );
              })}
            </svg>
            {labels.length > 0 ? (
              <div className="dui-line-axis">
                {labels.map((l, i) => (
                  <span key={i}>{String(l)}</span>
                ))}
              </div>
            ) : null}
          </div>
        );
      }
      // --- feedback ---
      case "alert": {
        // Whitelist the class the same way BADGE_TINTS gates badges; the
        // severity glyph (✓/!/✕/i in a currentColor disc) mirrors the native
        // AlertIcon so alerts scan by shape, not color alone.
        const sev = ALERT_SEVERITIES.has(String(n.severity)) ? String(n.severity) : "info";
        const glyph = sev === "success" ? "✓" : sev === "warning" ? "!" : sev === "error" ? "✕" : "i";
        return (
          <div key={key} className={`dui-alert ${sev}`}>
            <span className="dui-alert-glyph" aria-hidden="true">
              {glyph}
            </span>
            <div className="dui-alert-body">
              {n.title ? <div className="dui-alert-title">{renderInline(String(n.title), `${key}.t`)}</div> : null}
              {/* Alert bodies are prose — models emphasize inline ("**중요**: …"). */}
              <div>{renderInline(String(n.message || ""), key)}</div>
            </div>
          </div>
        );
      }
      case "progress": {
        const v = typeof n.value === "number" ? Math.max(0, Math.min(1, n.value)) : null;
        return (
          <div key={key} className="dui-col">
            {n.label || v != null ? (
              <div className="dui-progress-hd">
                <span className="dui-stat-label">{n.label ? String(n.label) : ""}</span>
                {v != null && !String(n.label ?? "").includes(`${Math.round(v * 100)}%`) ? (
                  <span className="dui-progress-pct">{Math.round(v * 100)}%</span>
                ) : null}
              </div>
            ) : null}
            <span className="dui-progress">
              {/* value-less = indeterminate: an animated sliding bar (native uses
                  Material's indeterminate indicator). The old static 40%/opacity
                  fill read as a stuck/broken bar. */}
              <span
                className={"dui-progress-fill" + (v == null ? " indeterminate" : "")}
                style={v == null ? undefined : { width: `${v * 100}%` }}
              />
            </span>
          </div>
        );
      }
      case "countdown":
        return <Countdown key={key} seconds={Number(n.seconds) || 0} label={String(n.label || "")} />;
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
          <label key={key} className={fieldClass(id)}>
            {n.label ? <span className="dui-label">{String(n.label)}</span> : null}
            {n.multiline ? (
              <textarea {...common} rows={3} style={{ resize: "vertical" }} />
            ) : (
              <input {...common} type={n.keyboard === "number" || n.keyboard === "decimal" ? "number" : "text"} />
            )}
            {reqHint(id)}
          </label>
        );
      }
      case "date_input":
      case "time_input":
        return (
          <label key={key} className={fieldClass(id)}>
            {n.label ? <span className="dui-label">{String(n.label)}</span> : null}
            <input
              className="field"
              type={n.type === "date_input" ? "date" : "time"}
              value={String(form[id] ?? "")}
              disabled={busy}
              onChange={(e) => setField(id, e.target.value)}
            />
            {reqHint(id)}
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
          <label key={key} className={fieldClass(id)}>
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
            {reqHint(id)}
          </label>
        );
      case "radio_group":
        return (
          <div key={key} className={fieldClass(id)} role="radiogroup">
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
            {reqHint(id)}
          </div>
        );
      case "slider": {
        // Normalize an inverted range (min>max) — the native coerces it, but a raw
        // min>max yields a broken <input type="range"> on desktop.
        const a = Number(n.min ?? 0);
        const b = Number(n.max ?? 100);
        const min = Math.min(a, b);
        const max = Math.max(a, b);
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
          <div key={key} className={"dui-chips" + (invalid.has(id) ? " invalid" : "")}>
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
            {reqHint(id)}
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
  const cls = "assistant-text" + (segments.length > 1 ? " mixed" : "");
  return (
    <div className={cls}>
      {segments.map((seg, i) => {
        if (seg.kind === "md")
          return (
            <div key={i} className="assistant-segment assistant-segment-md">
              <Markdown text={seg.text} onChoice={busy ? undefined : onUiSubmit} />
            </div>
          );
        if (seg.kind === "ui-pending") {
          // HTML v2 partials parse cleanly (EOF auto-close): paint display-only
          // cards live as they stream. Interactive trees keep the placeholder —
          // a half-built form must not accept clicks mid-stream.
          const partial = seg.body.trimStart().startsWith("<") ? parseDenebUi(seg.body) : null;
          if (partial && !hasInteractiveNode(partial))
            return (
              <div key={i} className="assistant-segment assistant-segment-ui">
                <DenebUi spec={partial} onSubmit={onUiSubmit} busy />
              </div>
            );
          return (
            <div key={i} className="assistant-segment assistant-segment-pending">
              <div className="dui-pending">UI 생성 중…</div>
            </div>
          );
        }
        const spec = parseDenebUi(seg.body);
        return spec ? (
          <div key={i} className="assistant-segment assistant-segment-ui">
            <DenebUi spec={spec} onSubmit={onUiSubmit} busy={busy} />
          </div>
        ) : (
          <div key={i} className="assistant-segment assistant-segment-code">
            <pre className="dui-code">
              <code>{seg.body}</code>
            </pre>
          </div>
        );
      })}
    </div>
  );
}
