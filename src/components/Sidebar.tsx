import { useEffect, useRef, useState } from "react";
import { type GatewayConfig, saveConfig } from "@/gateway";
import { useGatewayStatus } from "@/hooks";
import { useWorkspace } from "@/workspaceContext";
import { Icon } from "./Icon";
import { LiveDot } from "./LiveDot";
import { WindowControls } from "./WindowControls";
import { PANES } from "./panes";

// Slim nav rail: registry-driven icon tabs (the active one lifts like a Zen tab)
// + a connection status dot. The gateway URL/token form is tucked into a popover
// off the status dot so the rail stays narrow — on the real host it auto-connects
// from the keychain anyway, so the form is rarely needed.
export function Sidebar({ cfg, setCfg }: { cfg: GatewayConfig; setCfg: (c: GatewayConfig) => void }) {
  const { connected, view, setView, hiddenViews } = useWorkspace();
  // Rail tabs the user chose to show (settings is never hideable — see SettingsPane).
  const visiblePanes = PANES.filter((p) => p.key === "settings" || !hiddenViews.includes(p.key));
  const { status, check } = useGatewayStatus(cfg);
  const [open, setOpen] = useState(false);
  const popRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (connected) void check();
    // check on mount only
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // Close the gateway popover on Escape or a press outside it. The toggle button
  // lives inside popRef, so it still opens/closes through its own handler.
  useEffect(() => {
    if (!open) return;
    function onDown(e: MouseEvent) {
      if (popRef.current && !popRef.current.contains(e.target as Node)) setOpen(false);
    }
    function onKey(e: KeyboardEvent) {
      if (e.key === "Escape") setOpen(false);
    }
    document.addEventListener("mousedown", onDown);
    window.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("mousedown", onDown);
      window.removeEventListener("keydown", onKey);
    };
  }, [open]);

  const host = cfg.url ? cfg.url.replace(/^https?:\/\//, "").replace(/\/+$/, "") : "연결";
  const isError = status.startsWith("오류");

  return (
    <nav
      data-tauri-drag-region
      style={{
        width: "var(--rail-w)",
        flex: "0 0 auto",
        display: "flex",
        flexDirection: "column",
        gap: 2,
        padding: "2px 2px",
        position: "relative",
      }}
    >
      <WindowControls />
      {visiblePanes.map((p, i) => (
        <button
          key={p.key}
          className={"nav-item fade-up" + (view === p.key ? " active" : "")}
          style={{ animationDelay: `${i * 26}ms` }}
          onClick={() => setView(p.key)}
          title={p.label}
        >
          <span className="ico">
            <Icon name={p.key} />
          </span>
          <span style={{ overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>{p.label}</span>
        </button>
      ))}

      <div ref={popRef} style={{ marginTop: "auto", paddingTop: 8 }}>
        <button
          className="nav-item"
          onClick={() => setOpen((o) => !o)}
          style={{ fontSize: 11, color: "var(--muted-2)" }}
          title="게이트웨이 연결"
        >
          <LiveDot connected={connected} pulse />
          <span style={{ overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
            {connected ? host : "연결"}
          </span>
        </button>

        {open && (
          <div
            className="panel"
            style={{
              position: "absolute",
              left: 0,
              bottom: 46,
              width: 248,
              padding: 13,
              display: "grid",
              gap: 7,
              zIndex: 30,
            }}
          >
            <div className="micro" style={{ marginBottom: 1 }}>
              게이트웨이
            </div>
            <input
              className="field"
              placeholder="게이트웨이 URL"
              value={cfg.url}
              onChange={(e) => setCfg({ ...cfg, url: e.target.value })}
            />
            <input
              className="field"
              placeholder="클라이언트 토큰"
              value={cfg.token}
              onChange={(e) => setCfg({ ...cfg, token: e.target.value })}
            />
            <button
              className="btn btn-accent"
              onClick={() => {
                saveConfig(cfg);
                void check();
              }}
            >
              연결
            </button>
            {status && <span style={{ fontSize: 12, color: isError ? "var(--due)" : "var(--muted)" }}>{status}</span>}
          </div>
        )}
      </div>
    </nav>
  );
}
