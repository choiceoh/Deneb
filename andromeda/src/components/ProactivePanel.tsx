import type { ProactiveEvent } from "@/events";
import { fmtMailDate } from "@/format";
import type { GatewayConfig } from "@/gateway";
import { useEvents } from "@/hooks";
import type { View } from "@/types";
import { useWorkspace } from "@/workspaceContext";
import { Icon } from "./Icon";

export interface ProactiveNav {
  view: View;
  ref?: string;
}

// Pane keys a proactive nudge may target. The gateway sets event.kind to one of
// these in the push payload (clientPushEvent), so the nudge can open the relevant
// pane; absent/unknown kinds — the SSE "push" event-name fallback, errors — aren't
// navigable and render as plain text. ref is the resource id (or wiki path).
const NAV_VIEWS = new Set<View>([
  "mail",
  "calendar",
  "todo",
  "workfeed",
  "fleet",
  "wiki",
  "people",
  "crons",
  "files",
  "search",
  "progress",
  "notebook",
  "today",
]);

export function proactiveNav(ev: ProactiveEvent): ProactiveNav | null {
  const kind = ev.kind;
  if (!kind || !NAV_VIEWS.has(kind as View)) return null;
  return { view: kind as View, ref: ev.ref };
}

// Proactive nudges pushed by Deneb (events SSE). Sits atop the AI panel; renders
// nothing until something arrives, so it stays out of the way when quiet. A pile
// of nudges gets a header (count + 모두 지우기); each shows its title/body and a
// relative receipt time, with a warm accent rule on its left. A nudge that
// carries a deep-link target (gateway kind+ref) is clickable → opens its pane.
export function ProactivePanel({ cfg }: { cfg: GatewayConfig }) {
  const { connected, openPane, openWiki } = useWorkspace();
  const { events, dismiss, clearAll } = useEvents(cfg, connected);

  const onNavigate = (nav: ProactiveNav) => {
    if (nav.view === "wiki" && nav.ref) openWiki(nav.ref);
    else openPane(nav.view, nav.ref ? { id: nav.ref } : undefined);
  };

  return <ProactiveList events={events} onDismiss={dismiss} onClearAll={clearAll} onNavigate={onNavigate} />;
}

// Presentational — events injected, so it renders without an SSE subscription
// (tested directly). `now` is injectable for deterministic relative times.
export function ProactiveList({
  events,
  onDismiss,
  onClearAll,
  onNavigate,
  now,
}: {
  events: ProactiveEvent[];
  onDismiss: (id: string) => void;
  onClearAll: () => void;
  onNavigate?: (nav: ProactiveNav) => void;
  now?: number;
}) {
  if (events.length === 0) return null;

  return (
    <div className="proactive-panel" aria-live="polite" aria-label="능동 알림">
      <div className="proactive-head">
        <span className="proactive-head-label">알림 {events.length}</span>
        <button className="proactive-clear" onClick={onClearAll}>
          모두 지우기
        </button>
      </div>
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
