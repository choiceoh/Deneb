// Deterministic stand-in for the Deneb gateway's /api/even/* surface.
//
// The smoke harness needs more than a fixed payload: to observe the plugin's
// failure behaviour it has to be able to make the gateway misbehave on demand.
// Hence runtime MODES — the harness flips one, drives the glasses, and asserts.
//
//   ok    fixed payload, immediate            (default)
//   slow  never answers within the app's 10s request deadline
//   error 500 on every glance
//   alt   a DIFFERENT fixed payload           (proves a real change redraws)
//
// It also timestamps every request, which is the only way to see a retry
// backoff widening from the outside.
import { createServer } from 'node:http'

const OK_GLANCE = {
  text: '알림 2건 · 14:00 회의',
  // Fixed: the app excludes `generated` from its render signature on purpose,
  // and pinning it keeps the payload byte-identical across polls anyway.
  generated: '2026-07-25T09:00:00.000Z',
  cached: false,
  pages: [
    { id: 'home', title: '알림', text: '견적 회신 필요\n계약서 검토 요청' },
    { id: 'cal', title: '일정', text: '14:00 금호타이어 회의' },
    { id: 'todo', title: '할 일', text: '주간 보고 정리' },
  ],
  items: [
    { id: 'a1', title: '견적 회신 필요', preview: '금호타이어 곡성', body: '금호타이어 곡성 리스 견적 회신이 필요합니다.', priority: 4, age: '10분' },
    { id: 'a2', title: '계약서 검토 요청', preview: '기아 광주', body: '기아 광주 EPC 계약서 초안 검토 요청입니다.', priority: 2, age: '1시간' },
  ],
}

// Deliberately different in EVERY field the render signature reads, so a failed
// change-detection cannot pass by accident.
//
// ONE page on purpose. applyPayload keeps the page the wearer is on by id, so a
// multi-page alt would land wherever the previous checks left the app. With a
// single page the id lookup misses and pageIndex falls back to 0 — the alt
// payload always arrives on the home list, whatever ran before it.
const ALT_GLANCE = {
  text: '알림 1건 · 종일 비움',
  generated: '2026-07-25T09:00:00.000Z',
  cached: false,
  pages: [{ id: 'home', title: '알림', text: '세금계산서 발행 요청' }],
  items: [
    { id: 'b9', title: '세금계산서 발행 요청', preview: '대한전선', body: '대한전선 세금계산서 발행 요청입니다.', priority: 5, age: '2분' },
  ],
}

/**
 * startStubGateway serves the glance surface and lets the harness change its
 * behaviour mid-run.
 *
 * @param {{ token: string }} opts
 */
export async function startStubGateway({ token }) {
  const counts = { glance: 0, status: 0, unauthorized: 0 }
  /** Request arrival times per path, for interval (backoff) assertions. */
  const stamps = { glance: [] }
  let mode = 'ok'
  /** Pending slow responses, so close() cannot hang on them. */
  const openTimers = new Set()

  const server = createServer((req, res) => {
    const url = new URL(req.url ?? '/', 'http://stub.local')
    const send = (code, body) => {
      res.writeHead(code, {
        'Content-Type': 'application/json; charset=utf-8',
        // The app runs from the vite origin, so every call is cross-origin.
        'Access-Control-Allow-Origin': '*',
        'Access-Control-Allow-Headers': 'Authorization, Accept',
      })
      res.end(JSON.stringify(body))
    }

    if (req.method === 'OPTIONS') {
      res.writeHead(204, {
        'Access-Control-Allow-Origin': '*',
        'Access-Control-Allow-Headers': 'Authorization, Accept',
        'Access-Control-Allow-Methods': 'GET, OPTIONS',
      })
      res.end()
      return
    }

    // Harness control plane. Kept on a path the app never calls so it cannot
    // be confused with gateway traffic in the counters.
    if (url.pathname === '/__mode') {
      mode = url.searchParams.get('to') || 'ok'
      send(200, { mode })
      return
    }

    if (req.headers.authorization !== `Bearer ${token}`) {
      counts.unauthorized++
      send(401, { error: { message: 'stub: bad token' } })
      return
    }

    if (url.pathname === '/api/even/status') {
      counts.status++
      send(200, { ok: true, chatReady: true, session: 'glasses:main' })
      return
    }

    if (url.pathname === '/api/even/glance') {
      counts.glance++
      stamps.glance.push(Date.now())
      switch (mode) {
        case 'slow': {
          // Longer than the app's REQUEST_TIMEOUT_MS: the client must give up
          // first, which is exactly what is being tested.
          const t = setTimeout(() => {
            openTimers.delete(t)
            send(200, OK_GLANCE)
          }, 30_000)
          openTimers.add(t)
          return
        }
        case 'error':
          send(500, { error: { message: 'stub: induced failure' } })
          return
        case 'alt':
          send(200, ALT_GLANCE)
          return
        default:
          send(200, OK_GLANCE)
          return
      }
    }

    send(404, { error: { message: 'stub: not found' } })
  })

  await new Promise((resolve) => server.listen(0, '127.0.0.1', resolve))
  const { port } = server.address()
  const base = `http://127.0.0.1:${port}`
  return {
    port,
    base,
    counts: () => ({ ...counts }),
    /** Arrival gaps between consecutive glance requests, in ms. */
    glanceGaps: () =>
      stamps.glance.slice(1).map((t, i) => t - stamps.glance[i]),
    setMode: async (to) => {
      const res = await fetch(`${base}/__mode?to=${to}`)
      if (!res.ok) throw new Error(`stub setMode ${to}: HTTP ${res.status}`)
      return to
    },
    close: () =>
      new Promise((resolve) => {
        for (const t of openTimers) clearTimeout(t)
        openTimers.clear()
        server.closeAllConnections?.()
        server.close(() => resolve())
      }),
  }
}
