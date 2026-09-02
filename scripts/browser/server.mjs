// Deneb browser sidecar — a RESIDENT headful Chromium the agent reads pages
// through. Playwright owns the browser via a persistent profile
// (~/.deneb/browser-profile), launched headful on an Xvfb display so a human
// can attach over noVNC, log into sites (groupware, portals, cafes) and hand
// the sessions to the agent — the exact browser the agent drives is the one
// the human sees. Read-only v1: navigate + settle + extract readable text.
//
// Two contexts, chosen per request by the `isolated` flag:
//   - operator (default): the persistent profile above. The `browse` TOOL uses
//     this — operator-driven reads that WANT the logged-in sessions.
//   - isolated (isolated:true): an ephemeral context off a separate browser
//     with NO profile and a private-range egress block. The `web` tool's
//     automatic stealth/escalation renders use this: they feed the model
//     UNTRUSTED public URLs, so they must not run inside the operator's
//     authenticated cookies (CSRF against logged-in origins) nor be able to
//     reach loopback/RFC1918 services (SSRF). The top-level target is already
//     ValidatePublicTarget-checked on the Go side; this guards the rendered
//     page's own subresource/JS egress, which that check never saw.
//
// Loopback-only HTTP API (the gateway lives on the same host):
//   GET  /health                          → {ok, profile, pages}
//   POST /browse {url, waitMs?, selector?, isolated?, screenshot?, fullPage?}
//        → {ok, url, title, text, matched?, screenshot?} | {ok:false, error}
//
// Requests are serialized (one resident browser) and each carries a hard
// deadline so a hung navigation/launch can't wedge the queue for every later
// request; each browse opens its own tab and closes it, so the noVNC view
// keeps only the human's tabs.
import http from "node:http";
import net from "node:net";
import dns from "node:dns/promises";
import { chromium } from "playwright";

const PORT = Number(process.env.DENEB_BROWSER_PORT || 18930);
const PROFILE =
  process.env.DENEB_BROWSER_PROFILE || `${process.env.HOME}/.deneb/browser-profile`;
const MAX_CHARS = 16_000;
const NAV_TIMEOUT_MS = 25_000;
// Settle tuning (see settlePage). A page that already rendered real text, went
// quiet, and stopped talking to the network is done — the caller's wait_ms is a
// patience BUDGET, not a mandatory sleep. Everything else keeps the whole budget.
const SETTLE_CONTENT_FLOOR = 400; // chars of readable text that count as "rendered"
const SETTLE_QUIET_MS = 350; // text length unchanged this long = quiet
const SETTLE_NET_QUIET_MS = 400; // no request started/finished this long = network quiet
// Floor before ANY early exit. Insurance against work scheduled just after load:
// a page whose nav chrome alone clears the content floor can look "rendered and
// quiet" in ~400ms while its article fetch is still 200ms from starting.
// Measured 2026-09-03 on a synthetic nav+delayed-fetch page: without this floor
// the article was lost (the sidecar returned the menu bar); with it, the fetch
// starts inside the window, its in-flight request blocks the exit, and the
// article is captured. 1.2s also sits under the old code's own minimum settle
// (networkidle's 500ms quiet + a 500ms floor budget).
const SETTLE_MIN_MS = 1_200;
const SETTLE_NET_MAX_MS = 8_000; // give up waiting for network quiet (old networkidle timeout)
const SETTLE_POLL_MS = 100;
// Hard ceiling for one queued operation. page.goto is already bounded, but
// launchPersistentContext / newPage are not, and the queue is serial — so an
// unbounded op wedges every request behind it (the process stays alive, so
// systemd never restarts it). This turns "permanent wedge" into "at most one
// OP_DEADLINE_MS stall, then the queue drains".
const OP_DEADLINE_MS = Number(process.env.DENEB_BROWSER_OP_DEADLINE_MS || 90_000);
// A viewport/UA for the isolated (profile-less) context so untrusted renders
// still look like a normal browser.
const ISO_VIEWPORT = { width: 1280, height: 900 };
const ISO_UA =
  "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36";

// ---------------------------------------------------------------------------
// Private-range egress guard (isolated context only)
// ---------------------------------------------------------------------------

