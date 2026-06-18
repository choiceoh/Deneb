// Reusable stateful hooks: the AI chat stream machine, gateway health check, and
// the proactive events subscription.
import { useEffect, useState } from "react";
import { useInvalidate } from "@refinedev/core";
import { type GatewayConfig, chatStream, ping } from "./gateway";
import { type ProactiveEvent, subscribeEvents } from "./events";

export interface ChatState {
  out: string;
  thinking: string;
  busy: boolean;
  send: (message: string, workspaceContext: string, activeResource?: string) => Promise<void>;
}

// Drives one Deneb chat/stream turn: streams delta/tool/thinking, and on done
// invalidates the active resource so AI-driven data changes show in the grid.
export function useChat(cfg: GatewayConfig): ChatState {
  const [out, setOut] = useState("");
  const [thinking, setThinking] = useState("");
  const [busy, setBusy] = useState(false);
  const invalidate = useInvalidate();

  async function send(message: string, workspaceContext: string, activeResource?: string) {
    const msg = message.trim();
    if (!msg || busy) return;
    setOut("");
    setThinking("");
    setBusy(true);
    try {
      await chatStream(
        cfg,
        msg,
        {
          onThinking: (preview) => setThinking(preview.slice(0, 90)),
          onDelta: (t) => {
            setThinking("");
            setOut((p) => p + t);
          },
          // Show the AI using tools — the visible half of two-way collaboration.
          onTool: (ev) => {
            const e = ev as { state?: string; tool?: string };
            if (e.state === "started" && e.tool) setOut((p) => `${p}\n[🔧 ${e.tool}]\n`);
          },
          // The AI may have changed back-end data via a tool — refresh the active grid.
          onDone: () => {
            if (activeResource) invalidate({ resource: activeResource, invalidates: ["list"] });
          },
          onError: (e) => setOut((p) => `${p}\n[오류] ${e}`),
        },
        "client:main",
        workspaceContext,
      );
    } catch (e) {
      setOut(`[오류] ${(e as Error).message}`);
    } finally {
      setThinking("");
      setBusy(false);
    }
  }

  return { out, thinking, busy, send };
}

export interface GatewayStatus {
  status: string;
  check: () => Promise<void>;
}

// Live connection status line via miniapp.ping (version/model).
export function useGatewayStatus(cfg: GatewayConfig, initial = "미연결"): GatewayStatus {
  const [status, setStatus] = useState(initial);
  async function check() {
    setStatus("확인 중…");
    try {
      const r = await ping(cfg);
      setStatus(r.ok ? `연결됨 · v${r.version ?? "?"}` : "ping 실패");
    } catch (e) {
      setStatus(`오류: ${(e as Error).message}`);
    }
  }
  return { status, check };
}

export interface EventsState {
  events: ProactiveEvent[];
  status: string;
  dismiss: (id: string) => void;
}

// Subscribes to the proactive event stream while connected, keeping the most
// recent events. Aborts cleanly on unmount or config change.
export function useEvents(cfg: GatewayConfig, connected: boolean): EventsState {
  const [events, setEvents] = useState<ProactiveEvent[]>([]);
  const [status, setStatus] = useState("");

  useEffect(() => {
    if (!connected) {
      setStatus("");
      return;
    }
    const controller = new AbortController();
    setStatus("연결 중…");
    subscribeEvents(
      cfg,
      {
        onOpen: () => setStatus("수신 중"),
        onEvent: (ev) => setEvents((prev) => [ev, ...prev].slice(0, 50)),
        onError: (e) => setStatus(`오류: ${e}`),
      },
      controller.signal,
    ).catch((e) => {
      if (!controller.signal.aborted) setStatus(`오류: ${(e as Error).message}`);
    });
    return () => controller.abort();
    // Re-subscribe only when the connection identity changes, not on every cfg ref.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [cfg.url, cfg.token, connected]);

  const dismiss = (id: string) => setEvents((prev) => prev.filter((e) => e.id !== id));
  return { events, status, dismiss };
}
