// Deneb gateway client — the data layer the rest of Andromeda builds on.
//
// Phase 0 (this file): raw RPC envelope + token auth + ping + chat SSE stream.
// Phase 1: wrap callRpc() in a Refine data provider so resources (mail, calendar,
// todo, memory …) flow into grids/forms automatically.
import { asBool, asStr } from "./format";
import { readJsonSSE } from "./sse";
import { log } from "./log";
import { getJSON, setJSON } from "./storage";
import { isTauri } from "./tauri";

const rpcLog = log.child("rpc");
const chatLog = log.child("chat");

// In a packaged desktop app the webview's fetch is subject to CORS *and* macOS
// WKWebView's ATS (which blocks plain-HTTP gateways) — so a perfectly reachable
// gateway still fails from the UI. Route gateway calls through Tauri's native HTTP
// plugin (Rust reqwest), which bypasses both. On web/dev we fall back to the
// browser fetch. The import is dynamic so the web bundle never pulls the plugin.
let nativeFetch: typeof globalThis.fetch | null = null;
export async function gatewayFetch(input: string, init?: RequestInit): Promise<Response> {
  if (isTauri()) {
    if (!nativeFetch) {
      const mod = await import("@tauri-apps/plugin-http");
      nativeFetch = mod.fetch as typeof globalThis.fetch;
    }
    return nativeFetch(input, init);
  }
  return globalThis.fetch(input, init);
}

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
  const saved = getJSON<Partial<GatewayConfig>>(STORAGE_KEY) ?? {};
  const env = configFromEnv();
  return { url: saved.url || env.url || "", token: saved.token || env.token || "" };
}

