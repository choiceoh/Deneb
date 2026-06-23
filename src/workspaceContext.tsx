// Workstation state shared across the three columns, and the mechanism by which
// the active pane publishes its content to the AI panel.
//
// The "well-structured back end" principle (workspace.ts): the active pane
// serializes its current content to TEXT and PUSHES it here; the AI panel reads
// the latest text — no vision, no cross-pane data coupling. Only the active pane
// is mounted (Workstation renders one at a time), so whoever is mounted owns the
// AI context and names the resource the AI's tool calls should invalidate.
import { createContext, useContext, useEffect, useState, type ReactNode } from "react";
import type { GatewayConfig } from "./gateway";
import { getJSON, setJSON } from "./storage";
import type { View } from "./types";

export interface PaneTarget {
  view: View;
  id?: string | number;
  dayKey?: string;
  path?: string;
}

interface WorkspaceCtx {
  connected: boolean;
  // The gateway config, exposed so query-driven panes (wiki/search/notebook) can
  // call non-CRUD RPCs directly (DESIGN §9) instead of going through the provider.
  cfg: GatewayConfig;
  // Update the gateway config (lifted to App). The settings pane edits URL/token
  // through this; App persists + rebuilds the data/auth providers.
  setCfg: (c: GatewayConfig) => void;
  view: View;
  setView: (v: View) => void;
  paneTarget: PaneTarget | null;
  openPane: (view: View, target?: Omit<PaneTarget, "view">) => void;
  consumePaneTarget: () => void;
  // Pushed by the active pane: its content serialized for the AI, and the Refine
  // resource (if any) backing it — used to refresh after the AI mutates data.
  aiText: string;
  activeResource?: string;
  registerPane: (resource: string | undefined, text: string) => void;
  // Cross-pane "open this wiki page" channel: 인물 카드·검색 결과·노트북이 위키 경로를
  // 넘기면 위키 pane으로 전환하고 해당 페이지를 연다. WikiPane이 마운트되어 소비한다.
  wikiTarget: string | null;
  openWiki: (path: string) => void;
  consumeWikiTarget: () => void;
  // Nav-rail customization: pane keys the user has hidden from the left rail
  // (settings is never hideable — it's the way back). Persisted to localStorage.
  hiddenViews: View[];
  toggleViewHidden: (v: View) => void;
}

const HIDDEN_VIEWS_KEY = "andromeda.hiddenPanes";

function readHiddenViews(): View[] {
  const arr = getJSON<unknown[]>(HIDDEN_VIEWS_KEY);
  if (Array.isArray(arr)) return arr.filter((v): v is View => typeof v === "string" && v !== "settings");
  return [];
}

const Ctx = createContext<WorkspaceCtx | null>(null);

export function WorkspaceProvider({
  connected,
  cfg,
  setCfg,
  children,
}: {
  connected: boolean;
  cfg: GatewayConfig;
  setCfg: (c: GatewayConfig) => void;
  children: ReactNode;
}) {
  const [view, setView] = useState<View>("today"); // land on the 오늘 dashboard
  const [aiText, setAiText] = useState("");
  const [activeResource, setActiveResource] = useState<string | undefined>(undefined);
  const [wikiTarget, setWikiTarget] = useState<string | null>(null);
  const [paneTarget, setPaneTarget] = useState<PaneTarget | null>(null);
  const [hiddenViews, setHiddenViews] = useState<View[]>(readHiddenViews);

  const toggleViewHidden = (v: View) => {
    if (v === "settings") return; // settings stays — it's the way back to this screen
    setHiddenViews((prev) => (prev.includes(v) ? prev.filter((x) => x !== v) : [...prev, v]));
  };

  const registerPane = (resource: string | undefined, t: string) => {
    setActiveResource(resource);
    setAiText(t);
  };

  const openWiki = (path: string) => {
    setWikiTarget(path);
    setView("wiki");
  };
  const consumeWikiTarget = () => setWikiTarget(null);
  const openPane = (nextView: View, target?: Omit<PaneTarget, "view">) => {
    setPaneTarget(target ? { view: nextView, ...target } : null);
    setView(nextView);
  };
  const consumePaneTarget = () => setPaneTarget(null);

  useEffect(() => {
    setJSON(HIDDEN_VIEWS_KEY, hiddenViews);
  }, [hiddenViews]);

  return (
    <Ctx.Provider
      value={{
        connected,
        cfg,
        setCfg,
        view,
        setView,
        paneTarget,
        openPane,
        consumePaneTarget,
        aiText,
        activeResource,
        registerPane,
        wikiTarget,
        openWiki,
        consumeWikiTarget,
        hiddenViews,
        toggleViewHidden,
      }}
    >
      {children}
    </Ctx.Provider>
  );
}

export function useWorkspace(): WorkspaceCtx {
  const c = useContext(Ctx);
  if (!c) throw new Error("useWorkspace must be used within <WorkspaceProvider>");
  return c;
}

// Called by the active pane to publish its AI-text projection and backing
// resource whenever its content changes.
export function useRegisterPane(resource: string | undefined, text: string): void {
  const { registerPane } = useWorkspace();
  useEffect(() => {
    registerPane(resource, text);
    // registerPane is stable enough; re-run when the projection or resource changes.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [resource, text]);
}
