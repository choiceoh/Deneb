#!/usr/bin/env node
// andromeda-drive — CDP driver for live-verifying Andromeda/Cygnus surfaces.
//
// andromeda-shot.sh answers "what does it look like"; this answers "what does
// it DO": it boots a headless chromium against the running Vite dev server,
// types into the chat composer, sends, and samples the working state (DOM
// probe + screenshots) while the turn runs. It caught the defects static
// screenshots cannot: dead composers, chips losing hints mid-turn, disclosure
// layout breaking only in the expanded state.
//
// Usage (repo root, `pnpm dev` already running in andromeda/):
//   node scripts/dev/andromeda-drive.mjs shot  <url> <outdir>            # boot → settle → 1 screenshot
//   node scripts/dev/andromeda-drive.mjs send  <url> <outdir> "<prompt>" # type+send, sample 4 shots over ~22s
//   node scripts/dev/andromeda-drive.mjs eval  <url> <outdir> "<js>"     # run JS (e.g. open every <details>), then screenshot
//
//   <url> example: "http://localhost:1422/?window=cygnus"
//   Options: --size 480x700 (default) · --settle 3500 (ms before acting)
//
// The chromium child is owned by this process and killed on exit — no orphan
// to pkill (and no pkill self-match trap). Node ≥ 22 (global WebSocket/fetch).
import { writeFileSync, globSync } from "node:fs";
import { spawn } from "node:child_process";

const args = process.argv.slice(2);
const mode = args[0];
const url = args[1];
const outDir = args[2];
const extra = args[3] ?? "";
if (!["shot", "send", "eval"].includes(mode) || !url || !outDir) {
  console.error("usage: andromeda-drive.mjs shot|send|eval <url> <outdir> [prompt|js] [--size WxH] [--settle ms]");
  process.exit(2);
}
const flag = (name, dflt) => {
  const i = args.indexOf(name);
  return i >= 0 && args[i + 1] ? args[i + 1] : dflt;
};
const [w, h] = flag("--size", "480x700").split("x");
const settleMs = Number(flag("--settle", "3500"));
const port = 9222 + Math.floor(Math.random() * 400); // avoid clashing with a parallel run

const chromeBin = globSync(`${process.env.HOME}/.cache/ms-playwright/chromium-*/chrome-linux/chrome`).sort().at(-1);
if (!chromeBin) {
  console.error("no playwright chromium under ~/.cache/ms-playwright — run npx playwright install chromium");
  process.exit(1);
}
const chrome = spawn(
  chromeBin,
  [
    "--headless=new",
    "--no-sandbox",
    "--disable-gpu",
    "--hide-scrollbars",
    "--force-device-scale-factor=2",
    `--window-size=${w},${h}`,
    `--remote-debugging-port=${port}`,
    url,
  ],
  { stdio: "ignore" },
);
process.on("exit", () => chrome.kill("SIGKILL"));

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

// --- tiny CDP client (Node's global WebSocket; no deps) ---
let ws;
let msgId = 0;
const pending = new Map();
async function connect() {
  let target;
  for (let i = 0; i < 50 && !target; i++) {
    try {
      const list = await fetch(`http://127.0.0.1:${port}/json/list`).then((r) => r.json());
      target = list.find((t) => t.type === "page" && t.url.startsWith("http"))?.webSocketDebuggerUrl;
    } catch {
      /* chrome still booting */
    }
    if (!target) await sleep(300);
  }
  if (!target) throw new Error("no page target — is the dev server running?");
  ws = new WebSocket(target);
  await new Promise((res, rej) => {
    ws.onopen = res;
    ws.onerror = rej;
  });
  ws.onmessage = (ev) => {
    const m = JSON.parse(ev.data);
    if (m.id && pending.has(m.id)) {
      const { res, rej } = pending.get(m.id);
      pending.delete(m.id);
      m.error ? rej(new Error(JSON.stringify(m.error))) : res(m.result);
    }
  };
}
function send(method, params = {}) {
  const id = ++msgId;
  ws.send(JSON.stringify({ id, method, params }));
  return new Promise((res, rej) => pending.set(id, { res, rej }));
}
const evaluate = async (expression) =>
  (await send("Runtime.evaluate", { expression, returnByValue: true, awaitPromise: true })).result?.value;
async function shot(name) {
  const { data } = await send("Page.captureScreenshot", { format: "png" });
  writeFileSync(`${outDir}/${name}.png`, Buffer.from(data, "base64"));
  console.log(`shot ${outDir}/${name}.png`);
}

await connect();
await send("Page.enable");
await send("Runtime.enable");
await sleep(settleMs); // app boot + gateway connect

if (mode === "shot") {
  await shot("drive");
} else if (mode === "eval") {
  console.log("eval:", await evaluate(extra));
  await sleep(600);
  await shot("drive-eval");
} else {
  // send: React-controlled textarea needs the native setter + an input event.
  const typed = await evaluate(`(() => {
    const ta = document.querySelector('.ai-compose');
    if (!ta) return 'no-composer';
    const set = Object.getOwnPropertyDescriptor(window.HTMLTextAreaElement.prototype, 'value').set;
    set.call(ta, ${JSON.stringify(extra)});
    ta.dispatchEvent(new Event('input', { bubbles: true }));
    return 'typed';
  })()`);
  console.log("type:", typed);
  await sleep(400);
  await shot("w0-typed");
  console.log("send:", await evaluate(`(() => {
    const btn = document.querySelector('.ai-send');
    if (!btn) return 'no-send';
    btn.click();
    return 'clicked';
  })()`));
  for (const [ms, name] of [
    [1200, "w1-early"],
    [3000, "w2-mid"],
    [6000, "w3-late"],
    [12000, "w4-later"],
  ]) {
    await sleep(ms);
    const state = await evaluate(`(() => JSON.stringify({
      busy: !!document.querySelector('.ai-send.stop, .ai-stop, [data-busy="true"]'),
      chips: document.querySelectorAll('.tool-chip').length,
      status: document.querySelector('.deneb-status, .ai-status, .turn-progress')?.textContent?.slice(0, 80) || '',
      turns: document.querySelectorAll('.ai-turn').length,
    }))()`);
    console.log(name, state);
    await shot(name);
  }
}
process.exit(0);
