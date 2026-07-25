#!/usr/bin/env node
// Behavioural smoke for the Glance plugin, driven through the OFFICIAL Even Hub
// simulator's automation API (@evenrealities/evenhub-simulator ≥0.7.0).
//
// Why this exists: the unit suite covers pure policy (backoff, signatures,
// index guards) but cannot answer the questions that actually decide whether
// the HUD is pleasant to wear — does a background poll leave the display
// alone, and does closing the app really stop the network. Both were fixed in
// #4267 by reading code, with no way to observe the behaviour.
//
// Every #4267 fix is now driven end to end, using the stub gateway's runtime
// modes to induce the conditions the fixes exist for:
//
//   quiet poll   ok    → the framebuffer must stay byte-identical
//   redraw       alt   → a genuinely changed payload must repaint
//   detail       —     → a tap opens a detail, a second tap leaves it
//   detail step  —     → a swipe in a detail steps to the next alert
//   alert cursor —     → it follows what was read; a tap re-opens THAT one
//   paging       —     → swiping past the last alert reaches further pages
//   mid-poll tap slow  → a tap during an in-flight poll must not be dropped
//   deadline     slow  → a hung gateway must not wedge `busy` (🔴 of #4267)
//   backoff      error → the retry interval must widen past the 45s base
//   shutdown     ok    → a double-tap must stop the network for good
//
// It is slow BY CONSTRUCTION (~14 min): the poll interval is 45s and the
// backoff steps are 90s/180s, and shortening them for the test would mean
// testing something other than what ships. This is a nightly lane.
//
// PLATFORM: the simulator ships binaries for darwin-arm64/x64, linux-x64 and
// win32-x64 — there is NO linux-arm64 build, so this cannot run on the ARM
// gateway host. It is a CI lane (ubuntu-latest is x86_64). On an unsupported
// platform it SKIPS rather than fails, so running it locally is harmless.
import { spawn } from 'node:child_process'
import { existsSync, mkdirSync, readdirSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'
import { startStubGateway } from './stub-gateway.mjs'

const TOKEN = 'smoke-token'
const AUTOMATION_PORT = Number(process.env.SMOKE_AUTOMATION_PORT || 9898)
const PREVIEW_PORT = Number(process.env.SMOKE_PREVIEW_PORT || 4319)
// One background cycle plus slack. BASE_REFRESH_MS is 45s in src/refresh.ts;
// the quiet-poll assertion is only meaningful once a cycle has actually fired.
const CYCLE_WAIT_MS = Number(process.env.SMOKE_CYCLE_WAIT_MS || 55_000)
// One poll (≤45s) plus the app's 10s request deadline plus slack: by the end of
// this window a hung gateway must already have been abandoned.
const HANG_WATCH_MS = Number(process.env.SMOKE_HANG_WATCH_MS || 70_000)
// After a failure the next poll is 90s out (45s × 2), so recovery needs a
// longer leash than a normal cycle.
const RECOVERY_WAIT_MS = Number(process.env.SMOKE_RECOVERY_WAIT_MS || 110_000)
// Two failing polls: the first lands ≤45s in, the second 90s after it. 160s
// covers both with slack, which is the minimum that can show a widening gap.
const BACKOFF_WATCH_MS = Number(process.env.SMOKE_BACKOFF_WATCH_MS || 160_000)
// After two failures the retry is 180s out; this is how long the harness will
// wait for the link to be believed again.
const REBOUND_WAIT_MS = Number(process.env.SMOKE_REBOUND_WAIT_MS || 210_000)
// Whole-run watchdog. The checks below deliberately sleep for minutes at a
// time, so a wedged simulator would otherwise sit until the CI job's own
// timeout with no artifacts written.
const WATCHDOG_MS = Number(process.env.SMOKE_WATCHDOG_MS || 1_500_000)
const ARTIFACTS = join(process.cwd(), 'smoke-artifacts')

const children = []
let stub

const spawnErrors = []

function sh(label, cmd, args, opts = {}) {
  const child = spawn(cmd, args, { stdio: ['ignore', 'pipe', 'pipe'], ...opts })
  children.push(child)
  // A spawn failure arrives as an async 'error' EVENT, not a thrown exception:
  // without this listener Node kills the process with an unhandled error and
  // the harness's own diagnostics never run. That is exactly how the first CI
  // run reported an ENOENT with no artifacts (2026-07-25).
  child.on('error', (err) => {
    const msg = `${label} failed to start: ${err.code || ''} ${err.message}`
    spawnErrors.push(msg)
    process.stderr.write(`${msg}\n`)
  })
  // An early exit is a start failure too — the simulator prints its reason and
  // quits (e.g. "Unsupported platform"). Recording it lets the readiness wait
  // fail in a second with the real cause instead of timing out in a minute.
  child.on('exit', (code, signal) => {
    if (code !== 0 && signal !== 'SIGTERM') {
      spawnErrors.push(`${label} exited early (code=${code} signal=${signal ?? 'none'})`)
    }
  })
  child.stdout.on('data', (d) => process.stdout.write(`[${label}] ${d}`))
  child.stderr.on('data', (d) => process.stderr.write(`[${label}] ${d}`))
  return child
}

/**
 * simulatorEntry resolves the simulator's real entry FILE rather than trusting
 * the node_modules/.bin symlink.
 *
 * The first CI run died on `spawn node_modules/.bin/evenhub-simulator ENOENT`
 * even though `npm ci` reported the package installed and the same relative
 * spawn works locally — so the link is not something this harness should
 * depend on. Running the entry through process.execPath removes three failure
 * modes at once: the symlink, the shebang interpreter, and PATH.
 */
function simulatorEntry() {
  const entry = join('node_modules', '@evenrealities', 'evenhub-simulator', 'bin', 'index.js')
  if (existsSync(entry)) return entry
  const dir = join('node_modules', '@evenrealities')
  const installed = existsSync(dir) ? readdirSync(dir).join(', ') : '(no @evenrealities dir)'
  throw new Error(
    `simulator entry not found at ${entry}. Installed @evenrealities packages: ${installed}. ` +
      `Run npm ci in even-g2/.`,
  )
}

/**
 * viteEntry resolves vite's own entry for the same reason.
 *
 * `npx vite preview` also WORKS but leaves the server behind: SIGTERM goes to
 * the npx wrapper and the grandchild keeps the port, so the next run dies on
 * `--strictPort` with "Port 4319 is already in use" (observed 2026-07-25).
 * Spawning the entry directly makes the preview server the child that cleanup
 * actually signals.
 */
function viteEntry() {
  const entry = join('node_modules', 'vite', 'bin', 'vite.js')
  if (existsSync(entry)) return entry
  throw new Error(`vite entry not found at ${entry}. Run npm ci in even-g2/.`)
}

const sleep = (ms) => new Promise((r) => setTimeout(r, ms))

async function waitFor(label, probe, timeoutMs = 60_000) {
  const deadline = Date.now() + timeoutMs
  let lastErr
  while (Date.now() < deadline) {
    try {
      if (await probe()) return
    } catch (err) {
      lastErr = err
    }
    await sleep(500)
  }
  throw new Error(`timed out waiting for ${label}${lastErr ? `: ${lastErr.message}` : ''}`)
}

const api = (path, init) => fetch(`http://127.0.0.1:${AUTOMATION_PORT}${path}`, init)

async function screenshot(name) {
  const res = await api('/api/screenshot/glasses')
  if (!res.ok) throw new Error(`screenshot ${name}: HTTP ${res.status}`)
  const buf = Buffer.from(await res.arrayBuffer())
  writeFileSync(join(ARTIFACTS, `${name}.png`), buf)
  return buf
}

async function console_(sinceId) {
  const res = await api(`/api/console${sinceId ? `?since_id=${sinceId}` : ''}`)
  if (!res.ok) throw new Error(`console: HTTP ${res.status}`)
  return res.json()
}

const input = (action) =>
  api('/api/input', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ action }),
  })

