import type { ProactiveEvent } from "@/events";
import { fmtMailDate } from "@/format";
import type { GatewayConfig } from "@/gateway";
import { useEvents } from "@/hooks";
import { useWorkspace } from "@/workspaceContext";
import { Icon } from "./Icon";

// Proactive nudges pushed by Deneb (events SSE). Sits atop the AI panel; renders
// nothing until something arrives, so it stays out of the way when quiet. A pile
// of nudges gets a header (count + 모두 지우기); each shows its title/body and a
// relative receipt time, with a warm accent rule on its left.
//
// The gateway push payload is title+body only (clientPushEvent) — no resource
// id/kind — so a nudge can't deep-link to a specific mail/event yet. The receipt
// time is stamped client-side on arrival (useEvents). If the gateway grows a
// target field, ProactiveList is the single place a click-through would land.
export function ProactivePanel({ cfg }: { cfg: GatewayConfig }) {
  const { connected } = useWorkspace();
  const { events, dismiss, clearAll } = useEvents(cfg, connected);
  return <ProactiveList events={events} onDismiss={dismiss} onClearAll={clearAll} />;
}

// Presentational — events injected, so it renders without an SSE subscription
// (tested directly). `now` is injectable for deterministic relative times.
export function ProactiveList({
  events,
  onDismiss,
  onClearAll,
  now,
}: {
  events: ProactiveEvent[];
  onDismiss: (id: string) => void;
  onClearAll: () => void;
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
      {events.map((e) => (
        <div key={e.id} className="proactive-nudge">
          <div className="proactive-nudge-main">
            <div className="proactive-nudge-title">{e.title ?? e.kind ?? "알림"}</div>
            {e.body && <div className="proactive-nudge-body">{e.body}</div>}
            {e.ts != null && <div className="proactive-nudge-time">{fmtMailDate(e.ts, now)}</div>}
          </div>
          <button className="proactive-nudge-dismiss" onClick={() => onDismiss(e.id)} title="닫기" aria-label="닫기">
            <Icon name="close" size={13} />
          </button>
        </div>
      ))}
    </div>
  );
}