function ipv4IsPrivate(ip) {
  const p = ip.split(".").map(Number);
  if (p.length !== 4 || p.some((n) => Number.isNaN(n) || n < 0 || n > 255)) return false;
  const [a, b] = p;
  if (a === 10) return true; // 10.0.0.0/8
  if (a === 127) return true; // loopback
  if (a === 0) return true; // 0.0.0.0/8
  if (a === 169 && b === 254) return true; // link-local
  if (a === 172 && b >= 16 && b <= 31) return true; // 172.16.0.0/12
  if (a === 192 && b === 168) return true; // 192.168.0.0/16
  if (a === 100 && b >= 64 && b <= 127) return true; // CGNAT 100.64.0.0/10
  return false;
}

function addrIsPrivate(addr) {
  const fam = net.isIP(addr);
  if (fam === 4) return ipv4IsPrivate(addr);
  if (fam === 6) {
    const lower = addr.toLowerCase();
    if (lower === "::1" || lower === "::") return true; // loopback / unspecified
    // IPv4-mapped (::ffff:a.b.c.d) — judge on the embedded v4.
    const mapped = lower.match(/^::ffff:(\d+\.\d+\.\d+\.\d+)$/);
    if (mapped) return ipv4IsPrivate(mapped[1]);
    if (lower.startsWith("fe80")) return true; // link-local
    const head = parseInt(lower.split(":")[0] || "0", 16);
    if ((head & 0xfe00) === 0xfc00) return true; // ULA fc00::/7
    return false;
  }
  return false;
}

// hostIsPrivate resolves a request host to a verdict. IP literals judge
// directly; hostnames resolve through DNS (so a public name pointing at a
// private address — DNS-rebinding style — is still caught). Resolve failure is
// fail-open: a broken lookup must not block an otherwise-legitimate render.
async function hostIsPrivate(hostname) {
  if (!hostname) return false;
  const lower = hostname.toLowerCase();
  if (lower === "localhost" || lower.endsWith(".localhost")) return true;
  if (net.isIP(hostname)) return addrIsPrivate(hostname);
  try {
    const addrs = await dns.lookup(hostname, { all: true });
    return addrs.some((a) => addrIsPrivate(a.address));
  } catch {
    return false;
  }
}

// ---------------------------------------------------------------------------
// Resident resources: operator persistent context + isolated ephemeral browser
// ---------------------------------------------------------------------------

let opCtx = null; // persistent operator context (browse tool)
let isoBrowser = null; // profile-less browser for untrusted isolated renders

async function ensureOperatorContext() {
  if (opCtx) {
    try {
      opCtx.pages(); // throws once the context is closed
      return opCtx;
    } catch {
      opCtx = null;
    }
  }
  opCtx = await chromium.launchPersistentContext(PROFILE, {
    headless: false,
    viewport: { width: 1280, height: 900 },
    args: ["--disable-blink-features=AutomationControlled"],
  });
  opCtx.on("close", () => {
    opCtx = null;
  });
  return opCtx;
}

async function ensureIsolatedBrowser() {
  if (isoBrowser && isoBrowser.isConnected()) return isoBrowser;
  isoBrowser = await chromium.launch({
    headless: false,
    args: ["--disable-blink-features=AutomationControlled"],
  });
  isoBrowser.on("disconnected", () => {
    isoBrowser = null;
  });
  return isoBrowser;
}

// newIsolatedContext returns a fresh ephemeral context with no operator cookies
// and a private-range egress block installed on every request it makes.
async function newIsolatedContext() {
  const browser = await ensureIsolatedBrowser();
  const context = await browser.newContext({
    viewport: ISO_VIEWPORT,
    userAgent: ISO_UA,
  });
  await context.route("**/*", async (route) => {
    try {
      const u = new URL(route.request().url());
      if (u.protocol === "http:" || u.protocol === "https:") {
        if (await hostIsPrivate(u.hostname)) {
          await route.abort("blockedbyclient");
          return;
        }
      }
    } catch {
      /* unparseable URL: fall through to continue */
    }
    await route.continue().catch(() => {});
  });
  return context;
}

// ---------------------------------------------------------------------------
// Extraction (frame-aware)
// ---------------------------------------------------------------------------

