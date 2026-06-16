import { useEffect, useState } from "react";
import { type GatewayConfig, loadConfig, saveConfig, ping, chatStream } from "./gateway";
import { collectWorkspaceContext, type WorkspaceView } from "./workspace";

// Phase 0 skeleton: a 3-pane shell that proves (1) the gateway connection and
// (2) that Deneb's AI can READ the work area — as TEXT, no vision model.
//   left   — navigation/dashboard (Phase 1: Refine resources)
//   center — work area: connection form + a document stub (Phase 1: editor / data views)
//   right  — Deneb AI: chat stream that receives the work-area content as context
export function App() {
  const [cfg, setCfg] = useState<GatewayConfig>(loadConfig());
  const [status, setStatus] = useState("미연결");
  const [doc, setDoc] = useState(""); // work-area content — Deneb reads this on each turn
  const [chatInput, setChatInput] = useState("");
  const [chatOut, setChatOut] = useState("");
  const connected = Boolean(cfg.url && cfg.token);

  async function checkPing(c: GatewayConfig) {
    setStatus("확인 중…");
    try {
      const r = await ping(c);
      setStatus(r.ok ? `연결됨 · v${r.version ?? "?"} · ${r.model ?? ""}` : "ping 실패");
    } catch (e) {
      setStatus(`오류: ${(e as Error).message}`);
    }
  }

  useEffect(() => {
    if (connected) void checkPing(cfg);
    // run once on mount
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  async function send() {
    const msg = chatInput.trim();
    if (!msg) return;
    setChatInput("");
    setChatOut("");
    // Each work-area pane is a WorkspaceView that serializes its structured content
    // to text. Phase 1: grid/mail/calendar panes add theirs — no vision needed.
    const views: WorkspaceView[] = [
      { serializeForAI: () => (doc.trim() ? `[문서]\n${doc}` : "") },
    ];
    try {
      await chatStream(
        cfg,
        msg,
        {
          onDelta: (t) => setChatOut((p) => p + t),
          onError: (e) => setChatOut((p) => `${p}\n[오류] ${e}`),
        },
        "client:main",
        collectWorkspaceContext(views),
      );
    } catch (e) {
      setChatOut(`[오류] ${(e as Error).message}`);
    }
  }

  const pane: React.CSSProperties = { padding: 14, boxSizing: "border-box", overflow: "auto" };
  const line = "1px solid #2a2a2a";
  const field: React.CSSProperties = {
    padding: 8,
    background: "#1f1f1f",
    color: "#e8e8e8",
    border: line,
    borderRadius: 4,
  };

  return (
    <div style={{ display: "grid", gridTemplateColumns: "200px 1fr 340px", height: "100vh", fontFamily: "system-ui, sans-serif" }}>
      <nav style={{ ...pane, borderRight: line }}>
        <h3 style={{ margin: "2px 0 14px" }}>Andromeda</h3>
        <div style={{ opacity: 0.55, fontSize: 13, lineHeight: 1.8 }}>
          메일 · 일정 · 할일<br />연락처 · 위키
          <div style={{ marginTop: 10, opacity: 0.7 }}>(Phase 1)</div>
        </div>
      </nav>

      <main style={{ ...pane, display: "flex", flexDirection: "column", gap: 14 }}>
        <section>
          <h2 style={{ marginTop: 2 }}>
            게이트웨이 연결 <span style={{ opacity: 0.5, fontSize: 14 }}>(Phase 0)</span>
          </h2>
          <div style={{ display: "grid", gap: 8, maxWidth: 540 }}>
            <input placeholder="게이트웨이 URL (http://…:18789)" value={cfg.url}
              onChange={(e) => setCfg({ ...cfg, url: e.target.value })} style={field} />
            <input placeholder="클라이언트 토큰 (hex64)" value={cfg.token}
              onChange={(e) => setCfg({ ...cfg, token: e.target.value })} style={field} />
            <div style={{ display: "flex", gap: 10, alignItems: "center" }}>
              <button onClick={() => { saveConfig(cfg); void checkPing(cfg); }} style={{ padding: "8px 14px" }}>
                저장 + 연결 확인
              </button>
              <span style={{ fontSize: 14 }}>상태: {status}</span>
            </div>
          </div>
        </section>

        <section style={{ flex: 1, display: "flex", flexDirection: "column" }}>
          <h3 style={{ margin: "2px 0 8px" }}>
            작업 영역 <span style={{ opacity: 0.5, fontSize: 13 }}>(데네브 AI가 이 내용을 텍스트로 읽습니다)</span>
          </h3>
          <textarea
            value={doc}
            onChange={(e) => setDoc(e.target.value)}
            placeholder="여기에 문서를 작성하세요. 우측 AI에게 '이 문서 다듬어줘'라고 하면 이 내용을 컨텍스트로 함께 읽습니다. (Phase 1에서 실제 에디터·메일·데이터 그리드로 교체 — 각 뷰가 serializeForAI로 텍스트화)"
            style={{ ...field, flex: 1, resize: "none", fontFamily: "ui-monospace, monospace", fontSize: 13, lineHeight: 1.5 }}
          />
        </section>
      </main>

      <aside style={{ ...pane, borderLeft: line, display: "flex", flexDirection: "column" }}>
        <h3 style={{ marginTop: 2 }}>데네브 AI</h3>
        <pre style={{ flex: 1, whiteSpace: "pre-wrap", fontSize: 13, lineHeight: 1.5, margin: 0 }}>
          {chatOut || "여기에 응답이 스트리밍됩니다.\n(작업 영역 내용을 텍스트 컨텍스트로 함께 전송 — 비전 불필요)"}
        </pre>
        <div style={{ display: "flex", gap: 6, marginTop: 8 }}>
          <input style={{ ...field, flex: 1 }} placeholder="메시지…" value={chatInput}
            onChange={(e) => setChatInput(e.target.value)}
            onKeyDown={(e) => { if (e.key === "Enter") void send(); }} />
          <button onClick={() => void send()} style={{ padding: "8px 14px" }}>전송</button>
        </div>
      </aside>
    </div>
  );
}
