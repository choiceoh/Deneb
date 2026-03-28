// ═══ Propus app state — Svelte 5 runes (class-based for module export) ═══

import type { ChatMessage, ServerMessage } from "./types";
import { parseSegments } from "./types";
import { PropusWebSocket, loadSavedUrl, saveUrl, saveConnId } from "./ws";
import type { ConnectionStatus } from "./ws";

let nextId = 0;
function genId(): string {
  return `msg-${++nextId}`;
}

class PropusState {
  messages = $state<ChatMessage[]>([]);
  streamingText = $state("");
  isStreaming = $state(false);
  connectionStatus = $state<ConnectionStatus>("disconnected");
  needsServerUrl = $state(true);
  needsApiKey = $state(false);
  modelName = $state("");
  serviceName = $state("");
  statusText = $state("서버 연결 대기 중...");
  usageText = $state("");
  msgCount = $state(0);
  isTyping = $state(false);
  sidebarVisible = $state(true);

  private _typingTimer: ReturnType<typeof setTimeout> | undefined;
  private _toolStartTimes = new Map<string, number>();

  streamingSegments = $derived(parseSegments(this.streamingText));

  private ws: PropusWebSocket;

  constructor() {
    this.ws = new PropusWebSocket(
      (msg) => this.handleServerMessage(msg),
      (status) => this.handleStatusChange(status),
    );
  }

  private handleStatusChange(status: ConnectionStatus): void {
    this.connectionStatus = status;
    if (status === "connected") {
      this.statusText = "서버 연결됨, 설정 확인 중...";
    } else if (status === "connecting") {
      this.statusText = "연결 중...";
    } else {
      this.statusText = "서버 연결 끊김";
    }
  }

  private handleServerMessage(msg: ServerMessage): void {
    switch (msg.type) {
      case "Text":
        this.streamingText += msg.data.content;
        this.isTyping = false;
        break;

      case "ToolStart":
        this.flushStreaming();
        this._toolStartTimes.set(msg.data.name, Date.now());
        this.messages = [
          ...this.messages,
          {
            id: genId(),
            role: "tool",
            content: msg.data.args || "",
            segments: [],
            toolName: msg.data.name,
            expanded: false,
          },
        ];
        this.msgCount = this.messages.length;
        break;

      case "ToolResult": {
        const startTime = this._toolStartTimes.get(msg.data.name);
        const duration = startTime ? Date.now() - startTime : undefined;
        this._toolStartTimes.delete(msg.data.name);
        this.messages = [
          ...this.messages,
          {
            id: genId(),
            role: "tool",
            content: "",
            segments: [],
            toolName: `${msg.data.name} ✓`,
            toolResult: msg.data.result,
            toolDuration: duration,
            expanded: false,
          },
        ];
        this.msgCount = this.messages.length;
        break;
      }

      case "Usage":
        this.usageText = `입력 ${msg.data.prompt}  출력 ${msg.data.completion}  합계 ${msg.data.total}`;
        break;

      case "Done":
        this.flushStreaming();
        this.isStreaming = false;
        this.isTyping = false;
        this.statusText = "준비됨";
        break;

      case "Error":
        this.flushStreaming();
        this.messages = [
          ...this.messages,
          {
            id: genId(),
            role: "assistant",
            content: `오류: ${msg.data.message}`,
            segments: [{ type: "text", content: `오류: ${msg.data.message}` }],
          },
        ];
        this.msgCount = this.messages.length;
        this.isStreaming = false;
        this.statusText = "오류 발생";
        break;

      case "SessionSaved":
        this.statusText = `세션 저장됨: ${msg.data.path}`;
        break;

      case "ChatCleared":
        this.messages = [];
        this.streamingText = "";
        this.usageText = "";
        this.msgCount = 0;
        this.statusText = "대화 초기화됨";
        break;

      case "ConfigStatus":
        this.needsApiKey = msg.data.needs_api_key;
        if (!msg.data.needs_api_key) {
          this.modelName = msg.data.model;
          this.serviceName = msg.data.service;
          this.statusText = msg.data.deneb_status
            ? `준비됨 (Deneb ${msg.data.deneb_status})`
            : "준비됨";
        }
        // Save conn_id for session resume on reconnect.
        if (msg.data.conn_id) {
          saveConnId(msg.data.conn_id);
        }
        break;

      case "File":
        this.flushStreaming();
        this.messages = [
          ...this.messages,
          {
            id: genId(),
            role: "file",
            content: msg.data.name,
            segments: [],
            fileName: msg.data.name,
            fileUrl: msg.data.url,
            fileSize: msg.data.size,
            fileMediaType: msg.data.media_type,
          },
        ];
        this.msgCount = this.messages.length;
        break;

      case "Typing":
        this.isTyping = true;
        this.statusText = "입력 중...";
        clearTimeout(this._typingTimer);
        this._typingTimer = setTimeout(() => {
          this.isTyping = false;
        }, 3000);
        break;

      case "Ping":
      case "Pong":
        // Handled at the transport layer (ws.ts); included for type exhaustiveness.
        break;
    }
  }

  private flushStreaming(): void {
    if (this.streamingText.trim()) {
      this.messages = [
        ...this.messages,
        {
          id: genId(),
          role: "assistant",
          content: this.streamingText,
          segments: parseSegments(this.streamingText),
        },
      ];
      this.msgCount = this.messages.length;
    }
    this.streamingText = "";
  }

  // --- Actions ---

  connectToServer(url: string): void {
    if (!url.trim()) return;
    saveUrl(url);
    this.needsServerUrl = false;
    this.ws.connect(url);
  }

  sendMessage(text: string): void {
    if (!text.trim() || this.isStreaming) return;

    this.messages = [
      ...this.messages,
      {
        id: genId(),
        role: "user",
        content: text,
        segments: [{ type: "text", content: text }],
      },
    ];
    this.msgCount = this.messages.length;
    this.isStreaming = true;
    this.streamingText = "";
    this.statusText = "생성 중...";
    this.ws.send({ type: "SendMessage", data: { text } });
  }

  stopGeneration(): void {
    this.ws.send({ type: "StopGeneration" });
    this.isStreaming = false;
    this.statusText = "중지됨";
  }

  clearChat(): void {
    this.ws.send({ type: "ClearChat" });
    this.messages = [];
    this.streamingText = "";
    this.usageText = "";
    this.msgCount = 0;
    this.statusText = "대화 초기화됨";
  }

  saveSession(): void {
    this.ws.send({ type: "SaveSession" });
  }

  submitApiKey(key: string): void {
    this.ws.send({ type: "SetApiKey", data: { key } });
  }

  reconnect(): void {
    this.ws.reconnect();
  }

  toggleSidebar(): void {
    this.sidebarVisible = !this.sidebarVisible;
  }

  initAutoConnect(): void {
    const saved = loadSavedUrl();
    if (saved) {
      this.needsServerUrl = false;
      this.statusText = `저장된 서버: ${saved}`;
      this.ws.connect(saved);
    }
  }
}

export const app = new PropusState();