export function saveConfig(cfg: GatewayConfig): void {
  setJSON(STORAGE_KEY, cfg);
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
    const res = await gatewayFetch(`${base(cfg.url)}/api/v1/miniapp/rpc`, {
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

// --- Models (miniapp.models.*) — the AI panel's model/service picker ---

// One selectable model. Mirrors handlerminiapp.ModelOption (models.go).
export interface ModelOption {
  id: string;
  label: string;
  provider?: string;
  display?: string;
  health?: string;
  current?: boolean;
  custom?: boolean;
  deletable?: boolean;
  unhealthy?: boolean;
  note?: string; // server-rendered Korean tuner stat line
}

export interface ModelSection {
  title: string;
  models: ModelOption[];
}

export interface RoleModel {
  role: string; // main | lightweight | fallback | …
  model: string;
}

// miniapp.models.list payload — active model, per-role bindings, grouped sections.
export interface ModelsList {
  current: string;
  roles: RoleModel[];
  sections: ModelSection[];
  advisories?: string[];
  mainHasVision?: boolean;
}

export const listModels = (cfg: GatewayConfig) => callRpc<ModelsList>(cfg, "miniapp.models.list");

// Bind a model to a role (default main) — persists the picker choice gateway-side.
export const setModel = (cfg: GatewayConfig, id: string, role = "main") =>
  callRpc<{ ok: boolean; role: string; current: string }>(cfg, "miniapp.models.set", { id, role });

// --- Sessions (miniapp.sessions.*) — conversation history drawer ---

// One recent conversation row. Mirrors handlerminiapp.sessionRowOut (sessions.go).
export interface SessionRow {
  key: string;
  kind?: string;
  status?: string;
  channel?: string;
  model?: string;
  label?: string;
  updatedAtMs?: number;
  startedAtMs?: number;
  runtimeMs?: number;
  totalTokens?: number;
}

// One transcript message. Mirrors handlerminiapp.transcriptMsgOut (sessions.go).
export interface TranscriptMsg {
  id?: string;
  role: string; // user | assistant | system | tool
  content: string;
  timestampMs?: number;
}

export const recentSessions = (cfg: GatewayConfig, limit = 20) =>
  callRpc<{ sessions: SessionRow[]; count: number }>(cfg, "miniapp.sessions.recent", { limit }).then(
    (r) => r.sessions ?? [],
  );

export const sessionTranscript = (cfg: GatewayConfig, sessionKey: string, limit = 60) =>
  callRpc<{ sessionKey: string; messages: TranscriptMsg[]; total: number }>(cfg, "miniapp.sessions.transcript", {
    sessionKey,
    limit,
  }).then((r) => r.messages ?? []);

// Drop a dismissed conversation (the × in the session drawer). The gateway also
// deletes its transcript so the row can't resurrect on the next restart.
export const deleteSession = (cfg: GatewayConfig, sessionKey: string) =>
  callRpc<{ deleted: boolean }>(cfg, "miniapp.sessions.delete", { sessionKey }).then((r) => Boolean(r.deleted));

// --- Mail enrichment (miniapp.gmail.analyze / analysis_cached / sender_context / ask) ---

// A related project wiki page surfaced by mail analysis. Mirrors gmail_analyze.go ProjectRef.
export interface ProjectRef {
  path: string;
  title?: string;
  summary?: string;
}

// AI analysis of one message (analyze / analysis_cached). `analysis` is Markdown;
// `analysisQuality` is the importance tier; cached=false + empty analysis = a miss.
export interface MailAnalysis {
  id: string;
  analysis: string;
  relatedProjects?: ProjectRef[];
  cached: boolean;
  createdAt?: string;
  analysisQuality?: string;
  calendarProposalCount?: number;
  todoCount?: number;
}

export const cachedMailAnalysis = (cfg: GatewayConfig, id: string) =>
  callRpc<MailAnalysis>(cfg, "miniapp.gmail.analysis_cached", { id });

export const analyzeMail = (cfg: GatewayConfig, id: string, force = false) =>
  callRpc<MailAnalysis>(cfg, "miniapp.gmail.analyze", { id, force });

// Sender context card (sender_context): recent volume + hand-curated wiki pages.
export interface SenderRecent {
  count: number;
  lastReceivedAt?: string;
  windowDays: number;
  truncated?: boolean;
}
export interface SenderWikiHit {
  path: string;
  title?: string;
  summary?: string;
  category?: string;
}
export interface SenderContext {
  sender: string;
  email?: string;
  displayName?: string;
  recent?: SenderRecent;
  wikiHits?: SenderWikiHit[];
  wikiFacts?: string;
  notices?: string[];
}

export const senderContext = (cfg: GatewayConfig, sender: string) =>
  callRpc<SenderContext>(cfg, "miniapp.gmail.sender_context", { sender });

// One prior turn of mail Q&A — the client accumulates these so the gateway stays stateless.
export interface QATurn {
  q: string;
  a: string;
}

export const askMail = (cfg: GatewayConfig, id: string, question: string, history: QATurn[] = []) =>
  callRpc<{ answer: string }>(cfg, "miniapp.gmail.ask", { id, question, history }).then((r) => r.answer);

// --- Chat streaming over POST /api/v1/miniapp/chat/stream (SSE) ---

// One tool lifecycle frame, parsed from the gateway's `tool` SSE event
// (server_http_miniapp_stream.go toolStreamFrame). `started`→`completed` pairs
// share a toolUseId so the panel can flip a running chip to its result.
export interface ChatToolEvent {
  state: string; // "started" | "completed"
  tool: string;
  toolUseId: string;
  detail?: string;
  isError?: boolean;
}

export interface ChatHandlers {
  onDelta?: (text: string) => void;
  onTool?: (ev: ChatToolEvent) => void;
  onThinking?: (preview: string) => void;
  onDone?: (final: { text: string; model?: string; fellBack?: boolean }) => void;
  onError?: (err: string) => void;
}

export interface ChatStreamOpts {
  sessionKey?: string;
  workspaceContext?: string;
  // Per-turn model override (the picker's selection). Empty → gateway's main model.
  model?: string;
  // Aborts the in-flight turn when the user hits Stop.
  signal?: AbortSignal;
}

// Open an SSE stream against a miniapp endpoint: build the URL, inject the client
// token header, return the response body (throwing on a bad status / missing body).
// Shared by chatStream + subscribeEvents so the fetch-and-validate preamble lives once.
export async function streamFetch(
  cfg: GatewayConfig,
  path: string,
  init: RequestInit = {},
): Promise<ReadableStream<Uint8Array>> {
  const res = await gatewayFetch(`${base(cfg.url)}/api/v1/miniapp/${path}`, {
    ...init,
    headers: { [TOKEN_HEADER]: cfg.token, ...init.headers },
  });
  if (!res.ok) throw new Error(`${path}: HTTP ${res.status}`);
  if (!res.body) throw new Error(`${path}: empty response body`);
  return res.body;
}

// Minimal SSE parser matching the gateway frame format:
//   event: delta|tool|thinking|done|error
//   data:  {...}
export async function chatStream(
  cfg: GatewayConfig,
  message: string,
  handlers: ChatHandlers,
  opts: ChatStreamOpts = {},
): Promise<void> {
  const { sessionKey = "client:main", workspaceContext, model, signal } = opts;
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
  chatLog.debug(
    `→ stream (session ${sessionKey}, model ${model || "main"}, +context ${Boolean(workspaceContext?.trim())})`,
  );
  const body = await streamFetch(cfg, "chat/stream", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(model ? { message: composed, sessionKey, model } : { message: composed, sessionKey }),
    signal,
  });

  await readJsonSSE(body, (event, obj) => {
    switch (event) {
      case "delta": {
        const delta = asStr(obj.delta);
        if (delta !== undefined) handlers.onDelta?.(delta);
        break;
      }
      case "tool": {
        const tool = asStr(obj.tool);
        if (tool) {
          handlers.onTool?.({
            state: asStr(obj.state) ?? "",
            tool,
            toolUseId: asStr(obj.toolUseId) ?? "",
            detail: asStr(obj.detail),
            isError: asBool(obj.isError),
          });
        }
        break;
      }
      case "thinking": {
        const preview = asStr(obj.preview);
        if (preview !== undefined) handlers.onThinking?.(preview);
        break;
      }
      case "done":
        handlers.onDone?.({
          text: asStr(obj.text) ?? "",
          model: asStr(obj.model),
          fellBack: asBool(obj.fellBack),
        });
        break;
      case "error":
        handlers.onError?.(asStr(obj.error) ?? "unknown error");
        break;
    }
  });
}
