// extract-bench.mjs — extraction regression probe for the browser sidecar.
//
// Runs the golden set (extract-golden.json) through a LIVE sidecar and asserts
// a floor on extracted text per case. Its reason to exist: a page whose body is
// wrapped in an iframe (Naver blog/cafe) silently extracted to ~0 chars until
// frame-aware traversal landed — a regression no unit test caught because it
// needs a real browser + real page. This is the WPT-style idea (Kitesurf),
// scoped to the handful of surfaces that actually matter here.
//
// Network-dependent → MANUAL probe, NOT a CI gate. Run after touching the
// extraction path or the sidecar:
//   cd scripts/browser && npm run bench
// Exit 0 = all floors met · 1 = a regression · 2 = sidecar unreachable (env,
// not a code regression — start it with start-browser-sidecar.sh).
import fs from "node:fs";

const SIDECAR = (process.env.DENEB_BROWSE_URL || "http://127.0.0.1:18930").replace(/\/$/, "");
const golden = JSON.parse(
  fs.readFileSync(new URL("./extract-golden.json", import.meta.url), "utf8"),
);

async function probe(kase) {
  const res = await fetch(`${SIDECAR}/browse`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ url: kase.url, waitMs: kase.waitMs || 2000 }),
  });
  const out = await res.json();
  if (!out.ok) return { pass: false, chars: 0, reason: `sidecar error: ${out.error || res.status}` };
  const text = out.text || "";
  const chars = text.length;
  if (chars < (kase.minChars || 1)) {
    return { pass: false, chars, reason: `${chars} chars < floor ${kase.minChars}` };
  }
  for (const kw of kase.mustInclude || []) {
    if (!text.includes(kw)) return { pass: false, chars, reason: `missing keyword ${JSON.stringify(kw)}` };
  }
  return { pass: true, chars, reason: "" };
}

async function main() {
  // Fail fast with exit 2 (env, not regression) if the sidecar is down.
  try {
    const h = await fetch(`${SIDECAR}/health`);
    if (!h.ok) throw new Error(`health ${h.status}`);
  } catch (e) {
    console.error(`sidecar unreachable at ${SIDECAR} (${e.message}). Start it: scripts/browser/start-browser-sidecar.sh start`);
    process.exit(2);
  }

  let failed = 0;
  for (const kase of golden.cases || []) {
    let r;
    try {
      r = await probe(kase);
    } catch (e) {
      r = { pass: false, chars: 0, reason: `probe threw: ${e.message}` };
    }
    if (!r.pass) failed++;
    const mark = r.pass ? "PASS" : "FAIL";
    console.log(`[${mark}] ${kase.name} — ${r.chars} chars${r.reason ? " (" + r.reason + ")" : ""}`);
  }

  const total = (golden.cases || []).length;
  console.log(`BENCH_RESULT passed=${total - failed}/${total} sidecar=${SIDECAR}`);
  process.exit(failed > 0 ? 1 : 0);
}

main();
