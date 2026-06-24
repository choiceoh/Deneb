import { useEffect, useMemo } from "react";
import type { GatewayConfig } from "@/gateway";
import type { View } from "@/types";
import { useWorkspace } from "@/workspaceContext";
import { AIPanel } from "./AIPanel";
import { ChatView } from "./ChatView";
import { Sidebar } from "./Sidebar";
import { PANES } from "./panes";

// The shell: a slim nav rail + two floating panels (work area · Deneb AI) drifting
// on the window's gradient, Zen-browser style. The work area renders only the
// active pane; ⌘/Ctrl+0–9 shortcuts are derived from the pane registry (the labels
// are hidden in the rail, but the keys still work).
export function Workstation({ cfg }: { cfg: GatewayConfig }) {
  const { view, setView } = useWorkspace();

  const shortcuts = useMemo(() => {
    const m: Record<string, View> = {};
    for (const p of PANES) m[p.shortcut] = p.key;
    return m;
  }, []);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (!(e.metaKey || e.ctrlKey)) return;
      const next = shortcuts[e.key];
      if (!next) return;
      e.preventDefault();
      setView(next);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [shortcuts, setView]);

  const Active = PANES.find((p) => p.key === view)?.Component ?? PANES[0].Component;

  return (
    <div
      style={{
        position: "relative",
        display: "flex",
        gap: "var(--gap)",
        height: "100vh",
        padding: "22px var(--gap) var(--gap)",
        boxSizing: "border-box",
      }}
    >
      {/* Transparent top-edge drag handle — grab the very top of the frameless
          window to move it. Lives in the top padding band, clear of the panels
          and the top-left controls. */}
      <div className="drag-strip" data-tauri-drag-region />
      <Sidebar />
      {/* 작업 pane은 비채팅 탭에서만 렌더(채팅 탭은 ChatView가 중앙+우측을 차지). */}
      {view !== "chat" && (
        <main className="panel" style={{ flex: 1, minWidth: 0, overflow: "auto", padding: "20px 22px" }}>
          <div key={view} className="pane-enter">
            <Active />
          </div>
        </main>
      )}
      {/* 채팅 탭(비업무)·측면 데네브 패널 모두 항상 마운트(각자 대화 유지) — 비활성 탭에선 숨긴다. */}
      <ChatView cfg={cfg} hidden={view !== "chat"} />
      <AIPanel cfg={cfg} hidden={view === "chat"} />
    </div>
  );
}
