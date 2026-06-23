// Proactive push channel — GET /api/v1/miniapp/events (SSE). The gateway nudges
// the workstation ("이 메일 급해 보여요", "회의 10분 전") without being asked; this
// client streams those frames so a ProactivePanel can surface them.
//
// EventSource can't set the X-Deneb-Client-Token header, so we read the SSE stream
// off fetch() with an AbortSignal (same approach as chatStream).
import { asNum, asStr } from "./format";
import { type GatewayConfig, streamFetch } from "./gateway";
import { readJsonSSE } from "./sse";
import { log } from "./log";

const evLog = log.child("events");

export interface ProactiveEvent {
  id: string;
  kind?: string;
  title?: string;
  body?: string;
  ts?: number;
  raw: Record<string, unknown>;
}

export interface EventHandlers {
  onOpen?: () => void;
  onEvent?: (ev: ProactiveEvent) => void;
  onError?: (err: string) => void;
}

// Map a raw SSE frame (event name + parsed data object) to a ProactiveEvent,
// tolerating whatever field names the gateway uses.
function toEvent(eventName: string, data: Record<string, unknown>): ProactiveEvent {
  return {
    id: asStr(data.id) ?? crypto.randomUUID(),
    kind: asStr(data.kind) ?? asStr(data.type) ?? (eventName && eventName !== "message" ? eventName : undefined),
    title: asStr(data.title) ?? asStr(data.subject),
    body: asStr(data.body) ?? asStr(data.text) ?? asStr(data.message),
    ts: asNum(data.ts) ?? asNum(data.tsMs),
    raw: data,
  };
}

// Subscribe until the signal aborts or the stream ends. Resolves on clean end.
export async function subscribeEvents(
  cfg: GatewayConfig,
  handlers: EventHandlers,
  signal?: AbortSignal,
): Promise<void> {
  const body = await streamFetch(cfg, "events", { signal });
  evLog.info("stream open");
  handlers.onOpen?.();

  await readJsonSSE(body, (event, obj) => {
    const ev = toEvent(event, obj);
    evLog.debug(`event ${ev.kind ?? "?"}`, ev.title ?? "");
    handlers.onEvent?.(ev);
  });
}
