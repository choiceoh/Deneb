import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import type { WorkspaceCommand } from "./commands";
import type { GatewayConfig } from "./gateway";
import { PANES, paneLabel } from "./components/panes";
import { getJSON, getString, setJSON, setString } from "./storage";
import { closeInTiles, focusedTile, isTileable, MAX_TILES, openInTiles, sanitizeTiles, splitInTiles } from "./tiling";
import type { View } from "./types";
import {
  Ctx,
  FeedCtx,
  FeedWriteCtx,
  type NotebookTop,
  type PaneFeed,
  type PaneTarget,
  type SavedLayout,
} from "./workspaceContext";

const HIDDEN_VIEWS_KEY = "andromeda.hiddenPanes";
const FOLLOW_MODE_KEY = "andromeda.followMode";
const VIEW_KEYS: ReadonlySet<View> = new Set(PANES.map((p) => p.key));

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

// Tiled-workspace persistence: the tile set + focused tile survive restarts, so
// a "결재+메일+위키" working arrangement is still there tomorrow morning.
const TILES_KEY = "andromeda.tiles";
const LAYOUTS_KEY = "andromeda.layouts";

function readTiles(): { tiles: View[]; focused: View } {
  const saved = getJSON<{ tiles?: unknown; focused?: unknown }>(TILES_KEY);
  const tiles = sanitizeTiles(saved?.tiles, VIEW_KEYS);
  const focused =
    typeof saved?.focused === "string" && tiles.includes(saved.focused as View) ? (saved.focused as View) : tiles[0];
  return { tiles, focused };
}

