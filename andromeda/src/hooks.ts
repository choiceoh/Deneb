// Reusable stateful hooks: the AI chat stream machine, gateway health check, and
// the proactive events subscription.
import { useEffect, useRef, useState } from "react";
import { useInvalidate } from "@/crud";
import { clearCachedResource } from "./cachedList";
import {
  type ChatToolEvent,
  type GatewayConfig,
  callRpc,
  chatStream,
  composeChatMessage,
  ping,
  recoverTurnAnswer,
} from "./gateway";
import { type ProactiveEvent, subscribeEvents } from "./events";
import { errText } from "./format";
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
export type CaptureKind = "image" | "audio" | "document";
export interface AttachmentPart {
  kind: "attachment";
  id: string;
  filename: string;
  mimeType: string;
  captureKind: CaptureKind;
  caption?: string;
  text: string;
  state: "completed" | "error";
  isError?: boolean;
}
export type AssistantPart = TextPart | ToolPart | AttachmentPart;

export interface ChatTurn {
  id: string;
  role: "user" | "assistant";
  // user: the message as typed; assistant: the accumulated plain text (used for
  // copy, regenerate, and as the canonical body for transcript-loaded turns).
  text: string;
  imageUrl?: string; // user turns only — staged-image thumbnail (session-local)
  parts?: AssistantPart[]; // assistant turns only; live-streamed segments
  status: "done" | "streaming" | "error" | "stopped";
  model?: string;
  canRegenerate?: boolean;
  reasoning?: string; // assistant chain-of-thought → expandable reasoning block
}

export interface SendOpts {
  workspaceContext?: string;
  activeResource?: string;
  model?: string;
  sessionKey?: string;
  caption?: string;
  // Object URL of a staged image — rendered as the user-turn thumbnail so the
  // conversation shows the image, not just its filename (session-local).
  previewUrl?: string;
}

// ‹n/N› pager state for the newest answer's regenerate variants (live session
// only — mirrors the native client's #3870 semantics).
export interface VariantView {
  index: number;
  count: number;
}

export interface ChatState {
  thinking: string;
  busy: boolean;
  // True while the current busy turn can actually be aborted (a streamed send).
  // capture RPCs are not abortable, so composers show an honest non-stop state.
  stoppable: boolean;
  turns: ChatTurn[];
  send: (message: string, opts?: SendOpts) => Promise<void>;
  capture: (file: { name: string; mimeType: string; base64: string }, opts?: SendOpts) => Promise<void>;
  captureBatch: (
    files: { name: string; mimeType: string; base64: string; previewUrl?: string }[],
    opts?: SendOpts,
  ) => Promise<void>;
  stop: () => void;
  regenerate: () => void;
  // Edit the last user message and resend it (drops the trailing exchange).
  editResend: (message: string) => void;
  variants: VariantView | null;
  selectVariant: (dir: -1 | 1) => void;
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
  // Regenerate variants of the newest answer (#3870 parity). prevAnswers holds
  // the stashed older versions; newestAnswer snapshots the live one while the
  // user pages back. All live-session-local: any fresh user send resets them.
  const prevAnswersRef = useRef<ChatTurn[]>([]);
  const newestAnswerRef = useRef<ChatTurn | null>(null);
  const keepVariantsRef = useRef(false);
  const [variants, setVariants] = useState<VariantView | null>(null);

  function resetVariants() {
    prevAnswersRef.current = [];
    newestAnswerRef.current = null;
    setVariants(null);
  }

  function clear() {
    if (busy) return;
    setThinking("");
    setTurns([]);
    lastSendRef.current = null;
    resetVariants();
  }

  function stop() {
    abortRef.current?.abort();
  }

