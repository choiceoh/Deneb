// Proactive push channel — GET /api/v1/miniapp/events (SSE). The gateway nudges
// the workstation ("이 메일 급해 보여요", "회의 10분 전") without being asked; this
// client streams those frames so a ProactivePanel can surface them.
//
// EventSource can't set the X-Deneb-Client-Token header, so we read the SSE stream
// off fetch() with an AbortSignal (same approach as chatStream).
import { type GatewayConfig, TOKEN_HEADER, base } from "./gateway";
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
  const str = (v: unknown) => (typeof v === "string" ? v : undefined);
  const num = (v: unknown) => (typeof v === "number" ? v : undefined);
  return {
    id: str(data.id) ?? crypto.randomUUID(),
    kind: str(data.kind) ?? str(data.type) ?? (eventName && eventName !== "message" ? eventName : undefined),
    title: str(data.title) ?? str(data.subject),
    body: str(data.body) ?? str(data.text) ?? str(data.message),
    ts: num(data.ts) ?? num(data.tsMs),
    raw: data,
  };
}

// Subscribe until the signal aborts or the stream ends. Resolves on clean end.
export async function subscribeEvents(
  cfg: GatewayConfig,
  handlers: EventHandlers,
  signal?: AbortSignal,
): Promise<void> {
  const res = await fetch(`${base(cfg.url)}/api/v1/miniapp/events`, {
    headers: { [TOKEN_HEADER]: cfg.token },
    signal,
  });
  if (!res.ok) {
    evLog.error(`✗ subscribe: HTTP ${res.status}`);
    throw new Error(`events: HTTP ${res.status}`);
  }
  if (!res.body) throw new Error("events: empty response body");
  evLog.info("stream open");
  handlers.onOpen?.();

  const reader = res.body.getReader();
  const decoder = new TextDecoder();
  let buffer = "";
  let event = "";

  for (;;) {
    const { value, done } = await reader.read();
    if (done) break;
    buffer += decoder.decode(value, { stream: true });
    const lines = buffer.split("\n");
    buffer = lines.pop() ?? ""; // keep trailing partial line
    for (const line of lines) {
      if (line.startsWith("event:")) {
        event = line.slice(6).trim();
        continue;
      }
      if (!line.startsWith("data:")) continue;
      const data = line.slice(5).trim();
      if (!data) continue;
      try {
        const obj = JSON.parse(data) as Record<string, unknown>;
        const ev = toEvent(event, obj);
        evLog.debug(`event ${ev.kind ?? "?"}`, ev.title ?? "");
        handlers.onEvent?.(ev);
      } catch {
        /* ignore malformed frame */
      }
      event = "";
    }
  }
}
