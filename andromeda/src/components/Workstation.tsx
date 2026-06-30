import { useEffect, useMemo, useState } from "react";
import type { GatewayConfig } from "@/gateway";
import { useNativeSync } from "@/sync";
import type { View } from "@/types";
import { useWorkspace } from "@/workspaceContext";
import { AIPanel } from "./AIPanel";
import { ChatView } from "./ChatView";
import { CodeView } from "./CodeView";
import { Sidebar } from "./Sidebar";
import { PANES } from "./panes";

// The shell: a slim nav rail + two floating panels (work area · Deneb AI) drifting
// on the window's gradient, Zen-browser style. The work area renders only the
// active pane; ⌘/Ctrl+0–9 shortcuts are derived from the pane registry (the labels
// are hidden in the rail, but the keys still work).
export function Workstation({ cfg }: { cfg: GatewayConfig }) {
  const { view, setView, codeMode, connected } = useWorkspace();

  // 우측 데네브 패널을 중앙 작업 영역까지 넓히는 토글(maximize). 활성화되면 작업 pane을
  // 숨기고 AIPanel이 사이드바를 제외한 전 폭을 차지한다. 채팅·코드 탭에선 ChatView/CodeView가
  // 중앙+우측을 이미 점유하므로 의미 없다(AIPanel 자체가 숨겨짐).
  const [aiExpanded, setAiExpanded] = useState(false);

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
  const bottomChat = view === "notebook" && !codeMode;
  // 작업 pane은 비채팅·비코드 탭에서 렌더. 데네브 패널 확대(maximize) 시엔 숨기지만, 노트북
  // 하단 채팅 모드에서는 위쪽 셀(자료)로 항상 함께 렌더한다.
  const showMain = view !== "chat" && !codeMode && (bottomChat || !aiExpanded);

  return (
    <div className={"workstation-shell" + (bottomChat ? " ws-bottom-chat" : "")}>
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
      {/* 코드 모드 — 가운데 코딩 채팅(주작업) + 우측 작업 관리. 항상 마운트, 비활성 시 숨김. */}
      <CodeView cfg={cfg} hidden={!codeMode} />
      {/* 채팅 탭(비업무)·측면 데네브 패널 모두 항상 마운트(각자 대화 유지) — 비활성 탭에선 숨긴다. */}
      <ChatView cfg={cfg} hidden={view !== "chat" || codeMode} />
      <AIPanel
        cfg={cfg}
        hidden={view === "chat" || codeMode}
        placement={bottomChat ? "bottom" : "side"}
        expanded={!bottomChat && aiExpanded}
        onToggleExpand={bottomChat ? undefined : () => setAiExpanded((v) => !v)}
      />
    </div>
  );
}