  function regenerate() {
    const last = lastSendRef.current;
    if (!last || busy) return;
    // Replace the previous answer: drop the trailing assistant+user pair, then
    // re-run the same message (send re-appends both). The dropped answer is
    // stashed so ‹n/N› can page back to it. Stash from the closure snapshot —
    // NOT inside the setTurns updater (updaters run deferred and must stay pure,
    // and send()'s finally reads the ref before an updater would have run).
    const copy = [...turns];
    const tail = copy.at(-1);
    if (tail?.role === "assistant") {
      if (tail.status !== "streaming") prevAnswersRef.current = [...prevAnswersRef.current, tail];
      copy.pop();
    }
    if (copy.at(-1)?.role === "user") copy.pop();
    setTurns(copy);
    newestAnswerRef.current = null;
    keepVariantsRef.current = true;
    void send(last.message, last.opts);
  }

  // Edit the last user message and resend: same trailing-exchange replacement as
  // regenerate, but with new text — so the old answer thread (and its variants)
  // no longer applies.
  function editResend(message: string) {
    const last = lastSendRef.current;
    const msg = message.trim();
    if (!last || busy || !msg) return;
    const copy = [...turns];
    if (copy.at(-1)?.role === "assistant") copy.pop();
    if (copy.at(-1)?.role === "user") copy.pop();
    setTurns(copy);
    void send(msg, last.opts);
  }

  // Page the newest answer across its regenerate variants. Index count-1 is the
  // live answer; anything below swaps a stashed snapshot into the tail slot.
  function selectVariant(dir: -1 | 1) {
    if (busy || !variants) return;
    const target = variants.index + dir;
    if (target < 0 || target >= variants.count) return;
    const tail = turns.at(-1);
    if (!tail || tail.role !== "assistant") return;
    if (variants.index === variants.count - 1) newestAnswerRef.current = tail;
    const replacement = target === variants.count - 1 ? newestAnswerRef.current : prevAnswersRef.current[target];
    if (!replacement) return;
    setTurns([...turns.slice(0, -1), replacement]);
    setVariants({ ...variants, index: target });
  }

