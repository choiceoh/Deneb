// Workstation state shared across the three columns, and the mechanism by which
// mounted panes publish their content to the AI panel.
//
// Two contexts on purpose:
//  - Ctx (navigation/layout/commands) — changes on user navigation, rarely.
//  - FeedCtx (pane→AI text projections) — changes on every keystroke of an open
//    editor. Splitting them keeps editor keystrokes from re-rendering the rail,
//    the shell, and every pane (the old single-context cascade).
//
// The "well-structured back end" principle: every MOUNTED pane serializes its
// content to TEXT and pushes it here, keyed by its tile (TileCtx). The AI reads
// the focused tile's text first, then the other visible tiles — what you see is
// what the AI sees, across the whole split.
import { createContext, useContext, useEffect } from "react";
import type { GatewayConfig } from "./gateway";
import type { WorkspaceCommand } from "./commands";
import type { View } from "./types";

export interface PaneTarget {
  view: View;
  id?: string | number;
  dayKey?: string;
  path?: string;
  size?: number;
  query?: string; // query-driven panes (search): run this query on open
  // 데네브 조종 확장: date = day-pager 점프(YYYY-MM-DD, mail/approvals),
  // prefill = 할일 폼 초안(저장은 사용자), spotlight = 타일 플래시 동반.
  date?: string;
  prefill?: { title: string; due?: string; note?: string };
  spotlight?: boolean;
}

// 노트북 상단(자료) 영역의 3단계 높이 — 접힘(바)·기본(30%)·확대(70%). 바 버튼이
// 접힘→기본→확대→접힘 순으로 순환시키고, Workstation이 그리드 상단 행을 이 값으로 맞춘다.
export type NotebookTop = "folded" | "default" | "expanded";

export interface SavedLayout {
  name: string;
  views: View[];
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
  // Tiled workspace: the work area hosts 1–3 panes side by side. `tiles` is the
  // current tile set; the focused tile is `view` when tileable (falls back to
  // tiles[0] while a non-tile surface like settings/chat is up).
  tiles: View[];
  splitPane: (v: View, target?: Omit<PaneTarget, "view">) => void;
  closePane: (v?: View) => void;
  applyLayout: (views: View[]) => void;
  // Named layouts (localStorage): capture the current tile set, re-apply later.
  layouts: SavedLayout[];
  saveLayout: (name: string) => void;
  deleteLayout: (name: string) => void;
  // The command bus: palette / shortcuts / gateway pushes all execute through
  // this single entry point.
  runCommand: (cmd: WorkspaceCommand) => void;
  // 데네브의 "여기 보세요" — 마지막 spotlight 커맨드 (Workstation이 타일 플래시로 소비).
  spotlight: { view: View; seq: number } | null;
  // ⌘K command palette visibility (rendered by Workstation).
  paletteOpen: boolean;
  setPaletteOpen: (open: boolean) => void;
  // 우측 데네브 패널 접힘 — 커맨드(팔레트·데네브에게 묻기)가 패널을 다시 열 수 있도록
  // 컨텍스트로 승격 (Workstation 로컬 state였다).
  aiCollapsed: boolean;
  setAiCollapsed: (collapsed: boolean) => void;
  // "데네브에게 묻기" sink: AIPanel registers a submit function; the palette
  // sends a prompt through it (opens the panel first via setAiCollapsed).
  askDeneb: (text: string) => boolean;
  setAskSink: (sink: ((text: string) => void) | null) => void;
  // The reverse channel: the AI panel publishes each finished answer; a pane may
  // observe it (the notebook highlights the sources an answer cited). Ref-held in
  // the provider, so publishing does not re-render the shell.
  publishAnswer: (text: string) => void;
  setAnswerSink: (sink: ((text: string) => void) | null) => void;
  paneTarget: PaneTarget | null;
  openPane: (view: View, target?: Omit<PaneTarget, "view">) => void;
  consumePaneTarget: () => void;
  // Cross-pane "open this wiki page" channel: 인물 카드·검색 결과·노트북이 위키 경로를
  // 넘기면 위키 pane으로 전환하고 해당 페이지를 연다. WikiPane이 마운트되어 소비한다.
  wikiTarget: string | null;
  openWiki: (path: string) => void;
  // Like openWiki but as a SPLIT tile — screen-follow features (결재 검토 모드,
  // 컨텍스트 팔로우) must not steal the pane the user is reading.
  splitWiki: (path: string) => void;
  consumeWikiTarget: () => void;
  // 컨텍스트 팔로우 모드: 대화에서 언급된 프로젝트/인물 위키를 옆 타일로 따라
  // 연다. 침습적이라 기본 OFF; 팔레트로 토글, localStorage 영속.
  followMode: boolean;
  setFollowMode: (on: boolean) => void;
  // Cross-pane "save this AI answer into the open notebook" channel: NotebookPane
  // registers a sink while a notebook is open (and clears it on unmount/close);
  // the AI panel shows a per-answer 노트에 저장 button only while a sink exists.
  // This is the notebook's output loop — an answer worth keeping becomes a cited
  // note source instead of scrolling away in the chat. The sink resolves with
  // whether the pin actually landed, so the button can show failure + retry
  // instead of a false 저장됨.
  noteSink: ((text: string) => Promise<boolean>) | null;
  setNoteSink: (sink: ((text: string) => Promise<boolean>) | null) => void;
  // Nav-rail customization: pane keys the user has hidden from the left rail
  // (settings is never hideable — it's the way back). Persisted to localStorage.
  hiddenViews: View[];
  toggleViewHidden: (v: View) => void;
  // Nav-rail order: non-settings pane keys in the user's chosen order (settings is
  // pinned last). Persisted. SettingsPane reorders; Sidebar renders in this order.
  viewOrder: View[];
  setViewOrder: (order: View[]) => void;
  // 노트북 상단(자료) 영역 높이(3단계): NotebookPane's bar button cycles it; Workstation
  // reads it to size the bottom-chat grid's top row — 접힘=바 높이(하단 채팅이 전부 차지),
  // 기본=30%, 확대=70%. Persisted to localStorage.
  notebookTop: NotebookTop;
  setNotebookTop: (t: NotebookTop) => void;
}

