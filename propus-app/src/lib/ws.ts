// ═══ WebSocket client — connects to Propus gateway channel ═══

import type { ClientMessage, ServerMessage } from "./types";

export type ConnectionStatus = "disconnected" | "connecting" | "connected";
export type MessageHandler = (msg: ServerMessage) => void;
export type StatusHandler = (status: ConnectionStatus) => void;

const PING_INTERVAL_MS = 30_000;
const RECONNECT_DELAYS = [1000, 2000, 4000, 8000, 16000]; // 5 attempts with exponential backoff

export class PropusWebSocket {
  private ws: WebSocket | null = null;
  private pingTimer: ReturnType<typeof setInterval> | null = null;
  private onMessage: MessageHandler;
  private onStatus: StatusHandler;
  private url = "";
  private reconnectAttempt = 0;
  private intentionalClose = false;

  constructor(onMessage: MessageHandler, onStatus: StatusHandler) {
    this.onMessage = onMessage;
    this.onStatus = onStatus;
  }

  connect(url: string): void {
    this.url = url;
    this.intentionalClose = false;
    this.reconnectAttempt = 0;
    this.doConnect();
  }

  disconnect(): void {
    this.intentionalClose = true;
    this.cleanup();
    this.onStatus("disconnected");
  }

  send(msg: ClientMessage): void {
    if (this.ws?.readyState === WebSocket.OPEN) {
      this.ws.send(JSON.stringify(msg));
    }
  }

  get connected(): boolean {
    return this.ws?.readyState === WebSocket.OPEN;
  }

  reconnect(): void {
    this.intentionalClose = false;
    this.reconnectAttempt = 0;
    this.doConnect();
  }

  private doConnect(): void {
    this.cleanup();
    this.onStatus("connecting");

    // On reconnect, append ?resume=<connID> to reuse the server-side session.
    let connectUrl = this.url;
    const savedConn = loadSavedConnId();
    if (savedConn) {
      const sep = this.url.includes("?") ? "&" : "?";
      connectUrl = `${this.url}${sep}resume=${encodeURIComponent(savedConn)}`;
    }

    try {
      this.ws = new WebSocket(connectUrl);
    } catch {
      this.onStatus("disconnected");
      return;
    }

    this.ws.onopen = () => {
      this.reconnectAttempt = 0;
      this.onStatus("connected");
      this.startPing();
    };

    this.ws.onmessage = (event) => {
      try {
        const msg = JSON.parse(event.data) as ServerMessage;
        this.onMessage(msg);
      } catch {
        // ignore malformed messages
      }
    };

    this.ws.onclose = () => {
      this.stopPing();
      if (!this.intentionalClose) {
        this.onStatus("disconnected");
        this.tryReconnect();
      }
    };

    this.ws.onerror = () => {
      // onclose will fire after onerror
    };
  }

  private tryReconnect(): void {
    if (this.reconnectAttempt >= RECONNECT_DELAYS.length) return;

    const delay = RECONNECT_DELAYS[this.reconnectAttempt]!;
    this.reconnectAttempt++;

    setTimeout(() => {
      if (!this.intentionalClose) {
        this.doConnect();
      }
    }, delay);
  }

  private startPing(): void {
    this.stopPing();
    this.pingTimer = setInterval(() => {
      this.send({ type: "Ping" });
    }, PING_INTERVAL_MS);
  }

  private stopPing(): void {
    if (this.pingTimer) {
      clearInterval(this.pingTimer);
      this.pingTimer = null;
    }
  }

  private cleanup(): void {
    this.stopPing();
    if (this.ws) {
      this.ws.onopen = null;
      this.ws.onmessage = null;
      this.ws.onclose = null;
      this.ws.onerror = null;
      if (
        this.ws.readyState === WebSocket.OPEN ||
        this.ws.readyState === WebSocket.CONNECTING
      ) {
        this.ws.close();
      }
      this.ws = null;
    }
  }
}

// --- localStorage helpers for saved server URL ---

const STORAGE_KEY = "propus_server_url";
const CONN_ID_KEY = "propus_conn_id";

export function loadSavedUrl(): string | null {
  try {
    return localStorage.getItem(STORAGE_KEY);
  } catch {
    return null;
  }
}

export function saveUrl(url: string): void {
  try {
    localStorage.setItem(STORAGE_KEY, url);
  } catch {
    // ignore storage errors
  }
}

export function loadSavedConnId(): string | null {
  try {
    return localStorage.getItem(CONN_ID_KEY);
  } catch {
    return null;
  }
}

export function saveConnId(connId: string): void {
  try {
    localStorage.setItem(CONN_ID_KEY, connId);
  } catch {
    // ignore storage errors
  }
}
