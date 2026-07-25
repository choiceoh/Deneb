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

  sh('vite', 'npx', ['vite', 'preview', '--port', String(PREVIEW_PORT), '--strictPort', '--host', '127.0.0.1'])
  await waitFor('vite preview', async () => (await fetch(`http://127.0.0.1:${PREVIEW_PORT}/`)).ok)

  // Seed the app through the documented bootstrap query so no localStorage or
  // baked runtime-config.json is required.
  const appUrl =
    `http://127.0.0.1:${PREVIEW_PORT}/?baseUrl=${encodeURIComponent(`http://127.0.0.1:${stub.port}`)}` +
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

  // ── input still works after the quiet cycle ─────────────────────────────
  await input('click')
  await sleep(2_000)
  const afterClick = await screenshot('03-after-click')
  check(Buffer.compare(booted, afterClick) !== 0, 'a tap changes the screen (input path alive)')

  // ── shutdown must stop the network ──────────────────────────────────────
  // Before the fix, setInterval outlived shutDownPageContainer and kept
  // hitting the gateway for the life of the WebView.
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
