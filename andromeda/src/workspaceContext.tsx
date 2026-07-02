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
  // Cross-pane "save this AI answer into the open notebook" channel: NotebookPane
  // registers a sink while a notebook is open (and clears it on unmount/close);
  // the AI panel shows a per-answer 노트에 저장 button only while a sink exists.
  // This is the notebook's output loop — an answer worth keeping becomes a cited
  // note source instead of scrolling away in the chat.
  noteSink: ((text: string) => void) | null;
  setNoteSink: (sink: ((text: string) => void) | null) => void;
  // Nav-rail customization: pane keys the user has hidden from the left rail
  // (settings is never hideable — it's the way back). Persisted to localStorage.
  hiddenViews: View[];
  toggleViewHidden: (v: View) => void;
  // Nav-rail order: non-settings pane keys in the user's chosen order (settings is
  // pinned last). Persisted. SettingsPane reorders; Sidebar renders in this order.
  viewOrder: View[];
  setViewOrder: (order: View[]) => void;
  // Coding mode: when on, the left rail shows coding sessions instead of panes
  // (the work area is the 코드 pane). Persisted to localStorage.
  codeMode: boolean;
  setCodeMode: (on: boolean) => void;
  // Bumped after a coding-mode mutation (start/discard/verify) so the Sidebar
  // session rail refetches in sync with the CodePane work area.
  codeSessionsRev: number;
  bumpCodeSessions: () => void;
  // The selected coding session ("code:<id>") in 코드 모드. CodeView shows this
  // session's chat in the center (the main work surface) and the management aside
  // controls it; the rail / CodePane set it. null = nothing selected (greeting).
  activeCodeKey: string | null;
  openCodeChat: (key: string) => void;
  setActiveCodeKey: (key: string | null) => void;
}

const HIDDEN_VIEWS_KEY = "andromeda.hiddenPanes";

function readHiddenViews(): View[] {
  const arr = getJSON<unknown[]>(HIDDEN_VIEWS_KEY);
  if (Array.isArray(arr)) return arr.filter((v): v is View => typeof v === "string" && v !== "settings");
  return [];
}

const VIEW_ORDER_KEY = "andromeda.viewOrder";

function readViewOrder(): View[] {
  const arr = getJSON<unknown[]>(VIEW_ORDER_KEY);
  return Array.isArray(arr) ? arr.filter((v): v is View => typeof v === "string") : [];
}

const CODE_MODE_KEY = "andromeda.codeMode";

function readCodeMode(): boolean {
  return getJSON<boolean>(CODE_MODE_KEY) === true;
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
  // Land on the 오늘 dashboard — unless coding mode is persisted on, where the rail
  // shows sessions and the work area must match (the 코드 pane), not Today.
  const [view, setView] = useState<View>(readCodeMode() ? "code" : "today");
  const [aiText, setAiText] = useState("");
  const [activeResource, setActiveResource] = useState<string | undefined>(undefined);
  const [wikiTarget, setWikiTarget] = useState<string | null>(null);
  const [paneTarget, setPaneTarget] = useState<PaneTarget | null>(null);
  // Stored via the updater form: the sink IS a function, and setState would
  // otherwise call it as an updater.
  const [noteSink, setNoteSinkState] = useState<((text: string) => void) | null>(null);
  const setNoteSink = (sink: ((text: string) => void) | null) => setNoteSinkState(() => sink);
  const [hiddenViews, setHiddenViews] = useState<View[]>(readHiddenViews);
  const [viewOrder, setViewOrder] = useState<View[]>(readViewOrder);
  const [codeMode, setCodeMode] = useState<boolean>(readCodeMode);
  const [codeSessionsRev, setCodeSessionsRev] = useState(0);
  const bumpCodeSessions = () => setCodeSessionsRev((n) => n + 1);
  const [activeCodeKey, setActiveCodeKey] = useState<string | null>(null);
  const openCodeChat = (key: string) => setActiveCodeKey(key);

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

  useEffect(() => {
    setJSON(VIEW_ORDER_KEY, viewOrder);
  }, [viewOrder]);

  useEffect(() => {
    setJSON(CODE_MODE_KEY, codeMode);
  }, [codeMode]);

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
        noteSink,
        setNoteSink,
        hiddenViews,
        toggleViewHidden,
        viewOrder,
        setViewOrder,
        codeMode,
        setCodeMode,
        codeSessionsRev,
        bumpCodeSessions,
        activeCodeKey,
        openCodeChat,
        setActiveCodeKey,
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