function cleanup() {
  for (const c of children) {
    try {
      c.kill('SIGTERM')
    } catch {
      /* already gone */
    }
  }
  return stub?.close?.()
}

async function main() {
  // SMOKE_FORCE=1 runs the harness anyway. On ARM the simulator itself still
  // refuses to start, but everything up to that point — stub gateway, vite
  // preview, preflight, spawn wiring, failure reporting — gets exercised. That
  // is the only way to validate this plumbing on the ARM gateway host.
  if (process.platform === 'linux' && process.arch === 'arm64' && process.env.SMOKE_FORCE !== '1') {
    console.log(
      'SKIP: the Even Hub simulator has no linux-arm64 build (CI runs this on x86_64). ' +
        'SMOKE_FORCE=1 to exercise the harness plumbing anyway.',
    )
    return 0
  }
  mkdirSync(ARTIFACTS, { recursive: true })

  // Preflight, printed before anything can fail: if the next CI run still
  // cannot start the simulator, this block says why without another round trip.
  const entry = simulatorEntry()
  const preflight = {
    platform: `${process.platform}-${process.arch}`,
    node: process.version,
    cwd: process.cwd(),
    simulatorEntry: entry,
    evenrealities: readdirSync(join('node_modules', '@evenrealities')),
    binLinks: existsSync(join('node_modules', '.bin'))
      ? readdirSync(join('node_modules', '.bin')).filter((f) => f.includes('even'))
      : [],
    display: process.env.DISPLAY || '(unset)',
  }
  console.log('preflight', JSON.stringify(preflight))
  writeFileSync(join(ARTIFACTS, 'preflight.json'), JSON.stringify(preflight, null, 2))

  stub = await startStubGateway({ token: TOKEN })
  console.log(`stub gateway on :${stub.port}`)

  sh('vite', process.execPath, [
    viteEntry(),
    'preview',
    '--port',
    String(PREVIEW_PORT),
    '--strictPort',
    '--host',
    '127.0.0.1',
  ])
  await waitFor('vite preview', async () => (await fetch(`http://127.0.0.1:${PREVIEW_PORT}/`)).ok)

  // Seed the app through the documented bootstrap query so no localStorage or
  // baked runtime-config.json is required.
  const appUrl =
    `http://127.0.0.1:${PREVIEW_PORT}/?baseUrl=${encodeURIComponent(stub.base)}` +
    `&token=${encodeURIComponent(TOKEN)}`

  sh('simulator', process.execPath, [entry, appUrl, '--automation-port', String(AUTOMATION_PORT)])
  await waitFor('simulator automation API', async () => {
    // Surface a start failure immediately instead of burning the whole timeout.
    if (spawnErrors.length) throw new Error(spawnErrors.join('; '))
    return (await api('/api/ping')).ok
  })

  const failures = []
  const check = (ok, msg) => {
    console.log(`${ok ? 'PASS' : 'FAIL'}  ${msg}`)
    if (!ok) failures.push(msg)
  }

  // ── boot ────────────────────────────────────────────────────────────────
  await waitFor('first glance fetch', async () => stub.counts().glance >= 1, 30_000)
  await sleep(1_500)
  const booted = await screenshot('01-boot')
  check(booted.length > 1_000, `boot renders a non-empty framebuffer (${booted.length}B)`)

  const bootLogs = await console_()
  const bootErrors = JSON.stringify(bootLogs).toLowerCase()
  check(
    !bootErrors.includes('uncaught') && !bootErrors.includes('failed to fetch'),
    'boot produced no uncaught error or failed fetch',
  )

  // ── the two claims #4267 could not observe ──────────────────────────────
  // The stub payload never changes, so a background poll must leave the
  // display byte-identical. Before the fix the loop wrote "불러오는 중…" over
  // the wearer's view every cycle and rebuilt the container unconditionally.
  const glanceBefore = stub.counts().glance
  console.log(`waiting ${Math.round(CYCLE_WAIT_MS / 1000)}s for a background cycle…`)
  await sleep(CYCLE_WAIT_MS)
  const afterQuiet = await screenshot('02-after-background-cycle')
  const glanceAfter = stub.counts().glance

  check(glanceAfter > glanceBefore, `background poll actually fired (${glanceBefore} → ${glanceAfter})`)
  check(
    Buffer.compare(booted, afterQuiet) === 0,
    'unchanged payload left the HUD byte-identical (no loading flash, no rebuild)',
  )

  // ── alert cursor + paging ───────────────────────────────────────────────
  // Runs HERE, on the ok payload, because it needs more than one alert.
  //
  // These were OBSERVATIONS until the alerts moved off the host's list
  // container. The host owned the selection and told the app nothing about it
  // — no index on the click, no scroll event at the end of the list — so the
  // two things that matter most about an alert page could only be watched, not
  // asserted, and the simulator's README disclaims that area's fidelity
  // anyway. With an app-owned cursor on a text container both are the app's
  // behaviour, so both are gated here.
  await input('click')
  await sleep(2_500)
  const detailOfAlert0 = await screenshot('03-detail-of-alert-0')

  // A swipe inside a detail steps to the NEXT alert rather than dropping back
  // to the list. Reading a morning's alerts is one gesture each instead of
  // three (detail → list → tap).
  await input('down')
  await sleep(2_500)
  const detailOfAlert1 = await screenshot('04-detail-of-alert-1')
  check(
    Buffer.compare(detailOfAlert0, detailOfAlert1) !== 0,
    'a swipe in a detail steps to the next alert',
  )

  // Past the last alert, the swipe leaves the detail.
  await input('down')
  await sleep(2_500)
  const alertPage = await screenshot('05-back-on-the-alert-page')
  check(
    Buffer.compare(detailOfAlert1, alertPage) !== 0,
    'a swipe past the last alert leaves the detail',
  )

  // The cursor stayed on the alert that was being read, so a tap re-opens THAT
  // one — byte-identical to the detail seen a moment ago, which also proves the
  // tap is not just always opening the first alert.
  await input('click')
  await sleep(2_500)
  const reopened = await screenshot('06-reopened-alert-1')
  check(
    Buffer.compare(reopened, detailOfAlert1) === 0,
    'the cursor followed the alert that was read (a tap re-opens it, not the first)',
  )
  await input('click') // back to the alert page, cursor still on alert 1
  await sleep(2_500)

  // With the cursor on the LAST alert, further swipes must leave the page.
  // The host list stopped dead here, which stranded cal/todo behind the alerts
  // whenever there were any — a tap opens a detail and a double-tap exits, so
  // there was no other way out.
  const beforePaging = await screenshot('07-before-paging-away')
  const pageShots = []
  for (const i of [1, 2, 3]) {
    await input('down')
    await sleep(2_500)
    pageShots.push(await screenshot(`08-page-down-${i}`))
  }
  const framesSeen = new Set([beforePaging, ...pageShots].map((b) => b.toString('base64'))).size
  check(
    framesSeen >= 3,
    `swiping past the last alert reaches further pages (${framesSeen} distinct frames over ${pageShots.length} swipes)`,
  )

  // Walk back up. Four is enough to bottom out at home from anywhere above,
  // and it leaves the status screen — the background poll deliberately skips
  // while status is up, and every check below reads that poll.
  for (const i of [1, 2, 3, 4]) {
    await input('up')
    await sleep(2_000)
    if (i === 4) await screenshot('09-swiped-back-home')
  }

  // ── the other half of change detection: a REAL change must redraw ───────
  // The quiet check above only proves "unchanged → no redraw". On its own a
  // detector stuck closed passes it while the HUD goes permanently stale, so
  // the same background path is now given a genuinely different payload.
  //
  // Deliberately NOT triggered by a tap: on a list page a tap opens a detail
  // (that is what listEvent means), so a tap-driven version of this check
  // would pass on a screen change that has nothing to do with the payload.
  const beforeChange = await screenshot('10-before-payload-change')
  await stub.setMode('alt')
  console.log(`waiting ${Math.round(CYCLE_WAIT_MS / 1000)}s for the changed payload to land…`)
  await sleep(CYCLE_WAIT_MS)
  const afterChange = await screenshot('11-after-payload-change')
  check(
    Buffer.compare(beforeChange, afterChange) !== 0,
    'a changed payload DID redraw the HUD (change detection is not stuck closed)',
  )

  // ── list tap opens a detail, and tapping again leaves it ────────────────
  const listView = afterChange
  await input('click')
  await sleep(2_500)
  const detailView = await screenshot('12-detail')
  check(Buffer.compare(listView, detailView) !== 0, 'a list tap opens a detail screen')
  await input('click')
  await sleep(2_500)
  const backToList = await screenshot('13-back-to-list')
  // Byte-equality with listView would be wrong to assert: the list header
  // carries a relative "N분 전" stamp that ticks over.
  check(
    Buffer.compare(detailView, backToList) !== 0,
    'tapping in a detail leaves it (returns to the list)',
  )

  // ── a tap DURING a poll must still work ─────────────────────────────────
  // Regression test for a defect this harness found by being flaky: run
  // 30171967100 delivered a listEvent that changed nothing, and the identical
  // tap 2.5s later worked. openDetail was gated on `busy`, so any tap that
  // happened to land while a background poll was in flight was silently
  // dropped — a 45s-periodic window up to 10s wide, on the app's PRIMARY
  // interaction.
  //
  // `slow` makes that window deterministic: the stub holds the response, so
  // once a request has arrived the app is provably inside refreshGlance.
  await stub.setMode('slow')
  const beforeInflight = stub.counts().glance
  await waitFor('a poll to be in flight', async () => stub.counts().glance > beforeInflight, 60_000)
  const duringPoll = await screenshot('14-during-hung-poll')
  await input('click')
  await sleep(3_000)
  const tappedDuringPoll = await screenshot('15-tapped-during-hung-poll')
  check(
    Buffer.compare(duringPoll, tappedDuringPoll) !== 0,
    'a tap during an in-flight poll still opens the detail (navigation is not gated on the network)',
  )
  await input('click') // back to the list
  await sleep(2_500)

  // ── the request deadline: a hung gateway must not wedge the app ─────────
  // The 🔴 of #4267 and previously unobserved. Without the deadline
  // refreshGlance holds `busy = true` forever, every input handler returns
  // early, and runScheduledRefresh stops fetching — the app is dead until the
  // WebView restarts. The count is the proof: it can only keep rising if
  // `busy` was released.
  const hangFrom = await screenshot('16-before-hung-gateway')
  console.log(`waiting ${Math.round(HANG_WATCH_MS / 1000)}s across a hung poll…`)
  await sleep(HANG_WATCH_MS)
  const afterHang = await screenshot('17-after-hung-gateway')
  check(
    Buffer.compare(hangFrom, afterHang) === 0,
    'a hung background poll stayed off the display (no error flash in the wearer’s view)',
  )

  await stub.setMode('ok')
  const countAtRecovery = stub.counts().glance
  console.log(`waiting up to ${Math.round(RECOVERY_WAIT_MS / 1000)}s for polling to resume…`)
  let resumed = true
  try {
    await waitFor('a poll after the hang', async () => stub.counts().glance > countAtRecovery, RECOVERY_WAIT_MS)
  } catch {
    resumed = false
  }
  check(resumed, 'polling resumed after a hung request (the deadline released `busy`)')

  // ── failure backoff: retries must spread out ────────────────────────────
  await stub.setMode('error')
  const gapsBefore = stub.glanceGaps().length
  console.log(`waiting ${Math.round(BACKOFF_WATCH_MS / 1000)}s to watch the retry interval widen…`)
  await sleep(BACKOFF_WATCH_MS)
  const gaps = stub.glanceGaps().slice(gapsBefore)
  const lastGap = gaps[gaps.length - 1]
  // Base is 45s and the first failure doubles it to 90s. An absolute floor is
  // used rather than a ratio so that "too few samples" fails loudly instead of
  // passing vacuously.
  check(
    gaps.length >= 2 && lastGap > 60_000,
    `retry interval widened past the 45s base under failure (gaps ms: ${gaps.join(', ') || 'none'})`,
  )
  check(
    !JSON.stringify(await console_()).toLowerCase().includes('uncaught'),
    'the induced failures produced no uncaught error',
  )

  // ── shutdown must stop the network ──────────────────────────────────────
  // Before the fix, setInterval outlived shutDownPageContainer and kept
  // hitting the gateway for the life of the WebView.
  //
  // The backoff above left the next retry ~180s out, which would make a
  // "no traffic for 55s" assertion true whether or not shutdown works. So the
  // link is believed again FIRST, and the interval is asserted to be back at
  // base before the double-tap — otherwise this check is vacuous.
  await stub.setMode('ok')
  console.log(`waiting up to ${Math.round(REBOUND_WAIT_MS / 1000)}s for the backoff to unwind…`)
  const countAtRebound = stub.counts().glance
  let rebounded = true
  try {
    await waitFor('a successful poll after the backoff', async () => stub.counts().glance > countAtRebound, REBOUND_WAIT_MS)
    // …and one more at the reset interval, so the last observed gap is a base gap.
    const afterSuccess = stub.counts().glance
    await waitFor('the next poll at base interval', async () => stub.counts().glance > afterSuccess, CYCLE_WAIT_MS + 15_000)
  } catch {
    rebounded = false
  }
  const preShutdownGaps = stub.glanceGaps()
  const preShutdownGap = preShutdownGaps[preShutdownGaps.length - 1]
  check(
    rebounded && preShutdownGap !== undefined && preShutdownGap < 60_000,
    `a success reset the backoff to base before shutdown (last gap ${preShutdownGap ?? 'none'}ms)`,
  )

  await input('double_click')
  await sleep(2_000)
  const countAtShutdown = stub.counts().glance
  console.log(`waiting ${Math.round(CYCLE_WAIT_MS / 1000)}s to prove polling stopped…`)
  await sleep(CYCLE_WAIT_MS)
  const countAfterShutdown = stub.counts().glance
  check(
    countAfterShutdown === countAtShutdown,
    `no gateway traffic after shutdown (${countAtShutdown} → ${countAfterShutdown})`,
  )
  await screenshot('18-after-shutdown')

  const finalLogs = await console_()
  writeFileSync(join(ARTIFACTS, 'console.json'), JSON.stringify(finalLogs, null, 2))
  writeFileSync(join(ARTIFACTS, 'stub-counts.json'), JSON.stringify(stub.counts(), null, 2))

  if (failures.length) {
    console.error(`\n${failures.length} check(s) failed:`)
    for (const f of failures) console.error(`  - ${f}`)
    console.error(`\nArtifacts (screenshots + console) under ${ARTIFACTS}`)
    return 1
  }
  console.log('\nAll simulator smoke checks passed.')
  return 0
}

// Watchdog: the checks sleep for minutes at a time, so a wedged simulator would
// otherwise sit until the CI job's own timeout with nothing written to disk.
const watchdog = setTimeout(async () => {
  console.error(`\nsmoke harness watchdog fired after ${Math.round(WATCHDOG_MS / 1000)}s`)
  try {
    mkdirSync(ARTIFACTS, { recursive: true })
    writeFileSync(join(ARTIFACTS, 'error.txt'), `watchdog: no result after ${WATCHDOG_MS}ms`)
  } catch {
    /* best effort */
  }
  await cleanup()
  process.exit(1)
}, WATCHDOG_MS)

let code = 1
try {
  code = await main()
} catch (err) {
  console.error(`smoke harness error: ${err?.stack || err}`)
  try {
    mkdirSync(ARTIFACTS, { recursive: true })
    writeFileSync(join(ARTIFACTS, 'error.txt'), String(err?.stack || err))
  } catch {
    /* best effort */
  }
} finally {
  await cleanup()
}
process.exit(code)
