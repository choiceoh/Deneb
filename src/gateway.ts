// Deneb gateway client — the data layer the rest of Andromeda builds on.
//
// Phase 0 (this file): raw RPC envelope + token auth + ping + chat SSE stream.
// Phase 1: wrap callRpc() in a Refine data provider so resources (mail, calendar,
// todo, memory …) flow into grids/forms automatically.

const TOKEN_HEADER = "X-Deneb-Client-Token";
const STORAGE_KEY = "andromeda.gateway";

export interface GatewayConfig {
  url: string; // e.g. http://100.x.x.x:18789  (Tailscale/LAN)
  token: string; // hex64 client token (~/.deneb/client_token)
}

export function loadConfig(): GatewayConfig {
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    if (raw) return JSON.parse(raw) as GatewayConfig;
  } catch {
    /* ignore corrupt storage */
  }
  return { url: "", token: "" };
}

export function saveConfig(cfg: GatewayConfig): void {
  localStorage.setItem(STORAGE_KEY, JSON.stringify(cfg));
}

interface RpcRequest {
  id: string;
  method: string;
  params: Record<string, unknown>;
}

interface RpcEnvelope<T> {
  ok: boolean;
  payload?: T;
  error?: { code: string; message: string };
}

const base = (url: string) => url.replace(/\/$/, "");

// One JSON-RPC call against POST /api/v1/miniapp/rpc.
export async function callRpc<T>(
  cfg: GatewayConfig,
  method: string,
  params: Record<string, unknown> = {},
): Promise<T> {
  const body: RpcRequest = { id: crypto.randomUUID(), method, params };
  const res = await fetch(`${base(cfg.url)}/api/v1/miniapp/rpc`, {
    method: "POST",
    headers: { "Content-Type": "application/json", [TOKEN_HEADER]: cfg.token },
    body: JSON.stringify(body),
  });
  if (!res.ok) throw new Error(`RPC ${method}: HTTP ${res.status}`);
  const env = (await res.json()) as RpcEnvelope<T>;
  if (!env.ok) throw new Error(env.error?.message ?? `RPC ${method} failed`);
  return env.payload as T;
}

export interface PingResult {
  ok: boolean;
  version?: string;
  tsMs?: number;
  model?: string;
}

export const ping = (cfg: GatewayConfig) => callRpc<PingResult>(cfg, "miniapp.ping");

// --- Chat streaming over POST /api/v1/miniapp/chat/stream (SSE) ---

export interface ChatHandlers {
  onDelta?: (text: string) => void;
  onTool?: (ev: unknown) => void;
  onThinking?: (preview: string) => void;
  onDone?: (final: { text: string; model?: string }) => void;
  onError?: (err: string) => void;
}

// Minimal SSE parser matching the gateway frame format:
//   event: delta|tool|thinking|done|error
//   data:  {...}
export async function chatStream(
  cfg: GatewayConfig,
  message: string,
  handlers: ChatHandlers,
  sessionKey = "client:main",
  workspaceContext?: string,
): Promise<void> {
  // Inject the current work-area content as a USER-message prefix (never system),
  // so Deneb's AI can read what you're working on.
  //
  // This is SEMANTIC TEXT, not a screenshot — the work area serializes itself to
  // text (a markdown doc, mail header+body, a data grid as a table, a chart's
  // underlying numbers). So ANY LLM reads it with no vision model needed: more
  // accurate (no pixel guessing), cheaper (no vision tokens), and cache-friendly.
  // Vision stays a last resort for true images / scanned PDFs (Deneb's
  // capture.image + OCR path). In Phase 1 each pane exposes serializeForAI().
  //
  // User-turn placement keeps the gateway's vLLM prefix cache (APC) intact —
  // per-turn context belongs in the trailing user message, not the cached system
  // prompt (see Deneb prompt-cache §1.5).
  const composed = workspaceContext?.trim()
    ? `[작업 영역 — 현재 내용]\n${workspaceContext}\n\n[요청]\n${message}`
    : message;
  const res = await fetch(`${base(cfg.url)}/api/v1/miniapp/chat/stream`, {
    method: "POST",
    headers: { "Content-Type": "application/json", [TOKEN_HEADER]: cfg.token },
    body: JSON.stringify({ message: composed, sessionKey }),
  });
  if (!res.ok) throw new Error(`chat stream: HTTP ${res.status}`);
  if (!res.body) throw new Error("chat stream: empty response body");

  const reader = res.body.getReader();
  const decoder = new TextDecoder();
  let buffer = "";
  let event = "";

  for (;;) {
    const { value, done } = await reader.read();
    if (done) break;
    buffer += decoder.decode(value, { stream: true });
    const lines = buffer.split("\n");
    buffer = lines.pop() ?? ""; // keep the trailing partial line
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
        switch (event) {
          case "delta":
            if (typeof obj.delta === "string") handlers.onDelta?.(obj.delta);
            break;
          case "tool":
            handlers.onTool?.(obj);
            break;
          case "thinking":
            if (typeof obj.preview === "string") handlers.onThinking?.(obj.preview);
            break;
          case "done":
            handlers.onDone?.({
              text: typeof obj.text === "string" ? obj.text : "",
              model: typeof obj.model === "string" ? obj.model : undefined,
            });
            break;
          case "error":
            handlers.onError?.(typeof obj.error === "string" ? obj.error : "unknown error");
            break;
        }
      } catch {
        /* ignore malformed frame */
      }
    }
  }
}