// extractAllFrames concatenates readable text across the main frame AND every
// child frame. A page's body.innerText excludes text inside its <iframe>s (they
// are separate documents), so top-frame-only extraction returns near-nothing on
// the many Korean sites that wrap their article in an iframe (Naver blog/cafe).
// Walking page.frames() — which includes cross-origin OOPIFs — recovers it.
// Trivial/ad frames (< 20 chars) and exact-duplicate frames are dropped.
async function extractAllFrames(page) {
  const parts = [];
  const seen = new Set();
  for (const frame of page.frames()) {
    let t;
    try {
      t = await frame.evaluate(() => (document.body && document.body.innerText) || "");
    } catch {
      continue; // detached / not-yet-loaded frame
    }
    const trimmed = (t || "").trim();
    if (trimmed.length < 20) continue;
    if (seen.has(trimmed)) continue;
    seen.add(trimmed);
    parts.push(t);
  }
  return parts.join("\n\n");
}

// extractSelector aggregates matched-node text across all frames. The main
// frame decides selector validity (a syntactically bad selector throws there);
// child-frame throws are ignored so a valid selector that matches only inside
// an iframe still works.
async function extractSelector(page, sel) {
  const all = [];
  let matched = 0;
  let mainErr = null;
  const frames = page.frames();
  for (let i = 0; i < frames.length; i++) {
    try {
      const parts = await frames[i].$$eval(sel, (nodes) =>
        nodes.map((n) => n.innerText || "").filter(Boolean),
      );
      matched += parts.length;
      all.push(...parts);
    } catch (err) {
      if (i === 0) mainErr = err;
    }
  }
  if (mainErr && matched === 0) {
    const msg = String(mainErr && mainErr.message ? mainErr.message : mainErr).slice(0, 200);
    return { invalid: `invalid selector: ${msg}` };
  }
  return { matched, text: all.join("\n\n---\n\n") };
}

// frameTextLen sums readable text length across every frame — the cheap probe
// settlePage polls. It mirrors extractAllFrames' reach (iframe bodies
// included) without building the strings, so a page whose article lives in an
// iframe is judged on the article, not on the empty shell around it.
async function frameTextLen(page) {
  let n = 0;
  for (const frame of page.frames()) {
    try {
      n += await frame.evaluate(
        () => ((document.body && document.body.innerText) || "").length,
      );
    } catch {
      /* detached / navigating frame contributes nothing this tick */
    }
  }
  return n;
}

// trackNetwork counts in-flight requests and stamps the last request event on a
// page. Playwright's waitForLoadState("networkidle") answers the same question
// ONCE and then stops looking, which is why it cannot protect a fetch that
// starts after it fires — this tracker keeps answering for as long as we poll.
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

// settlePage waits until the page looks DONE, then returns — instead of sleeping
// the caller's whole wait_ms. Done means all three at once:
//   1. rendered   — at least SETTLE_CONTENT_FLOOR chars of readable text,
//   2. content quiet — that length unchanged for SETTLE_QUIET_MS,
//   3. network quiet — nothing in flight, nothing started for SETTLE_NET_QUIET_MS,
// and never before SETTLE_MIN_MS has passed.
//
// The budget (wait_ms) is the ceiling, and it starts counting when the network
// FIRST goes quiet — mirroring the old "networkidle, then sleep wait_ms" so a
// page that never renders keeps exactly the patience it used to get. If the
// network never goes quiet, SETTLE_NET_MAX_MS starts the budget anyway (the old
// networkidle timeout).
//
// Every one of these guards was earned by a measurement, not a hunch:
//   - Dropping the content floor: a skeleton page ("..." = 3 chars) is instantly
//     "quiet", so 3 of 3 synthetic late-content pages returned the skeleton.
//   - Dropping network tracking or the floor wait: a page whose nav chrome
//     clears the content floor returns the menu bar while the article fetch is
//     still pending (measured: returned in 435ms, article lost).
// Golden set, median of 3, identical extracted text: 8.4s → 4.3s total.
async function settlePage(page, budgetMs, net) {
  const start = Date.now();
  let budgetDeadline = Infinity;
  let lastLen = -1;
  let lenSince = Date.now();
  for (;;) {
    const now = Date.now();
    const netQuiet = net.inFlight === 0 && now - net.last >= SETTLE_NET_QUIET_MS;
    if (budgetDeadline === Infinity && (netQuiet || now - start >= SETTLE_NET_MAX_MS)) {
      budgetDeadline = now + budgetMs;
    }
    const len = await frameTextLen(page);
    if (len !== lastLen) {
      lastLen = len;
      lenSince = Date.now();
    }
    if (budgetDeadline !== Infinity) {
      const rendered = len >= SETTLE_CONTENT_FLOOR && Date.now() - lenSince >= SETTLE_QUIET_MS;
      const floorPassed = Date.now() - start >= Math.min(SETTLE_MIN_MS, budgetMs + SETTLE_NET_QUIET_MS);
      const stillQuiet = net.inFlight === 0 && Date.now() - net.last >= SETTLE_NET_QUIET_MS;
      if (rendered && floorPassed && stillQuiet) return;
      if (Date.now() >= budgetDeadline) return;
    }
    await page.waitForTimeout(SETTLE_POLL_MS);
  }
}

