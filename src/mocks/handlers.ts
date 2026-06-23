// Mock gateway — MSW handlers that mirror the Deneb miniapp.* contract. Used by
// tests (msw/node) and dev mock mode (msw/browser). The RPC dispatch table below
// doubles as a living spec of which methods the workstation calls and their shapes.
import { http, HttpResponse } from "msw";
import * as fx from "./fixtures";

interface RpcBody {
  id: string;
  method: string;
  params?: Record<string, any>;
}

const ok = (payload: unknown) => HttpResponse.json({ ok: true, payload });
const fail = (message: string) => HttpResponse.json({ ok: false, error: { code: "mock", message } });

// method → (params) → payload
const RPC: Record<string, (p: Record<string, any>) => unknown> = {
  "miniapp.ping": () => ({ ok: true, version: "mock", model: "mock-model", tsMs: Date.now() }),

  // List RPCs wrap their rows in a payload object keyed by the resource (+ meta),
  // mirroring the real gateway — NOT a bare array. The data provider unwraps via
  // resources.ts listKey; keeping the mock wrapped stops it from masking shape bugs.
  "miniapp.todo.list": () => ({ todos: fx.todos }),
  "miniapp.todo.create": (p) => ({ id: `t${Date.now()}`, title: p.title, done: false }),
  "miniapp.todo.update": (p) => ({ ...p }),
  "miniapp.todo.set_done": (p) => ({ id: p.id, done: p.done }),
  "miniapp.todo.delete": (p) => ({ id: p.id }),

  "miniapp.gmail.list_recent": () => ({ messages: fx.mail, nextPageToken: "" }),
  "miniapp.gmail.get": (p) => fx.mail.find((m) => String(m.id) === String(p.id)) ?? null,
  "miniapp.gmail.mark_read": () => ({ ok: true }),
  "miniapp.gmail.archive": () => ({ ok: true }),
  "miniapp.gmail.trash": (p) => ({ id: p.id, ok: true }),

  "miniapp.calendar.list_upcoming": () => ({ events: fx.events }),
  "miniapp.calendar.get": (p) => fx.events.find((e) => String(e.id) === String(p.id)) ?? null,
  "miniapp.calendar.create": (p) => ({ id: `e${Date.now()}`, local: true, ...p }),
  "miniapp.calendar.update": (p) => ({ ...p }),
  "miniapp.calendar.delete": () => ({ ok: true }),

  "miniapp.people.list": () => ({ people: fx.people, windowDays: 30, scannedCount: fx.people.length }),

  "miniapp.project.digests": () => ({ digests: fx.digests }),

  "miniapp.crons.list": () => ({ jobs: fx.crons, total: fx.crons.length }),
  "miniapp.crons.update": (p) => ({ ...p }),
  "miniapp.crons.run": () => ({ enqueued: true }),
  "miniapp.crons.remove": () => ({ removed: true }),

  "miniapp.workfeed.list": () => ({ count: fx.workfeed.length, items: fx.workfeed, total: fx.workfeed.length }),
  "miniapp.workfeed.ack": (p) => ({ ok: true, item: fx.workfeed.find((w) => String(w.id) === String(p.id)) ?? null }),
  "miniapp.workfeed.action.run": () => ({ ok: true, removeFromFeed: true }),

  // memory.search wraps hits as { results }; get_page/write_page carry the body
  // under `body` — mirror the real gateway (handlerminiapp/memory*.go) so the mock
  // can't mask the field-name shape the wiki pane depends on.
  "miniapp.memory.search": () => ({ results: fx.pages }),
  "miniapp.memory.get_page": (p) => ({
    path: p.path,
    title: String(p.path),
    body: `# ${p.path}\n\n목 위키 내용입니다.`,
  }),
  "miniapp.memory.write_page": (p) => ({ path: p.path, body: p.body }),

  "miniapp.search.all": (p) => fx.searchAll(typeof p.query === "string" ? p.query : ""),
};

function sse(frames: string[]) {
  const encoder = new TextEncoder();
  const stream = new ReadableStream({
    start(controller) {
      for (const f of frames) controller.enqueue(encoder.encode(f));
      controller.close();
    },
  });
  return new HttpResponse(stream, { headers: { "Content-Type": "text/event-stream", "Cache-Control": "no-cache" } });
}

// `*` matches any origin so whatever gateway URL is configured gets intercepted.
export const handlers = [
  http.post("*/api/v1/miniapp/rpc", async ({ request }) => {
    const body = (await request.json()) as RpcBody;
    const fn = RPC[body.method];
    return fn ? ok(fn(body.params ?? {})) : fail(`mock: unknown method ${body.method}`);
  }),

  http.post("*/api/v1/miniapp/chat/stream", () =>
    sse([
      'event: delta\ndata: {"delta":"목 게이트웨이 응답입니다. "}\n',
      'event: delta\ndata: {"delta":"실제 데네브 대신 모킹 중이에요."}\n',
      'event: done\ndata: {"text":"목 응답","model":"mock-model"}\n',
    ]),
  ),

  http.get("*/api/v1/miniapp/events", () =>
    sse(['event: nudge\ndata: {"id":"n1","title":"목 알림","body":"events SSE 모킹이 동작 중입니다."}\n']),
  ),
];
