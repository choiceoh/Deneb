// Deneb browser sidecar — a RESIDENT headful Chromium the agent reads pages
// through. Playwright owns the browser via a persistent profile
// (~/.deneb/browser-profile), launched headful on an Xvfb display so a human
// can attach over noVNC, log into sites (groupware, portals, cafes) and hand
// the sessions to the agent — the exact browser the agent drives is the one
// the human sees. Read-only v1: navigate + settle + extract readable text.
//
// Loopback-only HTTP API (the gateway lives on the same host):
//   GET  /health           → {ok, profile, pages}
//   POST /browse {url, waitMs?} → {ok, url, title, text} | {ok:false, error}
//
// Requests are serialized (one resident browser); each browse opens its own
// tab and closes it, so the noVNC view keeps only the human's tabs.
import http from "node:http";
import { chromium } from "playwright";

const PORT = Number(process.env.DENEB_BROWSER_PORT || 18930);
const PROFILE =
  process.env.DENEB_BROWSER_PROFILE || `${process.env.HOME}/.deneb/browser-profile`;
const MAX_CHARS = 16_000;
const NAV_TIMEOUT_MS = 25_000;

let ctx = null;

async function ensureContext() {
  if (ctx) {
    try {
      ctx.pages(); // throws once the context is closed
      return ctx;
    } catch {
      ctx = null;
    }
  }
  ctx = await chromium.launchPersistentContext(PROFILE, {
    headless: false,
    viewport: { width: 1280, height: 900 },
    args: ["--disable-blink-features=AutomationControlled"],
  });
  ctx.on("close", () => {
    ctx = null;
  });
  return ctx;
}

// Serialize agent requests: one resident browser, no interleaved navigations.
let chain = Promise.resolve();
function enqueue(fn) {
  const run = chain.then(fn, fn);
  chain = run.then(
    () => {},
    () => {},
  );
  return run;
}

async function browse(url, waitMs, selector) {
  if (!/^https?:\/\//i.test(url)) {
    return { ok: false, error: "url must be http(s)" };
  }
  const context = await ensureContext();
  const page = await context.newPage();
  try {
    await page.goto(url, { waitUntil: "domcontentloaded", timeout: NAV_TIMEOUT_MS });
    // SPA settle: bounded network-idle, then a short paint delay.
    await page.waitForLoadState("networkidle", { timeout: 8_000 }).catch(() => {});
    await page.waitForTimeout(Math.min(Math.max(waitMs || 0, 500), 5_000));
    const title = await page.title();
    // Targeted extraction: with a CSS selector only the matched nodes' text
    // comes back, so a page-sized read shrinks to the part actually needed
    // (a pricing table, a spec sheet section). Zero matches reports honestly
    // as matched:0 with empty text — NO silent whole-page fallback: a caller
    // that asked for "table" and got the full page would believe the page has
    // no table-shaped noise, and mis-scoped reads are how context rots.
    let text;
    let matched;
    if (typeof selector === "string" && selector.trim() !== "") {
      const sel = selector.trim();
      try {
        const parts = await page.$$eval(
          sel,
          (nodes) => nodes.map((n) => n.innerText || "").filter(Boolean),
        );
        matched = parts.length;
        text = parts.join("\n\n---\n\n");
      } catch (err) {
        return { ok: false, error: `invalid selector: ${String(err && err.message ? err.message : err).slice(0, 200)}` };
      }
    } else {
      text = await page.evaluate(
        () => (document.body && document.body.innerText) || "",
      );
    }
    const out = {
      ok: true,
      url: page.url(),
      title,
      text: text.slice(0, MAX_CHARS),
      truncated: text.length > MAX_CHARS,
    };
    if (matched !== undefined) out.matched = matched;
    return out;
  } finally {
    await page.close().catch(() => {});
  }
}

function readBody(req) {
  return new Promise((resolve, reject) => {
    let data = "";
    req.on("data", (c) => {
      data += c;
      if (data.length > 64 * 1024) reject(new Error("body too large"));
    });
    req.on("end", () => resolve(data));
    req.on("error", reject);
  });
}

const server = http.createServer(async (req, res) => {
  res.setHeader("Content-Type", "application/json; charset=utf-8");
  try {
    if (req.method === "GET" && req.url === "/health") {
      let pages = 0;
      try {
        pages = ctx ? ctx.pages().length : 0;
      } catch {
        /* context closed */
      }
      res.end(JSON.stringify({ ok: true, profile: PROFILE, pages }));
      return;
    }
    if (req.method === "POST" && req.url === "/browse") {
      const { url, waitMs, selector } = JSON.parse((await readBody(req)) || "{}");
      const out = await enqueue(() => browse(String(url || ""), Number(waitMs), selector));
      if (!out.ok) res.statusCode = 422;
      res.end(JSON.stringify(out));
      return;
    }
    res.statusCode = 404;
    res.end(JSON.stringify({ ok: false, error: "not found" }));
  } catch (e) {
    res.statusCode = 500;
    // Do not echo exception strings — Error#stack / engine-specific String(err)
    // can leak paths into the HTTP response.
    console.error("browse handler error:", e?.message || e);
    res.end(JSON.stringify({ ok: false, error: "internal error" }));
  }
});

server.listen(PORT, "127.0.0.1", () => {
  console.log(`deneb-browser-sidecar listening on 127.0.0.1:${PORT} (profile: ${PROFILE})`);
});
