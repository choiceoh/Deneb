import { useEffect, useState } from "react";
import { codeSessions } from "@/gateway";
import type { CodeSession } from "@/types";
import { useWorkspace } from "@/workspaceContext";
import { Icon } from "./Icon";
import { WindowControls } from "./WindowControls";
import { orderedViews, PANES } from "./panes";

const labelStyle = { overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" } as const;

// Code-session status → rail dot color (사용자 요청 매핑):
//   working(작업중)  → 진행중  → 초록 (--online)
//   failed/missing   → 문제    → 빨강 (--danger)
//   passed/그 외     → 멈춤(완료) → 검정 (--ink)
function codeDotColor(status?: string): string {
  switch (status) {
    case "working":
      return "var(--online)";
    case "failed":
    case "missing":
      return "var(--danger)";
    default:
      return "var(--ink)";
  }
}
function codeDotLabel(status?: string): string {
  switch (status) {
    case "working":
      return "진행중";
    case "failed":
      return "문제";
    case "missing":
      return "문제 (워크트리 없음)";
    default:
      return "멈춤";
  }
}

// Slim nav rail. Normally registry-driven pane tabs (the active one lifts like a Zen
// tab) in the user's order, with 설정 pinned bottom-left. In 코드 모드 the pane list is
// replaced by the coding-session list — the toggle sits just above 설정, and the
// work area is the 코드 pane. The toggle persists, so the rail stays where you left it.
export function Sidebar() {
  const {
    view,
    setView,
    hiddenViews,
    viewOrder,
    codeMode,
    setCodeMode,
    connected,
    cfg,
    codeSessionsRev,
    openCodeChat,
  } = useWorkspace();
  const visiblePanes = orderedViews(viewOrder)
    .filter((k) => !hiddenViews.includes(k))
    .map((k) => PANES.find((p) => p.key === k)!);
  const settings = PANES.find((p) => p.key === "settings")!;

  // Session list backing the rail in 코드 모드. Refetch on (re)entering the mode or
  // returning to the 코드 view, so create/delete in CodePane reflects here.
  const [sessions, setSessions] = useState<CodeSession[]>([]);
  useEffect(() => {
    if (!codeMode || !connected) {
      setSessions([]);
      return;
    }
    let alive = true;
    codeSessions(cfg)
      .then((s) => alive && setSessions(s))
      .catch(() => alive && setSessions([]));
    return () => {
      alive = false;
    };
  }, [codeMode, connected, cfg, view, codeSessionsRev]);

  function toggleCodeMode() {
    const next = !codeMode;
    setCodeMode(next);
    setView(next ? "code" : "today");
  }

  return (
    <nav
      data-tauri-drag-region
      style={{
        width: "var(--rail-w)",
        flex: "0 0 auto",
        display: "flex",
        flexDirection: "column",
        gap: 2,
        padding: "2px 2px",
        position: "relative",
      }}
    >
      <WindowControls />

      {codeMode ? (
        <>
          <button className="nav-item" onClick={() => setView("code")} title="새 작업">
            <span className="ico">
              <Icon name="plus" />
            </span>
            <span style={labelStyle}>새 작업</span>
          </button>
          {sessions.map((s, i) => (
            <button
              key={s.id}
              className="nav-item fade-up"
              style={{ animationDelay: `${i * 26}ms` }}
              onClick={() => {
                // Open this session's chat on the right + show the work area. Now
                // chatting drives this worktree's turns (edit → verify → checkpoint).
                openCodeChat(s.chatSessionKey || "code:" + s.id);
                setView("code");
              }}
              title={`${s.title || s.id} · ${codeDotLabel(s.status)} — 대화 열기`}
            >
              <span className="ico code-ico">
                <Icon name="code" />
                <span
                  className="code-dot"
                  style={{ background: codeDotColor(s.status) }}
                  role="img"
                  aria-label={codeDotLabel(s.status)}
                />
              </span>
              <span style={labelStyle}>{s.title || s.id}</span>
            </button>
          ))}
          {sessions.length === 0 && (
            <div style={{ padding: "8px 10px", opacity: 0.5, fontSize: 12 }}>
              {connected ? "세션 없음" : "연결 안 됨"}
            </div>
          )}
        </>
      ) : (
        visiblePanes.map((p, i) => (
          <button
            key={p.key}
            className={"nav-item fade-up" + (view === p.key ? " active" : "")}
            style={{ animationDelay: `${i * 26}ms` }}
            onClick={() => setView(p.key)}
            title={p.label}
          >
            <span className="ico">
              <Icon name={p.key} />
            </span>
            <span style={labelStyle}>{p.label}</span>
          </button>
        ))
      )}

      {/* 코드 모드 토글 — 켜면 레일이 세션 리스트로. 설정 바로 위. */}
      <button
        className={"nav-item" + (codeMode ? " active" : "")}
        style={{ marginTop: "auto" }}
        onClick={toggleCodeMode}
        title="코드 모드"
      >
        <span className="ico">
          <Icon name="code" />
        </span>
        <span style={labelStyle}>코드 모드</span>
      </button>

      {/* 설정 pinned to the bottom-left (gateway config lives inside it). */}
      <button
        className={"nav-item" + (view === "settings" ? " active" : "")}
        onClick={() => setView("settings")}
        title={settings.label}
      >
        <span className="ico">
          <Icon name="settings" />
        </span>
        <span style={labelStyle}>{settings.label}</span>
      </button>
    </nav>
  );
}
