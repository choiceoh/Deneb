import { useEffect, useMemo, useState } from "react";
import type { GatewayConfig } from "@/gateway";
import { useNativeSync } from "@/sync";
import { isTileable, MAX_TILES } from "@/tiling";
import type { View } from "@/types";
import { TileCtx, useWorkspace } from "@/workspaceContext";
import { AIPanel } from "./AIPanel";
import { ChatView } from "./ChatView";
import { CommandPalette } from "./CommandPalette";
import { Icon } from "./Icon";
import { Sidebar } from "./Sidebar";
import { orderedViews, PANES, paneLabel } from "./panes";
import { FilesPane } from "./panes/FilesPane";

// The shell: a slim nav rail + floating panels (work area · Deneb AI) drifting
// on the window's gradient, Zen-browser style. The work area renders the tile
// set (1–3 panes side by side; ⌘K palette / ⌘⇧key / Alt+rail-click split it);
// ⌘/Ctrl+0–9 shortcuts are derived from the pane registry.

// OS 표준 텍스트 편집 콤보(⌘/Ctrl + 복사·붙여넣기·잘라내기·전체선택·실행취소/재실행)의
// 키 — pane 단축키가 이걸 가로채면 복사가 화면 전환으로 둔갑한다(코드 pane의 옛 ⌘C 사고).
// 레지스트리에 이 키를 배정해도 onKey 가드가 걸러내 편집 동작이 항상 이긴다. 분할(⌘⇧)도
// 같은 키는 건드리지 않는다(⌘⇧V 서식 없이 붙여넣기, ⌘⇧Z 재실행 …).
const EDIT_KEYS = new Set(["a", "c", "v", "x", "y", "z"]);

// KeyboardEvent.code for a registry shortcut char — ⌘⇧+digit는 e.key가 기호로
// 바뀌므로(!,@,…) 분할 단축키는 물리 키(code)로 매칭한다.
function codeForShortcut(k: string): string {
  if (/^[0-9]$/.test(k)) return `Digit${k}`;
  if (/^[a-z]$/i.test(k)) return `Key${k.toUpperCase()}`;
  if (k === ",") return "Comma";
  return "";
}

// 마우스용 분할 추가 스트립 — 작업 영역 오른쪽의 슬림한 + 버튼. 클릭하면 아직 타일에
// 없는 pane 목록 팝오버가 뜨고, 고르면 현재 화면 옆에 분할로 열린다. (단축키·Alt+클릭을
// 안 쓰는 사용 방식의 1차 분할 진입점.)
function SplitAddStrip({ edgeTabVisible = false }: { edgeTabVisible?: boolean }) {
  const { tiles, splitPane, hiddenViews, viewOrder } = useWorkspace();
  const [open, setOpen] = useState(false);
  if (tiles.length >= MAX_TILES) return null;
  const candidates = orderedViews(viewOrder).filter(
    (k) => isTileable(k) && !tiles.includes(k) && !hiddenViews.includes(k),
  );
  if (candidates.length === 0) return null;
  return (
    // 패널이 접혀 ai-reopen 엣지 탭이 떠 있으면 그 폭만큼 비켜선다 — 두 컨트롤이
    // 같은 우측 세로 중앙에 겹치던 결함.
    <div className={"split-add" + (edgeTabVisible ? " with-edge-tab" : "")}>
      <button
        className="split-add-btn"
        onClick={() => setOpen((v) => !v)}
        title="화면 분할 추가"
        aria-label="화면 분할 추가"
        aria-expanded={open}
      >
        <Icon name="plus" size={14} />
      </button>
      {open && (
        <>
          <div className="split-add-backdrop" onMouseDown={() => setOpen(false)} role="presentation" />
          <div className="split-add-menu" role="menu" aria-label="분할로 열 화면">
            {candidates.map((k) => (
              <button
                key={k}
                role="menuitem"
                className="split-add-item"
                onClick={() => {
                  splitPane(k);
                  setOpen(false);
                }}
              >
                <Icon name={k} size={13} /> {paneLabel(k)}
              </button>
            ))}
          </div>
        </>
      )}
    </div>
  );
}

