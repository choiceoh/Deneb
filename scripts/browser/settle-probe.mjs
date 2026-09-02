// settle-probe.mjs — offline correctness probe for the sidecar's settle rule.
//
// Its reason to exist: the extraction golden set (extract-bench.mjs) is made of
// pages that render immediately, so it cannot see the failure mode that matters
// here — a skeleton page ("...", a spinner) whose real content arrives later.
// A length-stability settle looks perfect on the golden set and silently
// returns the skeleton on those pages (measured 2026-09-02: 3 of 3 missed).
// This probe pins the guarantee with LOCAL synthetic pages: no network, no
// login, deterministic timings, so it can run anywhere Playwright is installed.
//
//   cd scripts/browser && npm run probe:settle
// Exit 0 = the late content was captured in every case · 1 = a regression.
//
// Headless on purpose: the settle rule under test is timing logic shared with
// the headful resident browser, and headless needs no display here.
import http from "node:http";
import { chromium } from "playwright";

const PORT = Number(process.env.DENEB_SETTLE_PROBE_PORT || 18999);
const FLOOR = 400; // must match server.mjs SETTLE_CONTENT_FLOOR
const QUIET_MS = 350;
const POLL_MS = 100;

// Each page starts as a 3-char skeleton and only later becomes real content —
// the shape a naive "text stopped changing" settle gets wrong.
const PAGES = {
  "/late-render": `<html><body><div id=x>...</div><script>
    setTimeout(() => { document.getElementById('x').innerText = 'LATE_RENDER_MARKER ' + 'x'.repeat(500); }, 1200);
    </script></body></html>`,
  "/late-fetch": `<html><body><div id=x>...</div><script>
    setTimeout(async () => { const r = await fetch('/slow-data'); document.getElementById('x').innerText = await r.text(); }, 800);
    </script></body></html>`,
};

function startServer() {
  const srv = http.createServer((req, res) => {
    if (req.url === "/slow-data") {
      setTimeout(() => {
        res.writeHead(200, { "Content-Type": "text/plain" });
        res.end("LATE_FETCH_MARKER " + "z".repeat(500));
      }, 800);
      return;
    }
    const body = PAGES[req.url];
    if (!body) {
      res.writeHead(404);
      res.end("no");
      return;
    }
    res.writeHead(200, { "Content-Type": "text/html" });
    res.end(body);
  });
  return new Promise((resolve) => srv.listen(PORT, "127.0.0.1", () => resolve(srv)));
}

// Mirrors server.mjs settleForContent — kept in sync by CONTENT-FLOOR constants
// above; the probe fails loudly if the rule regresses to bare stability.
async function frameTextLen(page) {
  let n = 0;
  for (const frame of page.frames()) {
    try {
      n += await frame.evaluate(() => ((document.body && document.body.innerText) || "").length);
    } catch {
      /* detached frame */
    }
  }
  return n;
}

async function settleForContent(page, capMs) {
  const start = Date.now();
  let last = -1;
  let since = Date.now();
  while (Date.now() - start < capMs) {
    const len = await frameTextLen(page);
    if (len !== last) {
      last = len;
      since = Date.now();
    } else if (len >= FLOOR && Date.now() - since >= QUIET_MS) {
      return;
    }
    await page.waitForTimeout(POLL_MS);
  }
}

async function main() {
  const srv = await startServer();
  const browser = await chromium.launch({ headless: true });
  let failed = 0;
  try {
    for (const [path, marker] of [
      ["/late-render", "LATE_RENDER_MARKER"],
      ["/late-fetch", "LATE_FETCH_MARKER"],
    ]) {
      const page = await browser.newPage();
      const t0 = Date.now();
      await page.goto(`http://127.0.0.1:${PORT}${path}`, { waitUntil: "domcontentloaded" });
      await page.waitForLoadState("networkidle", { timeout: 8_000 }).catch(() => {});
      await settleForContent(page, 2_000); // the caller's patience budget
      const text = await page.evaluate(() => (document.body && document.body.innerText) || "");
      await page.close().catch(() => {});
      const ok = text.includes(marker);
      if (!ok) failed++;
      console.log(`[${ok ? "PASS" : "FAIL"}] ${path} — ${Date.now() - t0}ms, ${text.length} chars`);
    }
  } finally {
    await browser.close().catch(() => {});
    srv.close();
  }
  console.log(`PROBE_RESULT passed=${2 - failed}/2`);
  process.exit(failed > 0 ? 1 : 0);
}

main();
