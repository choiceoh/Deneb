// Deterministic stand-in for the Deneb gateway's /api/even/* surface.
//
// The smoke harness needs two things a real gateway cannot give it: a payload
// that never changes (so "the HUD must not redraw" is a real assertion, not a
// coin flip) and a request counter (so "polling stopped after shutdown" is
// observable at all).
import { createServer } from 'node:http'

/**
 * startStubGateway serves a fixed glance payload and counts every hit.
 *
 * @param {{ token: string }} opts
 * @returns {Promise<{ port: number, counts: () => Record<string, number>, close: () => Promise<void> }>}
 */
export async function startStubGateway({ token }) {
  const counts = { glance: 0, status: 0, unauthorized: 0 }

  const glance = {
    text: '알림 2건 · 14:00 회의',
    // Fixed `generated`: the app deliberately excludes it from its render
    // signature, and pinning it keeps the payload byte-identical anyway.
    generated: '2026-07-25T09:00:00.000Z',
    cached: false,
    pages: [
      { id: 'home', title: '알림', text: '견적 회신 필요\n계약서 검토 요청' },
      { id: 'cal', title: '일정', text: '14:00 금호타이어 회의' },
      { id: 'todo', title: '할 일', text: '주간 보고 정리', empty: false },
    ],
    items: [
      { id: 'a1', title: '견적 회신 필요', preview: '금호타이어 곡성', body: '금호타이어 곡성 리스 견적 회신이 필요합니다.', priority: 4, age: '10분' },
      { id: 'a2', title: '계약서 검토 요청', preview: '기아 광주', body: '기아 광주 EPC 계약서 초안 검토 요청입니다.', priority: 2, age: '1시간' },
    ],
  }

  const server = createServer((req, res) => {
    const url = new URL(req.url ?? '/', 'http://stub.local')
    const authed = req.headers.authorization === `Bearer ${token}`
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
    if (!authed) {
      counts.unauthorized++
      send(401, { error: { message: 'stub: bad token' } })
      return
    }
    if (url.pathname === '/api/even/glance') {
      counts.glance++
      send(200, glance)
      return
    }
    if (url.pathname === '/api/even/status') {
      counts.status++
      send(200, { ok: true, chatReady: true, session: 'glasses:main' })
      return
    }
    send(404, { error: { message: 'stub: not found' } })
  })

  await new Promise((resolve) => server.listen(0, '127.0.0.1', resolve))
  const { port } = server.address()
  return {
    port,
    counts: () => ({ ...counts }),
    close: () => new Promise((resolve) => server.close(() => resolve())),
  }
}
