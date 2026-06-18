import { useEffect } from "react";
import { type GatewayConfig, saveConfig } from "../gateway";
import { useGatewayStatus } from "../hooks";
import { color, field, kbd, line, navButton, pane } from "../theme";
import { useWorkspace } from "../workspaceContext";
import { PANES } from "./panes";

// Left column: registry-driven navigation + the gateway connection form.
export function Sidebar({ cfg, setCfg }: { cfg: GatewayConfig; setCfg: (c: GatewayConfig) => void }) {
  const { connected, view, setView } = useWorkspace();
  const { status, check } = useGatewayStatus(cfg);

  useEffect(() => {
    if (connected) void check();
    // check on mount only
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  return (
    <nav style={{ ...pane, borderRight: line, display: "flex", flexDirection: "column" }}>
      <h3 style={{ margin: "2px 0 12px" }}>Andromeda</h3>
      {PANES.map((p) => (
        <button key={p.key} style={navButton(view === p.key)} onClick={() => setView(p.key)}>
          <span>{p.label}</span>
          <span style={kbd}>⌘{p.shortcut}</span>
        </button>
      ))}
      <div style={{ opacity: 0.45, fontSize: 12, marginTop: 8 }}>위키 · 검색 (다음)</div>

      <div style={{ marginTop: "auto", paddingTop: 12, borderTop: line, display: "grid", gap: 6 }}>
        <input
          placeholder="게이트웨이 URL"
          value={cfg.url}
          onChange={(e) => setCfg({ ...cfg, url: e.target.value })}
          style={{ ...field, fontSize: 12 }}
        />
        <input
          placeholder="클라이언트 토큰"
          value={cfg.token}
          onChange={(e) => setCfg({ ...cfg, token: e.target.value })}
          style={{ ...field, fontSize: 12 }}
        />
        <button
          onClick={() => {
            saveConfig(cfg);
            void check();
          }}
          style={{ padding: "6px 10px" }}
        >
          연결
        </button>
        <span style={{ fontSize: 12, opacity: 0.7, color: status.startsWith("오류") ? color.danger : undefined }}>
          {status}
        </span>
      </div>
    </nav>
  );
}
