// Pure sandbox plumbing for deneb-html answers (see DenebHtml.tsx for the
// component): the injected CSP/base-style/bridge prelude and the window-message
// classifier. Component-free so react fast-refresh stays clean.

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

/** Smallest frame an inline deneb-html answer gets, in CSS px. */
export const DENEB_HTML_MIN_HEIGHT = 160;

/** Runaway backstop — **not** a content budget. See the Kotlin twin for the story. */
export const DENEB_HTML_MAX_HEIGHT = 8000;

/** Slack over the reported height, so sub-pixel rounding never leaves a scrollbar. */
const HEIGHT_SLACK = 8;

/**
 * Next frame height for a page reporting `reported` CSS px while the frame is
 * `current` px tall. Twin of `denebHtmlFrameHeight` in the native
 * `DenebHtmlFrame.kt`; contract in `docs/research/deneb-html.md`.
 *
 * The frame **grows to fit** — a card must never scroll inside the transcript.
 * That makes every report after the first an echo: once the frame fits, the
 * page's `documentElement.scrollHeight` hands the frame's own height back, so
 * adding slack unconditionally would ratchet the card upward forever. Only a
 * report that EXCEEDS the current frame is news.
 */
export function denebHtmlFrameHeight(current: number, reported: number): number {
  const floor = Math.min(DENEB_HTML_MAX_HEIGHT, Math.max(DENEB_HTML_MIN_HEIGHT, Math.ceil(current)));
  if (!Number.isFinite(reported) || Math.ceil(reported) <= floor) return floor;
  const target = Math.min(Math.ceil(reported), DENEB_HTML_MAX_HEIGHT - HEIGHT_SLACK);
  return Math.max(floor, target + HEIGHT_SLACK);
}
