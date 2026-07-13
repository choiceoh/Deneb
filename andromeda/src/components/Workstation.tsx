import { useEffect, useMemo, useState } from "react";
import type { GatewayConfig } from "@/gateway";
import { useNativeSync } from "@/sync";
import type { View } from "@/types";
import { useWorkspace } from "@/workspaceContext";
import { AIPanel } from "./AIPanel";
import { ChatView } from "./ChatView";
import { Icon } from "./Icon";
import { Sidebar } from "./Sidebar";
import { PANES } from "./panes";
import { FilesPane } from "./panes/FilesPane";

// The shell: a slim nav rail + two floating panels (work area · Deneb AI) drifting
// on the window's gradient, Zen-browser style. The work area renders only the
// active pane; ⌘/Ctrl+0–9 shortcuts are derived from the pane registry (the labels
// are hidden in the rail, but the keys still work).

// OS 표준 텍스트 편집 콤보(⌘/Ctrl + 복사·붙여넣기·잘라내기·전체선택·실행취소/재실행)의
// 키 — pane 단축키가 이걸 가로채면 복사가 화면 전환으로 둔갑한다(코드 pane의 옛 ⌘C 사고).
// 레지스트리에 이 키를 배정해도 onKey 가드가 걸러내 편집 동작이 항상 이긴다.
const EDIT_KEYS = new Set(["a", "c", "v", "x", "y", "z"]);

export function Workstation({ cfg }: { cfg: GatewayConfig }) {
  const { view, setView, connected, notebookTop } = useWorkspace();

  // 우측 데네브 패널을 중앙 작업 영역까지 넓히는 토글(maximize). 활성화되면 작업 pane을
  // 숨기고 AIPanel이 사이드바를 제외한 전 폭을 차지한다. 채팅 탭에선 ChatView가
  // 중앙+우측을 이미 점유하므로 의미 없다(AIPanel 자체가 숨겨짐).
  const [aiExpanded, setAiExpanded] = useState(false);

  // 우측 데네브 패널 접기 — 위키처럼 본문을 넓게 보고 싶을 때 패널을 완전히 숨긴다. 숨기면
  // 작업 pane이 전 폭을 차지하고, 우측 가장자리의 작은 탭으로 다시 연다. (노트북 하단 채팅
  // 모드에는 적용하지 않는다 — 거기선 채팅이 하단에 도킹돼 있다.)
  // 기본은 접힘 — 작업 영역을 넓게 두고, 필요할 때 우측 탭으로 데네브 패널을 연다.
  const [aiCollapsed, setAiCollapsed] = useState(true);

  // 파일 pane은 첫 방문 이후 계속 마운트 유지(열린 탭·미저장 편집을 pane 전환에도 보존).
  // 방문 전엔 렌더하지 않아 불필요한 프리페치·DOM 중복을 피한다.
  const [filesMounted, setFilesMounted] = useState(false);
  if (view === "files" && !filesMounted) setFilesMounted(true);

  // Durable catch-up sync, session-scoped (Workstation is always mounted): keeps
  // the work feed / calendar reconciled even when a live proactive push is missed.
  useNativeSync(cfg, connected);

  const shortcuts = useMemo(() => {
    const m: Record<string, View> = {};
    for (const p of PANES) m[p.shortcut] = p.key;
    return m;
  }, []);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (!(e.metaKey || e.ctrlKey)) return;
      if (EDIT_KEYS.has(e.key.toLowerCase())) return; // 복사·붙여넣기류는 절대 가로채지 않는다
      const next = shortcuts[e.key];
      if (!next) return;
      e.preventDefault();
      setView(next);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [shortcuts, setView]);

  const Active = PANES.find((p) => p.key === view)?.Component ?? PANES[0].Component;

  // 노트북 화면에서는 데네브 채팅을 우측이 아니라 하단 전 폭으로 도킹한다 — 노트북의 메인
  // 작업이 자료를 근거로 AI에게 질문하는 것이라, 좁은 측면 패널보다 넓은 하단이 맞다. CSS
  // 그리드로 같은 AIPanel 엘리먼트를 하단 셀에 배치만 바꾸므로(리마운트 없음) 대화는 유지된다.
  const bottomChat = view === "notebook";
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
      {showMain && (
        <main
          className={"panel" + (bottomChat ? " ws-main" : "")}
          style={{ flex: 1, minWidth: 0, overflow: "auto", padding: "20px 22px" }}
        >
          <div key={view} className="pane-enter">
            <Active />
          </div>
        </main>
      )}
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
          <FilesPane active={view === "files"} />
        </main>
      )}
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
    </div>
  );
}
