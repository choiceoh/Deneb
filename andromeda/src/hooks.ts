// Reusable stateful hooks: the AI chat stream machine, gateway health check, and
// the proactive events subscription.
import { useEffect, useRef, useState } from "react";
import { useInvalidate } from "@refinedev/core";
import { clearCachedResource } from "./cachedList";
import { type ChatToolEvent, type GatewayConfig, callRpc, chatStream, ping } from "./gateway";
import { type ProactiveEvent, subscribeEvents } from "./events";
import { relatedResourcesForResource, relatedResourcesForTools } from "./resourceRefresh";
import { appendTextPart, chatTurnId, upsertToolPart } from "./chatParts";

// An assistant reply is an ordered list of text segments and tool chips, so a
// tool call rendered mid-reply keeps its place in the prose (text → tool → text).
export interface TextPart {
  kind: "text";
  text: string;
}
export interface ToolPart {
  kind: "tool";
  id: string;
  tool: string;
  state: string; // "started" | "completed"
  detail?: string;
  isError?: boolean;
}
export type AssistantPart = TextPart | ToolPart;

export interface ChatTurn {
  id: string;
  role: "user" | "assistant";
  // user: the message as typed; assistant: the accumulated plain text (used for
  // copy, regenerate, and as the canonical body for transcript-loaded turns).
  text: string;
  parts?: AssistantPart[]; // assistant turns only; live-streamed segments
  status: "done" | "streaming" | "error" | "stopped";
  model?: string;
}

export interface SendOpts {
  workspaceContext?: string;
  activeResource?: string;
  model?: string;
  sessionKey?: string;
  caption?: string;
}

export interface ChatState {
  thinking: string;
  busy: boolean;
  turns: ChatTurn[];
  send: (message: string, opts?: SendOpts) => Promise<void>;
  capture: (file: { name: string; mimeType: string; base64: string }, opts?: SendOpts) => Promise<void>;
  stop: () => void;
  regenerate: () => void;
  clear: () => void;
  setTurns: (turns: ChatTurn[]) => void;
}

