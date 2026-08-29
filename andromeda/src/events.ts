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
  // Deep-link target: kind is a pane key, ref the resource id (or wiki path) the
  // nudge opens. Both set by the gateway push payload; absent → not navigable.
  ref?: string;
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

// ---- Gateway event plane (spectate) ----------------------------------------
// Desktop connections additionally join the gateway event broadcaster: the
// stream opens with `event: hello` {connId}, and session-scoped agent events
// (subscribed per session via the sessions.messages.subscribe RPC against that
// connId) arrive as `event: gateway` frames. This module fans them out so any
// surface (the foreign-turn spectate in useSessions) can listen without
// threading callbacks through the component tree.

export interface GatewayEventFrame {
  event: string; // e.g. "agent.event"
  payload: Record<string, unknown>;
}

type GatewayListener = (frame: GatewayEventFrame) => void;
type HelloListener = (connId: string) => void;

let currentConnId: string | null = null;
const gatewayListeners = new Set<GatewayListener>();
const helloListeners = new Set<HelloListener>();

/** connId of the live events stream, or null while (re)connecting. */
export function eventsConnId(): string | null {
  return currentConnId;
}

/** Listen for gateway event frames. Returns an unlisten function. */
export function onGatewayEvent(fn: GatewayListener): () => void {
  gatewayListeners.add(fn);
  return () => gatewayListeners.delete(fn);
}

/** Fires on every (re)connect with the fresh connId — resubscribe here. */
export function onEventsHello(fn: HelloListener): () => void {
  helloListeners.add(fn);
  if (currentConnId) fn(currentConnId);
  return () => helloListeners.delete(fn);
}

function dispatchGatewayFrame(obj: Record<string, unknown>) {
  const event = asStr(obj.event);
  if (!event) return;
  const payload = (obj.payload && typeof obj.payload === "object" ? obj.payload : {}) as Record<string, unknown>;
  for (const fn of [...gatewayListeners]) {
    try {
      fn({ event, payload });
    } catch (e) {
      evLog.warn("gateway listener failed", String(e));
    }
  }
}

// Map a raw SSE frame (event name + parsed data object) to a ProactiveEvent,
// tolerating whatever field names the gateway uses.
function toEvent(eventName: string, data: Record<string, unknown>): ProactiveEvent {
  return {
    id: asStr(data.id) ?? crypto.randomUUID(),
    kind: asStr(data.kind) ?? asStr(data.type) ?? (eventName && eventName !== "message" ? eventName : undefined),
    ref: asStr(data.ref),
    title: asStr(data.title) ?? asStr(data.subject),
    body: asStr(data.body) ?? asStr(data.text) ?? asStr(data.message),
    ts: asNum(data.ts) ?? asNum(data.tsMs),
    raw: data,
  };
}

// Subscribe until the signal aborts or the stream ends. Resolves on clean end.
// The client-kind header identifies this subscription as a DESKTOP: the gateway
// keys two behaviors on it — the FCM fallback must not be suppressed by a
// desktop connection, and workstation-control dispatch requires ≥1 desktop.
export async function subscribeEvents(
  cfg: GatewayConfig,
  handlers: EventHandlers,
  signal?: AbortSignal,
): Promise<void> {
  const body = await streamFetch(cfg, "events", { signal, headers: { "X-Deneb-Client-Kind": "desktop" } });
  evLog.info("stream open");
  handlers.onOpen?.();

  try {
    await readJsonSSE(
      body,
      (event, obj) => {
        if (event === "hello") {
          currentConnId = asStr(obj.connId) ?? null;
          // Debug affordance: headless CDP probes read this to verify the
          // event plane attached without console capture.
          (window as unknown as Record<string, unknown>).__DENEB_EVENTS_CONN = currentConnId;
          evLog.info(`gateway plane attached (${currentConnId ?? "?"})`);
          if (currentConnId) for (const fn of [...helloListeners]) fn(currentConnId);
          return;
        }
        if (event === "gateway") {
          dispatchGatewayFrame(obj);
          return;
        }
        const ev = toEvent(event, obj);
        evLog.debug(`event ${ev.kind ?? "?"}`, ev.title ?? "");
        handlers.onEvent?.(ev);
      },
      signal,
    );
  } finally {
    // Stream gone — the connId (and every server-side subscription keyed on
    // it) died with it. Listeners resubscribe on the next hello.
    currentConnId = null;
  }
}