  async function send(message: string, opts: SendOpts = {}) {
    const msg = message.trim();
    if (!msg || busy) return;
    if (keepVariantsRef.current) {
      keepVariantsRef.current = false;
    } else {
      resetVariants();
    }
    lastSendRef.current = { message: msg, opts };
    const assistantId = chatTurnId();
    setThinking("");
    setTurns((prev) => [
      ...prev,
      { id: chatTurnId(), role: "user", text: msg, status: "done" },
      {
        id: assistantId,
        role: "assistant",
        text: "",
        parts: [],
        status: "streaming",
        model: opts.model,
        canRegenerate: true,
      },
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
                reasoning: final.reasoning?.trim() ? final.reasoning : turn.reasoning,
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
        // The gateway detaches the run from the SSE connection, so a mid-turn
        // drop (sleep, network change) doesn't kill the turn — the server
        // finishes and persists the answer. Poll the transcript for it instead
        // of freezing on the streamed preamble; show "답변 이어받는 중…" so a
        // reconnect (minutes on a tool-heavy turn) never looks stalled.
        setThinking("답변 이어받는 중…");
        let recovered: string | null;
        try {
          recovered = await recoverTurnAnswer(
            cfg,
            opts.sessionKey ?? "client:main",
            composeChatMessage(msg, opts.workspaceContext),
            {
              signal: controller.signal,
              onStillRunning: () => setThinking("답변 이어받는 중…"),
            },
          );
        } catch {
          recovered = null;
        }
        if (recovered) {
          const answer = recovered;
          patch((turn) => ({
            ...turn,
            parts: [{ kind: "text" as const, text: answer }],
            text: answer,
            status: "done",
          }));
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
      }
    } finally {
      setThinking("");
      if (!failed && !stopped) patch((turn) => (turn.status === "streaming" ? { ...turn, status: "done" } : turn));
      abortRef.current = null;
      setBusy(false);
      // Regenerated answer settled → surface the ‹n/N› pager (newest selected).
      const stashed = prevAnswersRef.current.length;
      setVariants(stashed > 0 ? { index: stashed, count: stashed + 1 } : null);
    }
  }

  // Attach a file to the chat: OCR an image, transcribe audio, or extract a document
  // via the gateway's miniapp.capture.* RPCs — each runs one agent turn over the
  // extracted text and lands in this session. Non-streaming (the RPC returns the
  // final text), so it appends a user label and fills the assistant reply.
  async function capture(file: { name: string; mimeType: string; base64: string }, opts: SendOpts = {}) {
    if (busy || !file.base64) return;
    resetVariants(); // a new exchange begins — the pager belonged to the old tail
    const mimeType = file.mimeType || "application/octet-stream";
    const kind: CaptureKind = mimeType.startsWith("image/")
      ? "image"
      : mimeType.startsWith("audio/")
        ? "audio"
        : "document";
    const caption = opts.caption?.trim();
    const kindLabel = kind === "image" ? "이미지" : kind === "audio" ? "녹음" : "문서";
    const baseLabel = `${kindLabel} 첨부: ${file.name}`;
    const label = caption && kind !== "audio" ? `${baseLabel}\n${caption}` : baseLabel;
    const assistantId = chatTurnId();
    const attachmentId = chatTurnId();
    setThinking("");
    setTurns((prev) => [
      ...prev,
      { id: chatTurnId(), role: "user", text: label, imageUrl: opts.previewUrl, status: "done" },
      {
        id: assistantId,
        role: "assistant",
        text: "",
        parts: [],
        status: "streaming",
        model: opts.model,
        canRegenerate: false,
      },
    ]);
    setBusy(true);
    const patch = (update: (turn: ChatTurn) => ChatTurn) =>
      setTurns((prev) => prev.map((turn) => (turn.id === assistantId ? update(turn) : turn)));
    try {
      const params: Record<string, unknown> = {
        [kind]: file.base64,
        mimeType,
        sessionKey: opts.sessionKey,
      };
      if (kind === "document") params.filename = file.name;
      if (caption && kind !== "audio") params.caption = caption;
      const res = await callRpc<{ text?: string }>(cfg, `miniapp.capture.${kind}`, params);
      const text = res?.text?.trim() || "첨부에서 내용을 추출하지 못했거나 분석에 실패했습니다.";
      patch((turn) => ({
        ...turn,
        parts: [
          {
            kind: "attachment",
            id: attachmentId,
            filename: file.name,
            mimeType,
            captureKind: kind,
            caption: caption && kind !== "audio" ? caption : undefined,
            text,
            state: "completed",
          },
        ],
        text,
        status: "done",
      }));
      for (const resource of ["workfeed", "wiki", "search"]) clearCachedResource(resource);
      invalidate({ resource: "workfeed", invalidates: ["list"] });
    } catch (e) {
      const line = `[오류] ${(e as Error)?.message ?? "첨부 실패"}`;
      patch((turn) => ({
        ...turn,
        parts: [
          {
            kind: "attachment",
            id: attachmentId,
            filename: file.name,
            mimeType,
            captureKind: kind,
            caption: caption && kind !== "audio" ? caption : undefined,
            text: line,
            state: "error",
            isError: true,
          },
        ],
        text: line,
        status: "error",
      }));
    } finally {
      setBusy(false);
    }
  }

  // Attach N files in ONE turn. The gateway (miniapp.capture.batch) materializes
  // each file to the agent-readable memory store and runs a single agent turn over
  // a pointer list — the agent reads whichever files it needs with read/office and
  // cross-references them. So a batch is one user turn (the file list) + one
  // assistant turn (the analysis), instead of one capture turn per file.
  async function captureBatch(
    files: { name: string; mimeType: string; base64: string; previewUrl?: string }[],
    opts: SendOpts = {},
  ) {
    const usable = files.filter((f) => f.base64);
    if (busy || usable.length === 0) return;
    resetVariants();
    const caption = opts.caption?.trim() ?? "";
    const label =
      `📎 첨부 ${usable.length}개\n` + usable.map((f) => `• ${f.name}`).join("\n") + (caption ? `\n\n${caption}` : "");
    const firstPreview = usable.find((f) => f.previewUrl)?.previewUrl;
    const assistantId = chatTurnId();
    setThinking("");
    setTurns((prev) => [
      ...prev,
      { id: chatTurnId(), role: "user", text: label, imageUrl: firstPreview, status: "done" },
      {
        id: assistantId,
        role: "assistant",
        text: "",
        parts: [],
        status: "streaming",
        model: opts.model,
        canRegenerate: false,
      },
    ]);
    setBusy(true);
    const patch = (update: (turn: ChatTurn) => ChatTurn) =>
      setTurns((prev) => prev.map((turn) => (turn.id === assistantId ? update(turn) : turn)));
    try {
      const params: Record<string, unknown> = {
        files: usable.map((f) => ({ data: f.base64, mimeType: f.mimeType, filename: f.name })),
        sessionKey: opts.sessionKey,
      };
      if (caption) params.caption = caption;
      const res = await callRpc<{ text?: string }>(cfg, "miniapp.capture.batch", params);
      const text = res?.text?.trim() || "첨부에서 내용을 추출하지 못했거나 분석에 실패했습니다.";
      patch((turn) => ({ ...turn, parts: [{ kind: "text" as const, text }], text, status: "done" }));
      for (const resource of ["workfeed", "wiki", "search"]) clearCachedResource(resource);
      invalidate({ resource: "workfeed", invalidates: ["list"] });
    } catch (e) {
      const line = `[오류] ${(e as Error)?.message ?? "첨부 실패"}`;
      patch((turn) => ({ ...turn, parts: [{ kind: "text" as const, text: line }], text: line, status: "error" }));
    } finally {
      setBusy(false);
    }
  }

  // External setTurns (session switch / transcript load) starts a fresh thread —
  // stashed variants belong to the previous one.
  function setTurnsExternal(next: ChatTurn[]) {
    resetVariants();
    setTurns(next);
  }

  return {
    thinking,
    busy,
    stoppable: busy && abortRef.current !== null,
    turns,
    send,
    capture,
    captureBatch,
    stop,
    regenerate,
    editResend,
    variants,
    selectVariant,
    clear,
    setTurns: setTurnsExternal,
  };
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
  clearAll: () => void;
}

export const EVENTS_RETRY_BASE_MS = 1_000;
export const EVENTS_RETRY_MAX_MS = 30_000;

// Consecutive pre-connect failures back off exponentially. A successful HTTP/SSE
// open resets the attempt counter, so a later disconnect starts promptly again.
export function eventsRetryDelay(attempt: number): number {
  const exponent = Math.max(0, Math.min(Math.floor(attempt), 30));
  return Math.min(EVENTS_RETRY_MAX_MS, EVENTS_RETRY_BASE_MS * 2 ** exponent);
}

// Abort-aware sleep for the reconnect loop. Resolving false (rather than
// rejecting) keeps an ordinary unmount/config switch out of error handling.
function waitForEventsRetry(ms: number, signal: AbortSignal): Promise<boolean> {
  if (signal.aborted) return Promise.resolve(false);
  return new Promise((resolve) => {
    const finish = (elapsed: boolean) => {
      clearTimeout(timer);
      signal.removeEventListener("abort", onAbort);
      resolve(elapsed);
    };
    const onAbort = () => finish(false);
    const timer = setTimeout(() => finish(true), ms);
    signal.addEventListener("abort", onAbort, { once: true });
  });
}

function retrySeconds(ms: number): string {
  return `${Math.max(1, Math.ceil(ms / 1_000))}초`;
}

// Subscribes to the proactive event stream while connected, keeping the most
// recent events. One abortable loop owns both the active stream and its retry
// timer, preventing duplicate subscriptions across reconnects/config changes.
export function useEvents(
  cfg: GatewayConfig,
  connected: boolean,
  // Optional frame interceptor, called before an event is listed: return a
  // (possibly rewritten) event to show it as a nudge, or null to swallow it.
  // ProactivePanel uses this to execute gateway-pushed workspace commands and
  // replace them with a human-readable "화면 조정" notice — hooks.ts stays free
  // of the command grammar (commands.ts imports the pane registry).
  intercept?: (ev: ProactiveEvent) => ProactiveEvent | null,
): EventsState {
  const [events, setEvents] = useState<ProactiveEvent[]>([]);
  const [status, setStatus] = useState("");
  const invalidate = useInvalidate();
  // Latest interceptor without re-subscribing the SSE loop on each render
  // (updated post-commit — refs must not be written during render).
  const interceptRef = useRef(intercept);
  useEffect(() => {
    interceptRef.current = intercept;
  });

  // Reflect connection flips in the status line during render (react.dev
  // "adjusting state when props change") — the effect below only owns the
  // subscription; its async callbacks keep updating the status afterwards.
  const connKey = connected ? `on|${cfg.url}|${cfg.token}` : "off";
  const [prevConnKey, setPrevConnKey] = useState("");
  if (prevConnKey !== connKey) {
    setPrevConnKey(connKey);
    setStatus(connected ? "연결 중…" : "");
  }

  useEffect(() => {
    if (!connected) return;
    const controller = new AbortController();
    const { signal } = controller;

    const run = async () => {
      let attempt = 0;
      while (!signal.aborted) {
        if (attempt > 0) setStatus("재연결 중…");
        let failure: unknown;
        try {
          await subscribeEvents(
            cfg,
            {
              onOpen: () => {
                if (signal.aborted) return;
                attempt = 0;
                setStatus("수신 중");
              },
              // The gateway's push payload is title+body only (no ts), so stamp the
              // arrival time here — the panel renders it as a relative "N분 전".
              onEvent: (rawEv) => {
                if (signal.aborted) return;
                const ev = interceptRef.current ? interceptRef.current(rawEv) : rawEv;
                if (!ev) return;
                setEvents((prev) => [{ ...ev, ts: ev.ts ?? Date.now() }, ...prev].slice(0, 50));
                // A nudge means its backing data just changed server-side. Refresh the
                // affected resource list(s) now so the feed/grid updates with the
                // notification instead of waiting out the 60s list cache — the instant
                // counterpart to useNativeSync's catch-up poll (sync.ts). The event
                // kind is a pane key (workfeed/mail/calendar/…); non-resource kinds
                // (push fallback, errors) map to nothing and no-op.
                for (const resource of relatedResourcesForResource(ev.kind)) {
                  clearCachedResource(resource);
                  invalidate({ resource, invalidates: ["list"] });
                }
              },
              onError: (e) => {
                if (!signal.aborted) setStatus(`오류: ${e}`);
              },
            },
            signal,
          );
          if (signal.aborted) return;
          failure = new Error("이벤트 스트림이 종료되었습니다.");
        } catch (e) {
          if (signal.aborted) return;
          failure = e;
        }

        const delay = eventsRetryDelay(attempt);
        attempt += 1;
        setStatus(`오류: ${errText(failure)} · ${retrySeconds(delay)} 후 재연결…`);
        if (!(await waitForEventsRetry(delay, signal))) return;
      }
    };

    void run();
    return () => controller.abort();
    // Re-subscribe only when the connection identity changes, not on every cfg ref.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [cfg.url, cfg.token, connected]);

  const dismiss = (id: string) => setEvents((prev) => prev.filter((e) => e.id !== id));
  const clearAll = () => setEvents([]);
  return { events, status, dismiss, clearAll };
}
