// Deneb gateway client — the data layer the rest of Andromeda builds on.
//
// Phase 0 (this file): raw RPC envelope + token auth + ping + chat SSE stream.
// Phase 1: wrap callRpc() in a Refine data provider so resources (mail, calendar,
// todo, memory …) flow into grids/forms automatically.
import { readSSE } from "./sse";
import { log } from "./log";

const rpcLog = log.child("rpc");
const chatLog = log.child("chat");

export const TOKEN_HEADER = "X-Deneb-Client-Token";
const STORAGE_KEY = "andromeda.gateway";

export interface GatewayConfig {
  url: string; // e.g. http://100.x.x.x:18789  (Tailscale/LAN)
  token: string; // hex64 client token (~/.deneb/client_token)
}

// Build-time gateway defaults (VITE_GATEWAY_URL / VITE_GATEWAY_TOKEN), so a
// configured deployment auto-connects without manual entry. Prefer the desktop
// keychain / ~/.deneb/client_token for the token over committing it.
function configFromEnv(): Partial<GatewayConfig> {
  return { url: import.meta.env.VITE_GATEWAY_URL, token: import.meta.env.VITE_GATEWAY_TOKEN };
}

// Resolve the initial config: a saved (sidebar-entered) value wins per field,
// falling back to env defaults. Desktop also hydrates the token from the OS
// keychain / token file at startup (see tauri.readDesktopToken + App bootstrap).
export function loadConfig(): GatewayConfig {
  let saved: Partial<GatewayConfig> = {};
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    if (raw) saved = JSON.parse(raw) as GatewayConfig;
  } catch {
    /* ignore corrupt storage */
  }
  const env = configFromEnv();
  return { url: saved.url || env.url || "", token: saved.token || env.token || "" };
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

export const base = (url: string) => url.replace(/\/$/, "");

// One JSON-RPC call against POST /api/v1/miniapp/rpc.
export async function callRpc<T>(cfg: GatewayConfig, method: string, params: Record<string, unknown> = {}): Promise<T> {
  const body: RpcRequest = { id: crypto.randomUUID(), method, params };
  rpcLog.debug(`→ ${method}`, params);
  try {
    const res = await fetch(`${base(cfg.url)}/api/v1/miniapp/rpc`, {
      method: "POST",
      headers: { "Content-Type": "application/json", [TOKEN_HEADER]: cfg.token },
      body: JSON.stringify(body),
    });
    if (!res.ok) throw new Error(`RPC ${method}: HTTP ${res.status}`);
    const env = (await res.json()) as RpcEnvelope<T>;
    if (!env.ok) throw new Error(env.error?.message ?? `RPC ${method} failed`);
    rpcLog.debug(`← ${method}`);
    return env.payload as T;
  } catch (e) {
    rpcLog.error(`✗ ${method}: ${(e as Error).message}`);
    throw e;
  }
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
  chatLog.debug(`→ stream (session ${sessionKey}, +context ${Boolean(workspaceContext?.trim())})`);
  const res = await fetch(`${base(cfg.url)}/api/v1/miniapp/chat/stream`, {
    method: "POST",
    headers: { "Content-Type": "application/json", [TOKEN_HEADER]: cfg.token },
    body: JSON.stringify({ message: composed, sessionKey }),
  });
  if (!res.ok) {
    chatLog.error(`✗ stream: HTTP ${res.status}`);
    throw new Error(`chat stream: HTTP ${res.status}`);
  }
  if (!res.body) throw new Error("chat stream: empty response body");

  await readSSE(res.body, ({ event, data }) => {
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
  });
}
