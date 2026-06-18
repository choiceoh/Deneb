import { useEffect, useMemo } from "react";
import type { GatewayConfig } from "../gateway";
import { pane } from "../theme";
import type { View } from "../types";
import { useWorkspace } from "../workspaceContext";
import { AIPanel } from "./AIPanel";
import { Sidebar } from "./Sidebar";
import { PANES } from "./panes";

// The three-column shell: nav/dashboard · work area · Deneb AI. The work area
// renders only the active pane (so just its resource fetches), and ⌘/Ctrl+N
// shortcuts are derived from the pane registry.
export function Workstation({ cfg, setCfg }: { cfg: GatewayConfig; setCfg: (c: GatewayConfig) => void }) {
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
        display: "grid",
        gridTemplateColumns: "210px 1fr 340px",
        height: "100vh",
        fontFamily: "system-ui, sans-serif",
      }}
    >
      <Sidebar cfg={cfg} setCfg={setCfg} />
      <main style={{ ...pane }}>
        <Active />
      </main>
      <AIPanel cfg={cfg} />
    </div>
  );
}
