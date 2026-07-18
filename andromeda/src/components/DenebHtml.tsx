import { useEffect, useMemo, useRef, useState } from "react";

// deneb-html — a webpage-style HTML answer the agent authors as a complete
// self-contained document (```deneb-html fence). It renders sandboxed INLINE
// in the transcript: an iframe with sandbox="allow-scripts" (unique origin, no
// same-origin power) plus an injected CSP that blocks every network fetch, so
// inline CSS/JS run but nothing loads from or leaks to the outside.
//
// Two-way bridge (the page side is injected below, contract in the gateway
// system prompt): window.deneb.send(text) posts the text back into the chat
// as a user message; the page also reports its scrollHeight so the frame
// grows to fit instead of double-scrolling.

/** Base stylesheet + micro design system every page gets for free: variable-
 * driven so a single body class re-skins the whole page (theme-dark /
 * theme-warm / theme-mono; default = clean light), plus utility classes
 * (card, grid, stat, badge, bar, button.primary) the authoring contract
 * teaches — diverse looks, consistent quality, near-zero model CSS. Injected
 * FIRST so the page's own styles override it naturally. Keep in sync with the
 * native PRELUDE (DenebHtmlView.android.kt) and docs/research/deneb-html.md. */
const BASE_CSS =
  ":root{color-scheme:light}" +
  "body{--bg:#fff;--ink:#1f2128;--muted:#6f747e;--line:#e5e6ea;--card:#f7f7f9;" +
  "--accent:#3b6ea5;--ok:#2e7d32;--warn:#b26a00;--bad:#c62828;" +
  "margin:14px;font-family:'Pretendard','Noto Sans KR',system-ui,-apple-system,sans-serif;" +
  "font-size:14px;line-height:1.6;color:var(--ink);background:var(--bg)}" +
  "body.theme-dark{color-scheme:dark;--bg:#111318;--ink:#e8eaf0;--muted:#9aa1ad;" +
  "--line:#2a2e37;--card:#1b1e26;--accent:#7fa8d0}" +
  "body.theme-warm{--card:#faf5f0;--line:#eadfd5;--accent:#c17a5b}" +
  "body.theme-mono{--accent:#1f2128}" +
  "h1,h2,h3,h4{line-height:1.3;margin:0.7em 0 0.35em}" +
  "h1{font-size:22px}h2{font-size:18px}h3{font-size:15px}" +
  "p{margin:0.4em 0}" +
  "table{border-collapse:collapse;width:100%}" +
  "th,td{padding:6px 10px;border:1px solid var(--line);text-align:left}" +
  "th{background:var(--card)}" +
  "button{font:inherit;cursor:pointer;border:1px solid var(--line);border-radius:8px;" +
  "padding:6px 12px;background:var(--card);color:var(--ink)}" +
  "button.primary{background:var(--accent);border-color:var(--accent);color:#fff}" +
  ".card{background:var(--card);border:1px solid var(--line);border-radius:12px;padding:12px 14px;margin:8px 0}" +
  ".grid{display:grid;gap:10px;grid-template-columns:repeat(auto-fit,minmax(140px,1fr))}" +
  ".stat-value{font-size:24px;font-weight:600;line-height:1.2}" +
  ".stat-label{font-size:12px;color:var(--muted)}" +
  ".badge{display:inline-block;padding:2px 8px;border-radius:999px;font-size:12px;background:var(--card)}" +
  ".badge.ok{background:#e6f4ea;color:var(--ok)}" +
  ".badge.warn{background:#fdf3e3;color:var(--warn)}" +
  ".badge.bad{background:#fdeaea;color:var(--bad)}" +
  ".bar{height:8px;border-radius:4px;background:var(--line);overflow:hidden}" +
  ".bar>i{display:block;height:100%;background:var(--accent)}" +
  ".muted{color:var(--muted)}.accent{color:var(--accent)}";

/** CSP + base style + deneb bridge, prepended to the document. The CSP meta
 * blocks network subresources; the bridge exposes deneb.send and streams
 * content height. */
const PRELUDE =
  "<meta http-equiv=\"Content-Security-Policy\" content=\"default-src 'none'; style-src 'unsafe-inline'; script-src 'unsafe-inline'; img-src data:;\">" +
  "<style>" +
  BASE_CSS +
  "</style>" +
  "<script>(function(){" +
  'window.deneb={send:function(t){parent.postMessage({__deneb:"prompt",text:String(t)},"*")}};' +
  'var r=function(){parent.postMessage({__deneb:"height",h:document.documentElement.scrollHeight},"*")};' +
  'window.addEventListener("load",r);' +
  'if(typeof ResizeObserver==="function"){new ResizeObserver(r).observe(document.documentElement)}' +
  "})()</script>";

/** Builds the sandboxed srcdoc for a deneb-html body. Exported for tests. */
export function buildSrcdoc(body: string): string {
  return PRELUDE + body;
}

export type DenebHtmlMessage = { type: "prompt"; text: string } | { type: "height"; h: number } | null;

/** Classifies a window message from the sandboxed page. Exported for tests. */
export function parseDenebHtmlMessage(data: unknown): DenebHtmlMessage {
  if (!data || typeof data !== "object") return null;
  const d = data as { __deneb?: unknown; text?: unknown; h?: unknown };
  if (d.__deneb === "prompt" && typeof d.text === "string" && d.text.trim()) {
    return { type: "prompt", text: d.text.trim() };
  }
  if (d.__deneb === "height" && typeof d.h === "number" && Number.isFinite(d.h)) {
    return { type: "height", h: d.h };
  }
  return null;
}

const MIN_HEIGHT = 160;
const MAX_HEIGHT = 900;

export function DenebHtmlAnswer({
  body,
  onSubmit,
  busy,
  // false on non-last assistant turns: the page still renders and its local
  // scripts run, but deneb.send round-trips are ignored (stale-card gating,
  // same rule as deneb-ui callbacks).
  interactive = true,
}: {
  body: string;
  onSubmit: (msg: string) => void;
  busy?: boolean;
  interactive?: boolean;
}) {
  const frameRef = useRef<HTMLIFrameElement>(null);
  const [height, setHeight] = useState(MIN_HEIGHT);
  const srcdoc = useMemo(() => buildSrcdoc(body), [body]);

  // Live props for the stable message listener (busy flips per stream tick).
  // Updated post-commit, never during render (react-hooks ref rule).
  const live = useRef({ onSubmit, busy, interactive });
  useEffect(() => {
    live.current = { onSubmit, busy, interactive };
  });

  useEffect(() => {
    function onMessage(e: MessageEvent) {
      if (!frameRef.current || e.source !== frameRef.current.contentWindow) return;
      const msg = parseDenebHtmlMessage(e.data);
      if (!msg) return;
      if (msg.type === "height") {
        setHeight(Math.min(MAX_HEIGHT, Math.max(MIN_HEIGHT, Math.ceil(msg.h) + 8)));
        return;
      }
      const { onSubmit: submit, busy: b, interactive: it } = live.current;
      if (!b && it) submit(msg.text);
    }
    window.addEventListener("message", onMessage);
    return () => window.removeEventListener("message", onMessage);
  }, []);

  return (
    <iframe
      ref={frameRef}
      className="deneb-html-frame"
      sandbox="allow-scripts"
      srcDoc={srcdoc}
      style={{ height }}
      title="웹 응답"
    />
  );
}
