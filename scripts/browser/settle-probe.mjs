// settle-probe.mjs — offline correctness probe for the sidecar's settle rule.
//
// It imports settlePage/trackNetwork FROM server.mjs and drives them against
// local synthetic pages. Importing the real functions is the point: the first
// version of this probe carried its own copy of the rule, so it would have
// printed 3/3 PASS with every guard deleted from the server (found in review,
// 2026-09-03). server.mjs only calls listen() when run as a program, so the
// import costs nothing.
//
// Its other reason to exist: the extraction golden set (extract-bench.mjs) is
// made of pages that render immediately, so it cannot see the failure mode that
// matters here — a page whose real content arrives after the settle would like
// to return. No network, no login, deterministic timings, headless.
//
//   cd scripts/browser && npm run probe:settle
// Exit 0 = every case captured its late content · 1 = a regression.
import http from "node:http";
import { chromium } from "playwright";
import { settlePage, trackNetwork } from "./server.mjs";

const PORT = Number(process.env.DENEB_SETTLE_PROBE_PORT || 18999);
const LATE = "x".repeat(500);

// Each page withholds its real content until after the moment a naive settle
// would return. The shapes differ in WHY they fool a naive rule.
const PAGES = {
  // Skeleton: "..." is 3 chars — instantly "stable" if there is no content floor.
  "/late-render": `<html><body><div id=x>...</div><script>
    setTimeout(() => { document.getElementById('x').innerText = 'LATE_RENDER_MARKER ' + '${LATE}'; }, 1200);
    </script></body></html>`,
  // Same, but the content comes over the wire — caught only if the network is
  // still being watched when the fetch starts.
  "/late-fetch": `<html><body><div id=x>...</div><script>
    setTimeout(async () => { const r = await fetch('/slow-data'); document.getElementById('x').innerText = await r.text(); }, 800);
    </script></body></html>`,
  // The nav chrome alone clears the content floor and is perfectly quiet, so the
  // page looks finished while its article fetch has not started yet.
  "/chrome-then-article": `<html><body><div id=nav>${"메뉴 ".repeat(200)}</div><div id=x></div><script>
    setTimeout(async () => { const r = await fetch('/slow-data'); document.getElementById('x').innerText = await r.text(); }, 600);
    </script></body></html>`,
  // No network at all: hydration from an inline blob after the minimum settle.
  // Only an explicit wait_ms floor can wait this out.
  "/late-hydrate": `<html><body><div id=nav>${"메뉴 ".repeat(200)}</div><div class="article-body"></div><script>
    setTimeout(() => { document.querySelector('.article-body').innerText = 'LATE_HYDRATE_MARKER ' + '${LATE}'; }, 1500);
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

// Each case says what the caller asked for, because the contract differs:
// an explicit wait_ms is a floor, an omitted one leaves the pace to the sidecar.
const CASES = [
  { path: "/late-render", marker: "LATE_RENDER_MARKER", wait: 2_000, explicit: true },
  { path: "/late-fetch", marker: "LATE_FETCH_MARKER", wait: 2_000, explicit: true },
  { path: "/chrome-then-article", marker: "LATE_FETCH_MARKER", wait: 2_000, explicit: true },
  { path: "/late-hydrate", marker: "LATE_HYDRATE_MARKER", wait: 2_500, explicit: true },
  // Same late-hydrating page read through a SELECTOR: the settle must wait for
  // the requested subtree, not for the page's total text.
  {
    path: "/late-hydrate",
    marker: "LATE_HYDRATE_MARKER",
    wait: 2_500,
    explicit: true,
    selector: ".article-body",
  },
];

async function main() {
  const srv = await startServer();
  const browser = await chromium.launch({ headless: true });
  let failed = 0;
  try {
    for (const kase of CASES) {
      const page = await browser.newPage();
      const net = trackNetwork(page);
      const t0 = Date.now();
      await page.goto(`http://127.0.0.1:${PORT}${kase.path}`, { waitUntil: "domcontentloaded" });
      await settlePage(page, kase.wait, net, {
        selector: kase.selector,
        explicitWait: kase.explicit,
      });
      const text = kase.selector
        ? await page
            .$$eval(kase.selector, (nodes) => nodes.map((n) => n.innerText || "").join("\n"))
            .catch(() => "")
        : await page.evaluate(() => (document.body && document.body.innerText) || "");
      await page.close().catch(() => {});
      const ok = text.includes(kase.marker);
      if (!ok) failed++;
      const label = kase.selector ? `${kase.path} [${kase.selector}]` : kase.path;
      console.log(
        `[${ok ? "PASS" : "FAIL"}] ${label} — ${Date.now() - t0}ms, ${text.length} chars`,
      );
    }
  } finally {
    await browser.close().catch(() => {});
    srv.close();
  }
  console.log(`PROBE_RESULT passed=${CASES.length - failed}/${CASES.length}`);
  process.exit(failed > 0 ? 1 : 0);
}

main();