// Drives one Deneb chat/stream turn: streams delta into text parts, tool frames
// into inline chips, supports Stop (abort) and Regenerate, and on done
// invalidates the active resource so AI-driven data changes show in the grid.
export function useChat(cfg: GatewayConfig): ChatState {
  const [thinking, setThinking] = useState("");
  const [busy, setBusy] = useState(false);
  const [turns, setTurns] = useState<ChatTurn[]>([]);
  const invalidate = useInvalidate();
  // The in-flight turn's abort handle (for Stop) and the last send args (for
  // Regenerate). Refs, not state — changing them must not re-render.
  const abortRef = useRef<AbortController | null>(null);
  const lastSendRef = useRef<{ message: string; opts: SendOpts } | null>(null);

  function clear() {
    if (busy) return;
    setThinking("");
    setTurns([]);
    lastSendRef.current = null;
  }

  function stop() {
    abortRef.current?.abort();
  }

  function regenerate() {
    const last = lastSendRef.current;
    if (!last || busy) return;
    // Replace the previous answer: drop the trailing assistant+user pair, then
    // re-run the same message (send re-appends both).
    setTurns((prev) => {
      const copy = [...prev];
      if (copy.at(-1)?.role === "assistant") copy.pop();
      if (copy.at(-1)?.role === "user") copy.pop();
      return copy;
    });
    void send(last.message, last.opts);
  }

  async function send(message: string, opts: SendOpts = {}) {
    const msg = message.trim();
    if (!msg || busy) return;
    lastSendRef.current = { message: msg, opts };
    const assistantId = chatTurnId();
    setThinking("");
    setTurns((prev) => [
      ...prev,
      { id: chatTurnId(), role: "user", text: msg, status: "done" },
      { id: assistantId, role: "assistant", text: "", parts: [], status: "streaming", model: opts.model },
    ]);
    setBusy(true);
    const controller = new AbortController();
    abortRef.current = controller;
    const seenTools = new Set<string>();
    let failed = false;
    let stopped = false;

    const patch = (update: (turn: ChatTurn) => ChatTurn) => {
      setTurns((prev) => prev.map((turn) => (turn.id === assistantId ? update(turn) : turn)));
    };
    // Append streamed text / upsert a tool chip on the in-flight assistant turn —
    // pure reducers (chatParts) threaded through patch().
    const appendText = (t: string) => patch((turn) => appendTextPart(turn, t));
    const upsertTool = (ev: ChatToolEvent) => patch((turn) => upsertToolPart(turn, ev));

    try {
      await chatStream(
        cfg,
        msg,
        {
          onThinking: (preview) => setThinking(preview.slice(0, 120)),
          onDelta: (t) => {
            setThinking("");
            appendText(t);
          },
          onTool: (ev) => {
            setThinking("");
            if (!ev.isError && ev.tool) seenTools.add(ev.tool);
            upsertTool(ev);
          },
          // The AI may have changed back-end data via a tool — refresh the active grid.
          onDone: (final) => {
            patch((turn) => {
              const hasText = (turn.parts ?? []).some((p) => p.kind === "text" && p.text.trim());
              const parts = hasText ? turn.parts : [...(turn.parts ?? []), { kind: "text" as const, text: final.text }];
              return {
                ...turn,
                parts,
                text: turn.text || final.text,
                model: final.model ?? turn.model,
                status: "done",
              };
            });
            const resources =
              seenTools.size > 0
                ? relatedResourcesForTools(seenTools, opts.activeResource)
                : relatedResourcesForResource(opts.activeResource);
            for (const resource of resources) {
              if (seenTools.size > 0) clearCachedResource(resource);
              invalidate({ resource, invalidates: ["list"] });
            }
          },
          onError: (e) => {
            failed = true;
            patch((turn) => ({
              ...turn,
              parts: [...(turn.parts ?? []), { kind: "text" as const, text: `\n[오류] ${e}` }],
              text: `${turn.text}\n[오류] ${e}`,
              status: "error",
            }));
          },
        },
        {
          sessionKey: opts.sessionKey,
          workspaceContext: opts.workspaceContext,
          model: opts.model,
          signal: controller.signal,
        },
      );
    } catch (e) {
      if (controller.signal.aborted) {
        stopped = true;
        patch((turn) => ({ ...turn, status: "stopped" }));
      } else {
        failed = true;
        const line = `[오류] ${(e as Error).message}`;
        patch((turn) => ({
          ...turn,
          parts: [...(turn.parts ?? []), { kind: "text" as const, text: turn.text ? `\n${line}` : line }],
          text: turn.text ? `${turn.text}\n${line}` : line,
          status: "error",
        }));
      }
    } finally {
      setThinking("");
      if (!failed && !stopped) patch((turn) => (turn.status === "streaming" ? { ...turn, status: "done" } : turn));
      abortRef.current = null;
      setBusy(false);
    }
  }

  // Attach a file to the chat: OCR an image, transcribe audio, or extract a document
  // via the gateway's miniapp.capture.* RPCs — each runs one agent turn over the
  // extracted text and lands in this session. Non-streaming (the RPC returns the
  // final text), so it appends a user label and fills the assistant reply.
  async function capture(file: { name: string; mimeType: string; base64: string }, opts: SendOpts = {}) {
    if (busy || !file.base64) return;
    const kind = file.mimeType.startsWith("image/")
      ? "image"
      : file.mimeType.startsWith("audio/")
        ? "audio"
        : "document";
    const caption = opts.caption?.trim();
    const baseLabel =
      kind === "image" ? `📷 이미지 — ${file.name}` : kind === "audio" ? `🎙️ 녹음 — ${file.name}` : `📄 ${file.name}`;
    const label = caption && kind !== "audio" ? `${baseLabel}\n${caption}` : baseLabel;
    const assistantId = chatTurnId();
    setThinking("");
    setTurns((prev) => [
      ...prev,
      { id: chatTurnId(), role: "user", text: label, status: "done" },
      { id: assistantId, role: "assistant", text: "", parts: [], status: "streaming", model: opts.model },
    ]);
    setBusy(true);
    const patch = (update: (turn: ChatTurn) => ChatTurn) =>
      setTurns((prev) => prev.map((turn) => (turn.id === assistantId ? update(turn) : turn)));
    try {
      const params: Record<string, unknown> = {
        [kind]: file.base64,
        mimeType: file.mimeType,
        sessionKey: opts.sessionKey,
      };
      if (kind === "document") params.filename = file.name;
      if (caption && kind !== "audio") params.caption = caption;
      const res = await callRpc<{ text?: string }>(cfg, `miniapp.capture.${kind}`, params);
      const text = res?.text?.trim() || "첨부에서 내용을 추출하지 못했거나 분석에 실패했습니다.";
      patch((turn) => ({ ...turn, parts: [{ kind: "text", text }], text, status: "done" }));
      for (const resource of ["workfeed", "wiki", "search"]) clearCachedResource(resource);
      invalidate({ resource: "workfeed", invalidates: ["list"] });
    } catch (e) {
      const line = `[오류] ${(e as Error)?.message ?? "첨부 실패"}`;
      patch((turn) => ({ ...turn, parts: [{ kind: "text", text: line }], text: line, status: "error" }));
    } finally {
      setBusy(false);
    }
  }

  return { thinking, busy, turns, send, capture, stop, regenerate, clear, setTurns };
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
