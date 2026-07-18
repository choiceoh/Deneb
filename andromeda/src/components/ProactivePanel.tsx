import { useCallback, useEffect, useRef } from "react";
import { describeCommand, isWorkspaceCommandKind, parseWorkspaceCommand } from "@/commands";
import type { ProactiveEvent } from "@/events";
import { fmtMailDate } from "@/format";
import type { GatewayConfig } from "@/gateway";
import { useEvents } from "@/hooks";
import { log } from "@/log";
import { notifyDesktop } from "@/notify";
import { setBadgeCount } from "@/tauri";
import { useWorkspace } from "@/workspaceContext";
import { Icon } from "./Icon";
import { type ProactiveNav, proactiveNav } from "./proactiveNav";

const nudgeLog = log.child("proactive");

// Proactive nudges pushed by Deneb (events SSE). Sits atop the AI panel; renders
// nothing until something arrives, so it stays out of the way when quiet. A pile
// of nudges gets a header (count + 모두 지우기); each shows its title/body and a
// relative receipt time, with a warm accent rule on its left. A nudge that
// carries a deep-link target (gateway kind+ref) is clickable → opens its pane.
//
// This is also where Deneb drives the workstation: a pushed `workspace` event
// parses into a command (open/split/focus/layout — screen verbs only), executes
// through the command bus, and is replaced by a visible "화면 조정" nudge so a
// machine-driven rearrangement is never silent.
export function ProactivePanel({ cfg }: { cfg: GatewayConfig }) {
  const { connected, openPane, openWiki, runCommand } = useWorkspace();
  const intercept = useCallback(
    (ev: ProactiveEvent): ProactiveEvent | null => {
      if (!isWorkspaceCommandKind(ev.kind)) return ev;
      // The gateway's workstation tool carries the command in the frame's
      // `data` map (like phone_action frames); tolerate top-level fields too.
      const nested = ev.raw.data;
      const src =
        nested && typeof nested === "object" && !Array.isArray(nested) ? (nested as Record<string, unknown>) : ev.raw;
      const cmd = parseWorkspaceCommand(src);
      if (!cmd) {
        nudgeLog.warn("malformed workspace command dropped", JSON.stringify(ev.raw));
        return null;
      }
      runCommand(cmd);
      return { ...ev, kind: "workspace", title: ev.title ?? "화면 조정", body: describeCommand(cmd) };
    },
    [runCommand],
  );
  const { events, status, dismiss, clearAll } = useEvents(cfg, connected, intercept);

  // OS 알림: 비포커스 창에도 능동 넛지가 닿도록 — 최신 이벤트가 "새로" 도착했을
  // 때 1회 (마운트 시 이미 있던 목록은 복원분이라 침묵). notifyDesktop 자체가
  // 포커스 중·웹 빌드에선 no-op.
  //
  // 알림→행동 왕복: Tauri 데스크톱 백엔드는 알림 클릭 콜백을 노출하지 않으므로
  // (notify-rust fire-and-forget), 가장 가까운 확실 경로로 — 알림 후 60초 안에
  // 창이 포커스를 되찾으면 그 넛지의 화면으로 자동 내비 (알림 보고 돌아온 것).
  const prevNewestRef = useRef<string | undefined>(undefined);
  const notifyArmedRef = useRef(false);
  const pendingNavRef = useRef<{ nav: ProactiveNav; at: number } | null>(null);
  useEffect(() => {
    const newest = events[0];
    if (!notifyArmedRef.current) {
      notifyArmedRef.current = true;
      prevNewestRef.current = newest?.id;
      return;
    }
    if (newest && newest.id !== prevNewestRef.current) {
      prevNewestRef.current = newest.id;
      if (typeof document === "undefined" || !document.hasFocus()) {
        const nav = proactiveNav(newest);
        if (nav) pendingNavRef.current = { nav, at: Date.now() };
      }
      void notifyDesktop(newest.title ?? "데네브 알림", newest.body ?? "");
    }
  }, [events]);

  // 독/태스크바 배지 = 대기 중 넛지 수 (0이면 지움).
  useEffect(() => {
    void setBadgeCount(events.length);
  }, [events.length]);

  // 알림 후 복귀 내비 — 위 pendingNavRef 소비.
  useEffect(() => {
    function onFocus() {
      const pending = pendingNavRef.current;
      pendingNavRef.current = null;
      if (!pending || Date.now() - pending.at > 60_000) return;
      if (pending.nav.view === "wiki" && pending.nav.ref) openWiki(pending.nav.ref);
      else openPane(pending.nav.view, pending.nav.ref ? { id: pending.nav.ref } : undefined);
    }
    window.addEventListener("focus", onFocus);
    return () => window.removeEventListener("focus", onFocus);
  }, [openPane, openWiki]);

  const onNavigate = (nav: ProactiveNav) => {
    if (nav.view === "wiki" && nav.ref) openWiki(nav.ref);
    else openPane(nav.view, nav.ref ? { id: nav.ref } : undefined);
  };

  return (
    <>
      {/* 전역 오프라인 배너: 게이트웨이 핫스왑/다운 동안 페인별 산발 에러 대신
          한 줄의 정직한 상태. events SSE의 지수 백오프가 재연결을 소유한다. */}
      {status === "재연결 중…" && (
        <div className="offline-banner" role="status">
          게이트웨이 재연결 중… 자동으로 복구됩니다
        </div>
      )}
      <ProactiveList
        events={events}
        status={status}
        onDismiss={dismiss}
        onClearAll={clearAll}
        onNavigate={onNavigate}
      />
    </>
  );
}

// Presentational — events injected, so it renders without an SSE subscription
// (tested directly). `now` is injectable for deterministic relative times.
export function ProactiveList({
  events,
  onDismiss,
  onClearAll,
  onNavigate,
  status,
  now,
}: {
  events: ProactiveEvent[];
  onDismiss: (id: string) => void;
  onClearAll: () => void;
  onNavigate?: (nav: ProactiveNav) => void;
  status?: string;
  now?: number;
}) {
  const showStatus = status === "재연결 중…" || status?.startsWith("오류:");
  if (events.length === 0 && !showStatus) return null;

  return (
    <div className="proactive-panel" aria-live="polite" aria-label="능동 알림">
      {showStatus && <div className="pane-status">능동 알림 · {status}</div>}
      {events.length > 0 && (
        <div className="proactive-head">
          <span className="proactive-head-label">알림 {events.length}</span>
          <button className="proactive-clear" onClick={onClearAll}>
            모두 지우기
          </button>
        </div>
      )}
      {events.map((e) => {
        const nav = onNavigate ? proactiveNav(e) : null;
        const body = (
          <>
            <div className="proactive-nudge-title">{e.title ?? e.kind ?? "알림"}</div>
            {e.body && <div className="proactive-nudge-body">{e.body}</div>}
            {e.ts != null && <div className="proactive-nudge-time">{fmtMailDate(e.ts, now)}</div>}
          </>
        );
        return (
          <div key={e.id} className="proactive-nudge">
            {nav ? (
              <button
                className="proactive-nudge-main proactive-nudge-link"
                onClick={() => {
                  onNavigate!(nav);
                  onDismiss(e.id);
                }}
                title="열기"
              >
                {body}
              </button>
            ) : (
              <div className="proactive-nudge-main">{body}</div>
            )}
            <button className="proactive-nudge-dismiss" onClick={() => onDismiss(e.id)} title="닫기" aria-label="닫기">
              <Icon name="close" size={13} />
            </button>
          </div>
        );
      })}
    </div>
  );
}