function readLayouts(): SavedLayout[] {
  const arr = getJSON<unknown[]>(LAYOUTS_KEY);
  if (!Array.isArray(arr)) return [];
  const out: SavedLayout[] = [];
  for (const item of arr) {
    if (typeof item !== "object" || item === null) continue;
    const { name, views } = item as { name?: unknown; views?: unknown };
    if (typeof name !== "string" || name.trim() === "") continue;
    const tiles = sanitizeTiles(views, VIEW_KEYS);
    if (!out.some((l) => l.name === name)) out.push({ name, views: tiles });
  }
  return out;
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
  // Land on the last focused tile (falls back to the 오늘 dashboard).
  const [{ tiles, focused: view }, setTileState] = useState(readTiles);
  const [feeds, setFeeds] = useState<ReadonlyMap<View, PaneFeed>>(new Map());
  const [wikiTarget, setWikiTarget] = useState<string | null>(null);
  const [paneTarget, setPaneTarget] = useState<PaneTarget | null>(null);
  // Stored via the updater form: the sink IS a function, and setState would
  // otherwise call it as an updater.
  const [noteSink, setNoteSinkState] = useState<((text: string) => Promise<boolean>) | null>(null);
  const setNoteSink = useCallback(
    (sink: ((text: string) => Promise<boolean>) | null) => setNoteSinkState(() => sink),
    [],
  );
  const [hiddenViews, setHiddenViews] = useState<View[]>(readHiddenViews);
  const [viewOrder, setViewOrder] = useState<View[]>(readViewOrder);
  const [notebookTop, setNotebookTop] = useState<NotebookTop>(readNotebookTop);
  const [layouts, setLayouts] = useState<SavedLayout[]>(readLayouts);
  const [paletteOpen, setPaletteOpen] = useState(false);
  // 데네브 spotlight 커맨드의 타일 플래시 신호 (seq로 같은 타일 반복 강조 구분).
  const [spotlight, setSpotlight] = useState<{ view: View; seq: number } | null>(null);
  // 기본은 접힘 — 작업 영역을 넓게 두고, 필요할 때 우측 탭/커맨드로 데네브 패널을 연다.
  const [aiCollapsed, setAiCollapsed] = useState(true);
  // AIPanel의 submit을 참조로 보관 — state로 두면 패널 리렌더마다 컨텍스트가 출렁인다.
  const askSinkRef = useRef<((text: string) => void) | null>(null);
  const setAskSink = useCallback((sink: ((text: string) => void) | null) => {
    askSinkRef.current = sink;
  }, []);
  const askDeneb = useCallback((text: string): boolean => {
    const sink = askSinkRef.current;
    if (!sink) return false;
    sink(text);
    return true;
  }, []);

  const toggleViewHidden = useCallback((v: View) => {
    if (v === "settings") return; // settings stays — it's the way back to this screen
    setHiddenViews((prev) => (prev.includes(v) ? prev.filter((x) => x !== v) : [...prev, v]));
  }, []);

  // ---- Tiled navigation ----
  // `view` is the current surface; when it's tileable it IS the focused tile.
  // Non-tile surfaces (settings/chat/files) park on top of the tile set, which
  // stays intact for the return trip.
  const setView = useCallback((v: View) => {
    setTileState((prev) => {
      if (!isTileable(v)) return { tiles: prev.tiles, focused: v };
      const base = focusedTile(prev.tiles, prev.focused);
      const next = openInTiles(prev.tiles, base, v);
      return { tiles: next.tiles, focused: v };
    });
  }, []);

  const splitPane = useCallback((v: View, target?: Omit<PaneTarget, "view">) => {
    if (!isTileable(v)) return;
    setPaneTarget(target ? { view: v, ...target } : null);
    setTileState((prev) => {
      const base = focusedTile(prev.tiles, prev.focused);
      const next = splitInTiles(prev.tiles, base, v);
      return { tiles: next.tiles, focused: next.focused };
    });
  }, []);

  const closePane = useCallback((v?: View) => {
    setTileState((prev) => {
      const base = focusedTile(prev.tiles, prev.focused);
      const next = closeInTiles(prev.tiles, base, v);
      // Closing while parked on a non-tile surface keeps that surface up.
      const focused = isTileable(prev.focused) ? next.focused : prev.focused;
      return { tiles: next.tiles, focused };
    });
  }, []);

  const applyLayout = useCallback((views: View[]) => {
    const tiles = sanitizeTiles(views, VIEW_KEYS);
    setTileState({ tiles, focused: tiles[0] });
  }, []);

  const saveLayout = useCallback(
    (name: string) => {
      const trimmed = name.trim();
      if (trimmed === "") return;
      setLayouts((prev) => {
        const entry: SavedLayout = { name: trimmed, views: tiles };
        return [...prev.filter((l) => l.name !== trimmed), entry];
      });
    },
    [tiles],
  );

  const deleteLayout = useCallback((name: string) => {
    setLayouts((prev) => prev.filter((l) => l.name !== name));
  }, []);

  const openWiki = useCallback(
    (path: string) => {
      setWikiTarget(path);
      setView("wiki");
    },
    [setView],
  );
  // Screen-follow: open the wiki page WITHOUT stealing the focused pane — wiki
  // joins as a split tile (or refreshes its target if already tiled).
  const splitWiki = useCallback(
    (path: string) => {
      setWikiTarget(path);
      splitPane("wiki");
    },
    [splitPane],
  );
  const consumeWikiTarget = useCallback(() => setWikiTarget(null), []);

  // 컨텍스트 팔로우 모드 — 기본 OFF, 명시 토글만 (침습적 조종이라 opt-in).
  const [followMode, setFollowModeState] = useState(() => getString(FOLLOW_MODE_KEY) === "1");
  const setFollowMode = useCallback((on: boolean) => {
    setFollowModeState(on);
    setString(FOLLOW_MODE_KEY, on ? "1" : "0");
  }, []);
  const openPane = useCallback(
    (nextView: View, target?: Omit<PaneTarget, "view">) => {
      setPaneTarget(target ? { view: nextView, ...target } : null);
      setView(nextView);
    },
    [setView],
  );
  const consumePaneTarget = useCallback(() => setPaneTarget(null), []);

  // The command bus: palette, shortcuts, and gateway-pushed workspace events all
  // land here, so every driver of the screen shares one vocabulary (commands.ts).
  const runCommand = useCallback(
    (cmd: WorkspaceCommand) => {
      switch (cmd.kind) {
        case "open":
          openPane(
            cmd.view,
            cmd.ref || cmd.query || cmd.date ? { id: cmd.ref, query: cmd.query, date: cmd.date } : undefined,
          );
          break;
        case "spotlight":
          openPane(cmd.view, { id: cmd.ref, spotlight: true });
          setSpotlight({ view: cmd.view, seq: Date.now() });
          break;
        case "prefill":
          openPane(cmd.view, { prefill: { title: cmd.title, due: cmd.due, note: cmd.note } });
          break;
        case "wiki":
          openWiki(cmd.path);
          break;
        case "split":
          splitPane(cmd.view, cmd.ref || cmd.date ? { id: cmd.ref, date: cmd.date } : undefined);
          break;
        case "close":
          closePane(cmd.view);
          break;
        case "focus":
          setView(cmd.view);
          break;
        case "layout":
          applyLayout(cmd.views);
          break;
      }
    },
    [openPane, openWiki, splitPane, closePane, setView, applyLayout],
  );

  useEffect(() => {
    setJSON(HIDDEN_VIEWS_KEY, hiddenViews);
  }, [hiddenViews]);

  useEffect(() => {
    setJSON(VIEW_ORDER_KEY, viewOrder);
  }, [viewOrder]);

  useEffect(() => {
    setJSON(NOTEBOOK_TOP_KEY, notebookTop);
  }, [notebookTop]);

  useEffect(() => {
    setJSON(TILES_KEY, { tiles, focused: view });
  }, [tiles, view]);

  useEffect(() => {
    setJSON(LAYOUTS_KEY, layouts);
  }, [layouts]);

  // ---- Pane→AI feeds (fast-changing; separate context) ----
  const registerPane = useCallback((slot: View, resource: string | undefined, text: string) => {
    setFeeds((prev) => {
      const cur = prev.get(slot);
      if (cur && cur.resource === resource && cur.text === text) return prev;
      const next = new Map(prev);
      next.set(slot, { resource, text });
      return next;
    });
  }, []);
  const unregisterPane = useCallback((slot: View) => {
    setFeeds((prev) => {
      if (!prev.has(slot)) return prev;
      const next = new Map(prev);
      next.delete(slot);
      return next;
    });
  }, []);

  // What the AI sees: on a single pane, that pane's projection (as before). In a
  // split, the focused tile first, then the other visible tiles with 화면 headers
  // — the AI's context matches the whole visible workspace.
  const { aiText, activeResource } = useMemo(() => {
    if (!isTileable(view) || tiles.length <= 1 || !tiles.includes(view)) {
      const single = feeds.get(view);
      return { aiText: single?.text ?? "", activeResource: single?.resource };
    }
    const ordered = [view, ...tiles.filter((t) => t !== view)];
    const parts: string[] = [];
    for (const t of ordered) {
      const feed = feeds.get(t);
      if (!feed || feed.text.trim() === "") continue;
      parts.push(parts.length === 0 ? feed.text : `[분할 화면: ${paneLabel(t)}]\n${feed.text}`);
    }
    return { aiText: parts.join("\n\n"), activeResource: feeds.get(view)?.resource };
  }, [feeds, tiles, view]);

  // Reader (per-keystroke churn, consumed by AIPanel) and writer (two stable
  // callbacks, consumed by every registering pane) stay separate so a keystroke
  // in one tile never re-renders the other tiles through their registration.
  const feedValue = useMemo(() => ({ aiText, activeResource }), [aiText, activeResource]);
  const feedWriteValue = useMemo(() => ({ registerPane, unregisterPane }), [registerPane, unregisterPane]);

  const value = useMemo(
    () => ({
      connected,
      cfg,
      setCfg,
      view,
      setView,
      tiles,
      splitPane,
      closePane,
      applyLayout,
      layouts,
      saveLayout,
      deleteLayout,
      runCommand,
      spotlight,
      paletteOpen,
      setPaletteOpen,
      aiCollapsed,
      setAiCollapsed,
      askDeneb,
      setAskSink,
      paneTarget,
      openPane,
      consumePaneTarget,
      wikiTarget,
      openWiki,
      splitWiki,
      consumeWikiTarget,
      followMode,
      setFollowMode,
      noteSink,
      setNoteSink,
      hiddenViews,
      toggleViewHidden,
      viewOrder,
      setViewOrder,
      notebookTop,
      setNotebookTop,
    }),
    [
      connected,
      cfg,
      setCfg,
      view,
      setView,
      tiles,
      splitPane,
      closePane,
      applyLayout,
      layouts,
      saveLayout,
      deleteLayout,
      runCommand,
      spotlight,
      paletteOpen,
      aiCollapsed,
      askDeneb,
      setAskSink,
      paneTarget,
      openPane,
      consumePaneTarget,
      wikiTarget,
      openWiki,
      splitWiki,
      consumeWikiTarget,
      followMode,
      setFollowMode,
      noteSink,
      setNoteSink,
      hiddenViews,
      toggleViewHidden,
      viewOrder,
      notebookTop,
    ],
  );

  return (
    <Ctx.Provider value={value}>
      <FeedWriteCtx.Provider value={feedWriteValue}>
        <FeedCtx.Provider value={feedValue}>{children}</FeedCtx.Provider>
      </FeedWriteCtx.Provider>
    </Ctx.Provider>
  );
}

// Tile cap re-export for UI affordances (palette hints, sidebar tooltips).
export { MAX_TILES };