export const Ctx = createContext<WorkspaceCtx | null>(null);

export function useWorkspace(): WorkspaceCtx {
  const c = useContext(Ctx);
  if (!c) throw new Error("useWorkspace must be used within <WorkspaceProvider>");
  return c;
}

// ---- Pane→AI feed (fast-changing, own context) ----

export interface PaneFeed {
  resource?: string;
  text: string;
}

// Reader and writer live in SEPARATE contexts on purpose: the read value
// (aiText) changes on every editor keystroke, and if registration rode in the
// same context every pane calling useRegisterPane would re-render on any other
// tile's keystroke — exactly the cascade the split is meant to kill. The writer
// value is two stable callbacks, so registering panes never re-render from it.
interface FeedReadValue {
  // The AI-panel projection: focused tile's text first, other visible tiles
  // appended with headers (WorkspaceProvider derives it from the tile set).
  aiText: string;
  // The focused pane's backing resource — what the AI's mutating tool calls
  // should refresh.
  activeResource?: string;
}

interface FeedWriteValue {
  registerPane: (view: View, resource: string | undefined, text: string) => void;
  unregisterPane: (view: View) => void;
}

export const FeedCtx = createContext<FeedReadValue | null>(null);
export const FeedWriteCtx = createContext<FeedWriteValue | null>(null);

export function useAiFeed(): FeedReadValue {
  const c = useContext(FeedCtx);
  if (!c) throw new Error("useAiFeed must be used within <WorkspaceProvider>");
  return c;
}

// Which tile slot a pane is mounted in. Workstation provides one per tile (and
// for the single-pane render); panes never read it directly — useRegisterPane
// does. null = outside any tile (defensive default: register as the current view).
export const TileCtx = createContext<View | null>(null);

// Called by a mounted pane to publish its AI-text projection and backing
// resource whenever its content changes. Tile-aware: in a split, every mounted
// pane keeps its own feed registered; the provider assembles the AI text from
// the tile set. Unregisters on unmount so closed tiles drop out of the feed.
export function useRegisterPane(resource: string | undefined, text: string): void {
  const feed = useContext(FeedWriteCtx);
  const tile = useContext(TileCtx);
  if (!feed) throw new Error("useRegisterPane must be used within <WorkspaceProvider>");
  const { registerPane, unregisterPane } = feed;
  const { view } = useWorkspace();
  const slot = tile ?? view;
  useEffect(() => {
    registerPane(slot, resource, text);
  }, [registerPane, slot, resource, text]);
  useEffect(() => () => unregisterPane(slot), [unregisterPane, slot]);
}