async function browse(url, waitMs, selector, opts = {}) {
  if (!/^https?:\/\//i.test(url)) {
    return { ok: false, error: "url must be http(s)" };
  }
  const isolated = !!opts.isolated;
  let context;
  let ephemeral = false;
  if (isolated) {
    context = await newIsolatedContext();
    ephemeral = true;
  } else {
    context = await ensureOperatorContext();
  }
  const page = await context.newPage();
  const net = trackNetwork(page);
  try {
    await page.goto(url, { waitUntil: "domcontentloaded", timeout: NAV_TIMEOUT_MS });
    // Settle: rendered + content-quiet + network-quiet, capped by the caller's
    // budget (settlePage). Replaces the old networkidle-then-sleep pair.
    await settlePage(page, Math.min(Math.max(waitMs || 0, 500), 5_000), net);
    const title = await page.title();

    let text;
    let matched;
    if (typeof selector === "string" && selector.trim() !== "") {
      const res = await extractSelector(page, selector.trim());
      if (res.invalid) return { ok: false, error: res.invalid };
      matched = res.matched;
      text = res.text;
    } else {
      text = await extractAllFrames(page);
    }

    const out = {
      ok: true,
      url: page.url(),
      title,
      text: text.slice(0, MAX_CHARS),
      truncated: text.length > MAX_CHARS,
    };
    if (matched !== undefined) out.matched = matched;
    if (opts.screenshot) {
      const buf = await page.screenshot({ fullPage: !!opts.fullPage, type: "png" });
      out.screenshot = buf.toString("base64");
    }
    return out;
  } finally {
    await page.close().catch(() => {});
    if (ephemeral) await context.close().catch(() => {});
  }
}

// ---------------------------------------------------------------------------
// Serial queue with a per-op deadline (head-of-line wedge protection)
// ---------------------------------------------------------------------------

let chain = Promise.resolve();
let consecutiveTimeouts = 0;

function enqueue(fn) {
  const run = chain.then(fn, fn);
  chain = run.then(
    () => {},
    () => {},
  );
  return run;
}

// runWithDeadline races an operation against OP_DEADLINE_MS. A win resets the
// timeout counter; a loss increments it, and 3 consecutive timeouts mean the
// browser is genuinely wedged (not just one slow page) — exit so systemd
// restarts a clean sidecar (Restart=on-failure, RestartSec=5).
function runWithDeadline(fn) {
  return enqueue(() => {
    let timer;
    const deadline = new Promise((_, reject) => {
      timer = setTimeout(
        () => reject(new Error(`operation exceeded ${OP_DEADLINE_MS}ms`)),
        OP_DEADLINE_MS,
      );
    });
    return Promise.race([Promise.resolve().then(fn), deadline]).then(
      (v) => {
        clearTimeout(timer);
        consecutiveTimeouts = 0;
        return v;
      },
      (err) => {
        clearTimeout(timer);
        if (String(err && err.message).includes("operation exceeded")) {
          consecutiveTimeouts += 1;
          console.error(
            `browse op timeout (${consecutiveTimeouts}/3):`,
            err.message,
          );
          if (consecutiveTimeouts >= 3) {
            console.error("sidecar wedged — exiting for systemd restart");
            process.exit(1);
          }
        }
        throw err;
      },
    );
  });
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
        pages = opCtx ? opCtx.pages().length : 0;
      } catch {
        /* context closed */
      }
      res.end(JSON.stringify({ ok: true, profile: PROFILE, pages }));
      return;
    }
    if (req.method === "POST" && req.url === "/browse") {
      const { url, waitMs, selector, isolated, screenshot, fullPage } = JSON.parse(
        (await readBody(req)) || "{}",
      );
      const out = await runWithDeadline(() =>
        browse(String(url || ""), Number(waitMs), selector, {
          isolated: !!isolated,
          screenshot: !!screenshot,
          fullPage: !!fullPage,
        }),
      );
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
