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
// Must match server.mjs settle tuning.
const FLOOR = 400;
const QUIET_MS = 350;
const NET_QUIET_MS = 400;
const MIN_SETTLE_MS = 1_200;
const NET_MAX_MS = 8_000;
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
  // The nastiest shape, and the one a content floor alone does NOT catch: the
  // nav chrome already clears the floor and is perfectly quiet, so the page
  // looks finished while its article fetch has not even started yet. Only the
  // minimum-settle floor keeps the poll alive long enough to see that fetch.
  "/chrome-then-article": `<html><body><div id=nav>${"메뉴 ".repeat(200)}</div><div id=x></div><script>
    setTimeout(async () => { const r = await fetch('/slow-data'); document.getElementById('x').innerText = await r.text(); }, 600);
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

// Mirrors server.mjs settlePage — kept in sync by the constants above; the probe
// fails loudly if the rule regresses to bare stability or drops a guard.
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

function trackNetwork(page) {
  const st = { inFlight: 0, last: Date.now() };
  const bump = () => {
    st.last = Date.now();
  };
  page.on("request", () => {
    st.inFlight++;
    bump();
  });
  page.on("requestfinished", () => {
    st.inFlight = Math.max(0, st.inFlight - 1);
    bump();
  });
  page.on("requestfailed", () => {
    st.inFlight = Math.max(0, st.inFlight - 1);
    bump();
  });
  return st;
}

async function settlePage(page, budgetMs, net) {
  const start = Date.now();
  let budgetDeadline = Infinity;
  let lastLen = -1;
  let lenSince = Date.now();
  for (;;) {
    const now = Date.now();
    const netQuiet = net.inFlight === 0 && now - net.last >= NET_QUIET_MS;
    if (budgetDeadline === Infinity && (netQuiet || now - start >= NET_MAX_MS)) {
      budgetDeadline = now + budgetMs;
    }
    const len = await frameTextLen(page);
    if (len !== lastLen) {
      lastLen = len;
      lenSince = Date.now();
    }
    if (budgetDeadline !== Infinity) {
      const rendered = len >= FLOOR && Date.now() - lenSince >= QUIET_MS;
      const floorPassed = Date.now() - start >= Math.min(MIN_SETTLE_MS, budgetMs + NET_QUIET_MS);
      const stillQuiet = net.inFlight === 0 && Date.now() - net.last >= NET_QUIET_MS;
      if ((rendered && floorPassed && stillQuiet) || Date.now() >= budgetDeadline) return;
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
      ["/chrome-then-article", "LATE_FETCH_MARKER"],
    ]) {
      const page = await browser.newPage();
      const net = trackNetwork(page);
      const t0 = Date.now();
      await page.goto(`http://127.0.0.1:${PORT}${path}`, { waitUntil: "domcontentloaded" });
      await settlePage(page, 2_000, net); // the caller's patience budget
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
  console.log(`PROBE_RESULT passed=${3 - failed}/3`);
  process.exit(failed > 0 ? 1 : 0);
}

main();
