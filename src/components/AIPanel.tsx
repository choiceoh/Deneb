import { useState } from "react";
import type { GatewayConfig } from "../gateway";
import { useChat } from "../hooks";
import { field, line, pane } from "../theme";
import { useWorkspace } from "../workspaceContext";
import { ProactivePanel } from "./ProactivePanel";
import { paneLabel } from "./panes";

// Right column: Deneb AI collaboration. Reads the active pane's pushed text from
// the workspace context and streams a reply; tool calls that mutate data refresh
// the active grid (handled in useChat).
export function AIPanel({ cfg }: { cfg: GatewayConfig }) {
  const { view, aiText, activeResource } = useWorkspace();
  const { out, thinking, busy, send } = useChat(cfg);
  const [input, setInput] = useState("");

  function submit() {
    const msg = input.trim();
    if (!msg || busy) return;
    setInput("");
    void send(msg, aiText, activeResource);
  }

  return (
    <aside style={{ ...pane, borderLeft: line, display: "flex", flexDirection: "column" }}>
      <h3 style={{ marginTop: 2 }}>데네브 AI</h3>
      <ProactivePanel cfg={cfg} />
      <div style={{ fontSize: 12, opacity: 0.5, marginBottom: 6 }}>
        현재 뷰({paneLabel(view)})를 읽고, 도구로 바꾸면 즉시 반영됩니다
      </div>
      {thinking && (
        <div style={{ fontSize: 12, opacity: 0.6, fontStyle: "italic", marginBottom: 6 }}>🤔 {thinking}…</div>
      )}
      <pre style={{ flex: 1, whiteSpace: "pre-wrap", fontSize: 13, lineHeight: 1.5, margin: 0 }}>
        {out || (busy ? "응답 대기 중…" : "메시지를 보내면 응답이 스트리밍됩니다.")}
      </pre>
      <div style={{ display: "flex", gap: 6, marginTop: 8 }}>
        <input
          style={{ ...field, flex: 1 }}
          placeholder={busy ? "응답 중…" : "메시지…"}
          value={input}
          disabled={busy}
          onChange={(e) => setInput(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter") submit();
          }}
        />
        <button onClick={submit} disabled={busy} style={{ padding: "8px 14px" }}>
          전송
        </button>
      </div>
    </aside>
  );
}