export function Workstation({ cfg }: { cfg: GatewayConfig }) {
  const {
    view,
    setView,
    tiles,
    splitPane,
    closePane,
    connected,
    notebookTop,
    paletteOpen,
    setPaletteOpen,
    aiCollapsed,
    setAiCollapsed,
    spotlight,
  } = useWorkspace();

  // 데네브 spotlight — 대상 타일을 1.6초 플래시. 점화는 렌더 조정(adjust-state)으로,
  // 이펙트는 타이머 해제만 소유한다 (setState-in-effect 캐스케이드 회피).
  const [flashSeq, setFlashSeq] = useState<number | null>(null);
  if (spotlight && flashSeq !== spotlight.seq && flashSeq !== -spotlight.seq) {
    setFlashSeq(spotlight.seq);
  }
  useEffect(() => {
    if (flashSeq === null || flashSeq < 0) return;
    // 만료 표식은 -seq — 같은 seq로 렌더 조정이 재점화하지 않게 한다.
    const timer = window.setTimeout(() => setFlashSeq((s) => (s === null ? null : -s)), 1600);
    return () => window.clearTimeout(timer);
  }, [flashSeq]);

  // 우측 데네브 패널을 중앙 작업 영역까지 넓히는 토글(maximize). 활성화되면 작업 pane을
  // 숨기고 AIPanel이 사이드바를 제외한 전 폭을 차지한다. 채팅 탭에선 ChatView가
  // 중앙+우측을 이미 점유하므로 의미 없다(AIPanel 자체가 숨겨짐).
  const [aiExpanded, setAiExpanded] = useState(false);

  // 파일 pane은 첫 방문 이후 계속 마운트 유지(열린 탭·미저장 편집을 pane 전환에도 보존).
  // 방문 전엔 렌더하지 않아 불필요한 프리페치·DOM 중복을 피한다.
  const [filesMounted, setFilesMounted] = useState(false);
  if (view === "files" && !filesMounted) setFilesMounted(true);

  // Durable catch-up sync, session-scoped (Workstation is always mounted): keeps
  // the work feed / calendar reconciled even when a live proactive push is missed.
  useNativeSync(cfg, connected);

  const shortcuts = useMemo(() => {
    const byKey: Record<string, View> = {};
    const byCode: Record<string, View> = {};
    for (const p of PANES) {
      byKey[p.shortcut] = p.key;
      const code = codeForShortcut(p.shortcut);
      if (code) byCode[code] = p.key;
    }
    return { byKey, byCode };
  }, []);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (!(e.metaKey || e.ctrlKey)) return;
      const lower = e.key.toLowerCase();
      // ⌘K — 커맨드 팔레트. pane 단축키보다 먼저 본다(레지스트리의 k는 비웠다).
      if (lower === "k" && !e.shiftKey) {
        e.preventDefault();
        setPaletteOpen(!paletteOpen);
        return;
      }
      if (EDIT_KEYS.has(lower)) return; // 복사·붙여넣기류는 절대 가로채지 않는다
      // ⌘⇧+key — 해당 pane을 분할로 추가(물리 키 매칭; EDIT_KEYS 조합은 위에서 걸렀다).
      if (e.shiftKey) {
        const target = shortcuts.byCode[e.code];
        if (!target || !isTileable(target)) return;
        e.preventDefault();
        splitPane(target);
        return;
      }
      const next = shortcuts.byKey[e.key];
      if (!next) return;
      e.preventDefault();
      setView(next);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [shortcuts, setView, splitPane, paletteOpen, setPaletteOpen]);

  // 노트북 화면에서는 데네브 채팅을 우측이 아니라 하단 전 폭으로 도킹한다 — 노트북의 메인
  // 작업이 자료를 근거로 AI에게 질문하는 것이라, 좁은 측면 패널보다 넓은 하단이 맞다. CSS
  // 그리드로 같은 AIPanel 엘리먼트를 하단 셀에 배치만 바꾸므로(리마운트 없음) 대화는 유지된다.
  // 분할(타일) 모드에서는 적용하지 않는다 — 하단 도킹은 단일-노트북 전제의 레이아웃이다.
  const tiled = isTileable(view) && tiles.length > 1 && tiles.includes(view);
  const bottomChat = view === "notebook" && !tiled;
  // 노트북 상단(자료) 높이 3단계 — NotebookPane의 바 버튼이 컨텍스트로 순환시키고, 여기서
  // 그리드 상단 행을 접힘=auto(바 높이)·확대=70%로 바꾼다(기본은 CSS 기본값 30%). 접힘 시
  // 확보된 높이는 하단 채팅이 갖고, 확대 시엔 반대로 상단이 넓어진다.
  const topFolded = bottomChat && notebookTop === "folded";
  const topExpanded = bottomChat && notebookTop === "expanded";
  // 접기는 측면 모드에만 적용(노트북 하단 도킹 제외).
  const aiSideCollapsed = aiCollapsed && !bottomChat;
  // 작업 pane은 비채팅 탭에서 렌더. 데네브 패널 확대(maximize) 시엔 숨기지만, 노트북
  // 하단 채팅 모드·패널 접힘에서는 작업 pane이 전 폭을 차지하도록 함께 렌더한다.
  const mainVisible = bottomChat || aiSideCollapsed || !aiExpanded;
  // 파일 pane은 chat 처럼 별도로 항상 마운트되므로(열린 탭·미저장 편집 보존) 제네릭
  // 렌더에서 제외한다.
  const showMain = view !== "chat" && view !== "files" && mainVisible;
  const showFiles = view === "files" && mainVisible;

  const paneStyle = { flex: 1, minWidth: 0, overflow: "auto", padding: "20px 22px" } as const;

  return (
    <div
      className={
        "workstation-shell" +
        (bottomChat ? " ws-bottom-chat" : "") +
        (topFolded ? " ws-top-folded" : "") +
        (topExpanded ? " ws-top-expanded" : "")
      }
    >
      {/* Transparent top-edge drag handle — grab the very top of the frameless
          window to move it. Lives in the top padding band, clear of the panels
          and the top-left controls. */}
      <div className="drag-strip" data-tauri-drag-region />
      <Sidebar />
      {showMain &&
        (tiled ? (
          // 분할 워크스페이스 — 타일마다 떠 있는 패널 하나. 포커스 타일(액센트 링)이 AI
          // 컨텍스트의 1순위가 되고, 클릭으로 포커스가 따라온다. 타일은 포커스 전환에도
          // 리마운트되지 않는다(각자의 상태·스크롤 유지).
          tiles.map((t) => {
            const P = PANES.find((p) => p.key === t)?.Component;
            if (!P) return null;
            const focused = t === view;
            const spot = flashSeq !== null && flashSeq > 0 && spotlight?.view === t;
            return (
              <main
                key={t}
                className={"panel tile" + (focused ? " tile-focused" : "") + (spot ? " tile-spotlight" : "")}
                style={{ ...paneStyle, position: "relative" }}
                onMouseDownCapture={focused ? undefined : () => setView(t)}
                aria-label={paneLabel(t)}
              >
                <TileCtx.Provider value={t}>
                  <button
                    className="tile-close"
                    onClick={() => closePane(t)}
                    title={`${paneLabel(t)} 분할 닫기`}
                    aria-label={`${paneLabel(t)} 분할 닫기`}
                  >
                    <Icon name="close" size={12} />
                  </button>
                  <div className="pane-enter">
                    <P />
                  </div>
                </TileCtx.Provider>
              </main>
            );
          })
        ) : (
          <main
            className={
              "panel" +
              (bottomChat ? " ws-main" : "") +
              (flashSeq !== null && flashSeq > 0 && spotlight?.view === view ? " tile-spotlight" : "")
            }
            style={paneStyle}
          >
            <TileCtx.Provider value={view}>
              <div key={view} className="pane-enter">
                {(() => {
                  const Active = PANES.find((p) => p.key === view)?.Component ?? PANES[0].Component;
                  return <Active />;
                })()}
              </div>
            </TileCtx.Provider>
          </main>
        ))}
      {/* 파일 pane — 첫 방문 때 마운트하고 이후 계속 마운트 유지(숨김)해, 열린 탭과 저장하지
          않은 편집이 pane 전환에도 살아남게 한다(채팅·코드와 같은 규칙; key 리마운트면 편집이
          날아간다). active 로 현재 뷰일 때만 AI 패널에 게시(숨은 동안 오염 방지).
          단 게이트웨이 식별(URL/토큰)이 바뀌면 key 리마운트로 탭·편집 버퍼를 비운다 — 이전
          게이트웨이에서 연 내용이 새 게이트웨이의 같은 경로로 저장되는 사고 방지. */}
      {filesMounted && (
        <main
          key={`${cfg.url}|${cfg.token}`}
          className="panel"
          style={{
            flex: 1,
            minWidth: 0,
            overflow: "auto",
            padding: "20px 22px",
            display: showFiles ? undefined : "none",
          }}
        >
          <TileCtx.Provider value="files">
            <FilesPane />
          </TileCtx.Provider>
        </main>
      )}
      {/* 분할 추가 스트립 — 타일 여유가 있고 작업 영역이 보일 때만. */}
      {showMain && isTileable(view) && !bottomChat && <SplitAddStrip edgeTabVisible={aiSideCollapsed} />}
      {/* 채팅 탭(비업무)·측면 데네브 패널 모두 항상 마운트(각자 대화 유지) — 비활성 탭에선 숨긴다. */}
      <ChatView cfg={cfg} hidden={view !== "chat"} />
      <AIPanel
        cfg={cfg}
        hidden={view === "chat" || aiSideCollapsed}
        placement={bottomChat ? "bottom" : "side"}
        expanded={!bottomChat && aiExpanded}
        onToggleExpand={bottomChat ? undefined : () => setAiExpanded((v) => !v)}
        onCollapse={
          bottomChat
            ? undefined
            : () => {
                setAiCollapsed(true);
                setAiExpanded(false);
              }
        }
      />
      {/* 패널이 접혀 있을 때 우측 가장자리의 다시-열기 탭. */}
      {aiSideCollapsed && view !== "chat" && (
        <button
          className="ai-reopen"
          onClick={() => setAiCollapsed(false)}
          title="Deneb 패널 열기"
          aria-label="Deneb 패널 열기"
        >
          <Icon name="chat" size={16} />
        </button>
      )}
      {paletteOpen && <CommandPalette />}
    </div>
  );
}
