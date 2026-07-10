import { useEffect, useState, type ReactNode } from "react";
import type { GatewayConfig } from "./gateway";
import { getJSON, setJSON } from "./storage";
import type { View } from "./types";
import { Ctx, type NotebookTop, type PaneTarget } from "./workspaceContext";

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

const NOTEBOOK_TOP_KEY = "andromeda.notebook.top";
const NOTEBOOK_FOLDED_KEY = "andromeda.notebook.folded"; // legacy boolean, migrated below

function readNotebookTop(): NotebookTop {
  const v = getJSON<NotebookTop>(NOTEBOOK_TOP_KEY);
  if (v === "folded" || v === "default" || v === "expanded") return v;
  // Migrate the old boolean fold flag: true → 접힘, otherwise 기본.
  return getJSON<boolean>(NOTEBOOK_FOLDED_KEY) === true ? "folded" : "default";
}

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
  // Land on the 오늘 dashboard.
  const [view, setView] = useState<View>("today");
  const [aiText, setAiText] = useState("");
  const [activeResource, setActiveResource] = useState<string | undefined>(undefined);
  const [wikiTarget, setWikiTarget] = useState<string | null>(null);
  const [paneTarget, setPaneTarget] = useState<PaneTarget | null>(null);
  // Stored via the updater form: the sink IS a function, and setState would
  // otherwise call it as an updater.
  const [noteSink, setNoteSinkState] = useState<((text: string) => Promise<boolean>) | null>(null);
  const setNoteSink = (sink: ((text: string) => Promise<boolean>) | null) => setNoteSinkState(() => sink);
  const [hiddenViews, setHiddenViews] = useState<View[]>(readHiddenViews);
  const [viewOrder, setViewOrder] = useState<View[]>(readViewOrder);
  const [notebookTop, setNotebookTop] = useState<NotebookTop>(readNotebookTop);

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
    setJSON(NOTEBOOK_TOP_KEY, notebookTop);
  }, [notebookTop]);

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
        notebookTop,
        setNotebookTop,
      }}
    >
      {children}
    </Ctx.Provider>
  );
}
